/*
Copyright 2021.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"time"

	"github.com/go-logr/logr"
	commonLabels "github.com/medik8s/common/pkg/labels"
	"github.com/medik8s/common/pkg/lease"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/fields"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/klog"
	"k8s.io/kubectl/pkg/cmd/util"
	"k8s.io/kubectl/pkg/drain"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

const (
	maxAllowedErrorToUpdateOwnedLease = 3
	waitDurationOnDrainError          = 5 * time.Second
	FixedDurationReconcileLog         = "Reconciling with fixed duration"

	//lease consts
	LeaseHolderIdentity = "node-maintenance"
	LeaseDuration       = 3600 * time.Second
	DrainerTimeout      = 30 * time.Second
)

// NodeMaintenanceReconciler reconciles a NodeMaintenance object
type NodeMaintenanceReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	MgrConfig    *rest.Config
	LeaseManager lease.Manager
	logger       logr.Logger
}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeMaintenanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1beta1.NodeMaintenance{}).
		Complete(r)
}

//+kubebuilder:rbac:groups=nodemaintenance.medik8s.io,resources=nodemaintenances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nodemaintenance.medik8s.io,resources=nodemaintenances/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodemaintenance.medik8s.io,resources=nodemaintenances/finalizers,verbs=update

// TODO check if all these are really needed!
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;patch;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get;create
//+kubebuilder:rbac:groups="apps",resources=deployments;daemonsets;replicasets;statefulsets,verbs=get;list;watch
//+kubebuilder:rbac:groups="coordination.k8s.io",resources=leases,verbs=get;list;update;patch;watch;create
//+kubebuilder:rbac:groups="policy",resources=poddisruptionbudgets,verbs=get;list;watch
//+kubebuilder:rbac:groups="monitoring.coreos.com",resources=servicemonitors,verbs=get;create
//+kubebuilder:rbac:groups="oauth.openshift.io",resources=*,verbs=*

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the NodeMaintenance object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.9.2/pkg/reconcile
func (r *NodeMaintenanceReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	r.logger = log.FromContext(ctx)
	r.logger.Info("Reconciling NodeMaintenance")
	defer r.logger.Info("Reconcile completed")
	emptyResult := ctrl.Result{}

	// Fetch the NodeMaintenance instance (nm)
	nm := &v1beta1.NodeMaintenance{}
	err := r.Client.Get(ctx, req.NamespacedName, nm)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			r.logger.Info("NodeMaintenance not found", "name", req.NamespacedName)
			return emptyResult, nil
		}
		// Error reading the object - requeue the request.
		r.logger.Info("Error reading the request object, requeuing.")
		return emptyResult, err
	}

	// Add finalizer when object is created
	drainer, err := createDrainer(r.MgrConfig, ctx)
	if err != nil {
		return emptyResult, err
	}

	if !controllerutil.ContainsFinalizer(nm, v1beta1.NodeMaintenanceFinalizer) && nm.ObjectMeta.DeletionTimestamp.IsZero() {
		controllerutil.AddFinalizer(nm, v1beta1.NodeMaintenanceFinalizer)
		if err := r.Client.Update(ctx, nm); err != nil {
			return r.onReconcileError(nm, drainer, ctx, err)
		}
	} else if controllerutil.ContainsFinalizer(nm, v1beta1.NodeMaintenanceFinalizer) && !nm.ObjectMeta.DeletionTimestamp.IsZero() {
		// The object is being deleted
		r.logger.Info("Deletion timestamp not zero")

		// Stop node maintenance - uncordon and remove live migration taint from the node.
		if err := r.stopNodeMaintenanceOnDeletion(drainer, ctx, nm.Spec.NodeName); err != nil {
			r.logger.Error(err, "error stopping node maintenance")
			if !errors.IsNotFound(err) {
				return r.onReconcileError(nm, drainer, ctx, err)
			}
		}

		// Remove finalizer
		controllerutil.RemoveFinalizer(nm, v1beta1.NodeMaintenanceFinalizer)
		if err := r.Client.Update(ctx, nm); err != nil {
			return r.onReconcileError(nm, drainer, ctx, err)
		}
		return emptyResult, nil
	}

	err = initMaintenanceStatus(nm, drainer, ctx, r.Client)
	if err != nil {
		r.logger.Error(err, "Failed to update NodeMaintenance with \"Running\" status")
		return r.onReconcileError(nm, drainer, ctx, err)
	}

	nodeName := nm.Spec.NodeName

	r.logger.Info("Applying maintenance mode", "node", nodeName, "reason", nm.Spec.Reason)
	node, err := r.fetchNode(drainer, ctx, nodeName)
	if err != nil {
		return r.onReconcileError(nm, drainer, ctx, err)
	}

	setOwnerRefToNode(nm, node, r.logger)

	updateOwnedLeaseFailed, err := r.obtainLease(ctx, node)
	if err != nil && updateOwnedLeaseFailed {
		nm.Status.ErrorOnLeaseCount += 1
		if nm.Status.ErrorOnLeaseCount > maxAllowedErrorToUpdateOwnedLease {
			r.logger.Info("can't extend owned lease. uncordon for now")

			// Uncordon the node
			err = r.stopNodeMaintenanceImp(drainer, node, ctx)
			if err != nil {
				return r.onReconcileError(nm, drainer, ctx, fmt.Errorf("failed to uncordon upon failure to obtain owned lease : %v ", err))
			}
			nm.Status.Phase = v1beta1.MaintenanceFailed
		}
		return r.onReconcileError(nm, drainer, ctx, fmt.Errorf("failed to extend lease owned by us : %v errorOnLeaseCount %d", err, nm.Status.ErrorOnLeaseCount))
	}
	if err != nil {
		nm.Status.ErrorOnLeaseCount = 0
		return r.onReconcileError(nm, drainer, ctx, err)
	} else {
		if nm.Status.Phase != v1beta1.MaintenanceRunning || nm.Status.ErrorOnLeaseCount != 0 {
			nm.Status.Phase = v1beta1.MaintenanceRunning
			nm.Status.ErrorOnLeaseCount = 0

		}
	}

	if err := addExcludeRemediationLabel(node, r.Client, ctx, r.logger); err != nil {
		return r.onReconcileError(nm, drainer, ctx, err)
	}

	// Cordon node
	err = AddOrRemoveTaint(drainer.Client, true, node, ctx)
	if err != nil {
		return r.onReconcileError(nm, drainer, ctx, err)
	}

	if err = drain.RunCordonOrUncordon(drainer, node, true); err != nil {
		return r.onReconcileError(nm, drainer, ctx, err)
	}

	r.logger.Info("Evict all Pods from Node", "nodeName", nodeName)

	if err = drain.RunNodeDrain(drainer, nodeName); err != nil {
		r.logger.Info("Not all pods evicted", "nodeName", nodeName, "error", err)
		waitOnReconcile := waitDurationOnDrainError
		return r.onReconcileErrorWithRequeue(nm, drainer, ctx, err, &waitOnReconcile)
	} else if nm.Status.Phase != v1beta1.MaintenanceSucceeded {
		setLastUpdate(nm)
	}

	nm.Status.Phase = v1beta1.MaintenanceSucceeded
	nm.Status.DrainProgress = 100
	nm.Status.PendingPods = nil
	err = r.Client.Status().Update(ctx, nm)
	if err != nil {
		r.logger.Error(err, "Failed to update NodeMaintenance with \"Succeeded\" status")
		return r.onReconcileError(nm, drainer, ctx, err)
	}

	r.logger.Info("Maintenance was completed - all pods were evicted", "nodeName", nodeName)
	return emptyResult, nil

}

// createDrainer creates a drain.Helper struct for external cordon and drain API
func createDrainer(mgrConfig *rest.Config, ctx context.Context) (*drain.Helper, error) {
	drainer := &drain.Helper{}

	//Continue even if there are pods not managed by a ReplicationController, ReplicaSet, Job, DaemonSet or StatefulSet.
	//This is required because VirtualMachineInstance pods are not owned by a ReplicaSet or DaemonSet controller.
	//This means that the drain operation can’t guarantee that the pods being terminated on the target node will get
	//re-scheduled replacements placed else where in the cluster after the pods are evicted.
	//medik8s has its own controllers which manage the underlying VirtualMachineInstance pods.
	//Each controller behaves differently to a VirtualMachineInstance being evicted.
	drainer.Force = true

	//Continue even if there are pods using emptyDir (local data that will be deleted when the node is drained).
	//This is necessary for removing any pod that utilizes an emptyDir volume.
	//The VirtualMachineInstance Pod does use emptryDir volumes,
	//however the data in those volumes are ephemeral which means it is safe to delete after termination.
	drainer.DeleteEmptyDirData = true

	//Ignore DaemonSet-managed pods.
	//This is required because every node running a VirtualMachineInstance will also be running our helper DaemonSet called virt-handler.
	//This flag indicates that it is safe to proceed with the eviction and to just ignore DaemonSets.
	drainer.IgnoreAllDaemonSets = true

	//Period of time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used.
	drainer.GracePeriodSeconds = -1

	// TODO - add logical value or attach from the maintenance CR
	//The length of time to wait before giving up, zero means infinite
	drainer.Timeout = DrainerTimeout

	cs, err := kubernetes.NewForConfig(mgrConfig)
	if err != nil {
		return nil, err
	}
	drainer.Client = cs
	drainer.DryRunStrategy = util.DryRunNone
	drainer.Ctx = ctx

	drainer.Out = writer{klog.Info}
	drainer.ErrOut = writer{klog.Error}

	// OnPodDeletedOrEvicted function is called when a pod is evicted/deleted; for printing progress output
	drainer.OnPodDeletedOrEvicted = func(pod *corev1.Pod, usingEviction bool) {
		var verbString string
		if usingEviction {
			verbString = "Evicted"
		} else {
			verbString = "Deleted"
		}
		msg := fmt.Sprintf("pod: %s:%s %s from node: %s", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, verbString, pod.Spec.NodeName)
		klog.Info(msg)
	}
	return drainer, nil
}

func setOwnerRefToNode(nm *v1beta1.NodeMaintenance, node *corev1.Node, log logr.Logger) {

	for _, ref := range nm.ObjectMeta.GetOwnerReferences() {
		if ref.APIVersion == node.TypeMeta.APIVersion && ref.Kind == node.TypeMeta.Kind && ref.Name == node.ObjectMeta.GetName() && ref.UID == node.ObjectMeta.GetUID() {
			return
		}
	}

	log.Info("setting owner ref to node")

	nodeMeta := node.TypeMeta
	ref := metav1.OwnerReference{
		APIVersion:         nodeMeta.APIVersion,
		Kind:               nodeMeta.Kind,
		Name:               node.ObjectMeta.GetName(),
		UID:                node.ObjectMeta.GetUID(),
		BlockOwnerDeletion: ptr.To[bool](false),
		Controller:         ptr.To[bool](false),
	}

	nm.ObjectMeta.SetOwnerReferences(append(nm.ObjectMeta.GetOwnerReferences(), ref))
}

func (r *NodeMaintenanceReconciler) obtainLease(ctx context.Context, node *corev1.Node) (bool, error) {
	r.logger.Info("Lease object supported, obtaining lease")
	err := r.LeaseManager.RequestLease(ctx, node, LeaseDuration)

	if err != nil {
		r.logger.Error(err, "failed to create or get existing lease")
		return false, err
	}

	return false, nil
}

func addExcludeRemediationLabel(node *corev1.Node, r client.Client, ctx context.Context, log logr.Logger) error {
	if node.Labels[commonLabels.ExcludeFromRemediation] != "true" {
		patch := client.MergeFrom(node.DeepCopy())
		if node.Labels == nil {
			node.Labels = map[string]string{commonLabels.ExcludeFromRemediation: "true"}
		} else if node.Labels[commonLabels.ExcludeFromRemediation] != "true" {
			node.Labels[commonLabels.ExcludeFromRemediation] = "true"
		}
		if err := r.Patch(ctx, node, patch); err != nil {
			log.Error(err, "Failed to add exclude from remediation label from the node", "node name", node.Name)
			return err
		}
	}
	return nil
}

func removeExcludeRemediationLabel(node *corev1.Node, r client.Client, ctx context.Context, log logr.Logger) error {
	if node.Labels[commonLabels.ExcludeFromRemediation] == "true" {
		patch := client.MergeFrom(node.DeepCopy())
		delete(node.Labels, commonLabels.ExcludeFromRemediation)
		if err := r.Patch(ctx, node, patch); err != nil {
			log.Error(err, "Failed to remove exclude from remediation label from the node", "node name", node.Name)
			return err
		}
	}
	return nil
}

func (r *NodeMaintenanceReconciler) stopNodeMaintenanceImp(drainer *drain.Helper, node *corev1.Node, ctx context.Context) error {
	// Uncordon the node
	err := AddOrRemoveTaint(drainer.Client, false, node, ctx)
	if err != nil {
		return err
	}

	if err = drain.RunCordonOrUncordon(drainer, node, false); err != nil {
		return err
	}

	if err := r.LeaseManager.InvalidateLease(ctx, node); err != nil {
		return err
	}
	return removeExcludeRemediationLabel(node, r.Client, ctx, r.logger)
}

func (r *NodeMaintenanceReconciler) stopNodeMaintenanceOnDeletion(drainer *drain.Helper, ctx context.Context, nodeName string) error {
	node, err := r.fetchNode(drainer, ctx, nodeName)
	if err != nil {
		// if CR is gathered as result of garbage collection: the node may have been deleted, but the CR has not yet been deleted, still we must clean up the lease!
		if errors.IsNotFound(err) {
			if err := r.LeaseManager.InvalidateLease(ctx, &corev1.Node{ObjectMeta: metav1.ObjectMeta{Name: nodeName}}); err != nil {
				return err
			}
			return nil
		}
		return err
	}
	return r.stopNodeMaintenanceImp(drainer, node, ctx)
}

func (r *NodeMaintenanceReconciler) fetchNode(drainer *drain.Helper, ctx context.Context, nodeName string) (*corev1.Node, error) {
	node, err := drainer.Client.CoreV1().Nodes().Get(ctx, nodeName, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		r.logger.Error(err, "Node cannot be found", "nodeName", nodeName)
		return nil, err
	} else if err != nil {
		r.logger.Error(err, "Failed to get node", "nodeName", nodeName)
		return nil, err
	}
	return node, nil
}

func initMaintenanceStatus(nm *v1beta1.NodeMaintenance, drainer *drain.Helper, ctx context.Context, r client.Client) error {
	if nm.Status.Phase == "" {
		nm.Status.Phase = v1beta1.MaintenanceRunning
		setLastUpdate(nm)
		pendingList, errlist := drainer.GetPodsForDeletion(nm.Spec.NodeName)
		if errlist != nil {
			return fmt.Errorf("failed to get pods for eviction while initializing status")
		}
		if pendingList != nil {
			nm.Status.PendingPods = GetPodNameList(pendingList.Pods())
		}
		nm.Status.EvictionPods = len(nm.Status.PendingPods)

		podlist, err := drainer.Client.CoreV1().Pods(metav1.NamespaceAll).List(
			ctx,
			metav1.ListOptions{
				FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": nm.Spec.NodeName}).String(),
			})
		if err != nil {
			return err
		}
		nm.Status.TotalPods = len(podlist.Items)
		err = r.Status().Update(ctx, nm)
		return err
	}
	return nil
}

func (r *NodeMaintenanceReconciler) onReconcileErrorWithRequeue(nm *v1beta1.NodeMaintenance, drainer *drain.Helper, ctx context.Context, err error, duration *time.Duration) (ctrl.Result, error) {
	nm.Status.LastError = err.Error()
	setLastUpdate(nm)

	if nm.Spec.NodeName != "" {
		pendingList, _ := drainer.GetPodsForDeletion(nm.Spec.NodeName)
		if pendingList != nil {
			nm.Status.PendingPods = GetPodNameList(pendingList.Pods())
			if nm.Status.EvictionPods != 0 {
				nm.Status.DrainProgress = (nm.Status.EvictionPods - len(nm.Status.PendingPods)) * 100 / nm.Status.EvictionPods
			}
		}
	}

	updateErr := r.Client.Status().Update(ctx, nm)
	if updateErr != nil {
		r.logger.Error(updateErr, "Failed to update NodeMaintenance with \"Failed\" status")
	}
	if duration != nil {
		r.logger.Info(FixedDurationReconcileLog)
		return ctrl.Result{RequeueAfter: *duration}, nil
	}
	r.logger.Info("Reconciling with exponential duration")
	return ctrl.Result{}, err
}

func (r *NodeMaintenanceReconciler) onReconcileError(nm *v1beta1.NodeMaintenance, drainer *drain.Helper, ctx context.Context, err error) (ctrl.Result, error) {
	return r.onReconcileErrorWithRequeue(nm, drainer, ctx, err, nil)
}

func setLastUpdate(nm *v1beta1.NodeMaintenance) {
	nm.Status.LastUpdate.Time = time.Now()
}

// writer implements io.Writer interface as a pass-through for klog.
type writer struct {
	logFunc func(args ...interface{})
}

// Write passes string(p) into writer's logFunc and always returns len(p)
func (w writer) Write(p []byte) (n int, err error) {
	w.logFunc(string(p))
	return len(p), nil
}
