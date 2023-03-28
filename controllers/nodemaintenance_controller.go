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
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/medik8s/common/pkg/lease"
	nodemaintenancev1beta1 "github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

const (
	MaxAllowedErrorToUpdateOwnedLease = 3
	WaitDurationOnDrainError          = 5 * time.Second
	FixedDurationReconcileLog         = "Reconciling with fixed duration"

	//lease params
	leaseNamespaceDefault = "node-maintenance"
	LeaseHolderIdentity   = "node-maintenance"
	LeaseDuration         = 3600 * time.Second
	DrainerTimeout        = 30 * time.Second
)

var (
	LeaseNamespace = leaseNamespaceDefault
)

// NodeMaintenanceReconciler reconciles a NodeMaintenance object
type NodeMaintenanceReconciler struct {
	client.Client
	Scheme       *runtime.Scheme
	LeaseManager lease.Manager
	drainer      *drain.Helper
	logger       logr.Logger
}

//+kubebuilder:rbac:groups=nodemaintenance.medik8s.io,resources=nodemaintenances,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=nodemaintenance.medik8s.io,resources=nodemaintenances/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=nodemaintenance.medik8s.io,resources=nodemaintenances/finalizers,verbs=update

// TODO check if all these are really needed!
//+kubebuilder:rbac:groups="",resources=nodes,verbs=get;list;update;patch;watch
//+kubebuilder:rbac:groups="",resources=pods,verbs=get;list;watch
//+kubebuilder:rbac:groups="",resources=pods/eviction,verbs=create
//+kubebuilder:rbac:groups="",resources=namespaces,verbs=get
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

	// Fetch the NodeMaintenance instance
	instance := &nodemaintenancev1beta1.NodeMaintenance{}
	err := r.Client.Get(context.TODO(), req.NamespacedName, instance)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			r.logger.Info("NodeMaintenance not found", "name", req.NamespacedName)
			return reconcile.Result{}, nil
		}
		// Error reading the object - requeue the request.
		r.logger.Info("Error reading the request object, requeuing.")
		return reconcile.Result{}, err
	}

	// Add finalizer when object is created
	if instance.ObjectMeta.DeletionTimestamp.IsZero() {
		if !ContainsString(instance.ObjectMeta.Finalizers, nodemaintenancev1beta1.NodeMaintenanceFinalizer) {
			instance.ObjectMeta.Finalizers = append(instance.ObjectMeta.Finalizers, nodemaintenancev1beta1.NodeMaintenanceFinalizer)
			if err := r.Client.Update(context.TODO(), instance); err != nil {
				return r.onReconcileError(instance, err)
			}
		}
	} else {
		r.logger.Info("Deletion timestamp not zero")

		// The object is being deleted
		if ContainsString(instance.ObjectMeta.Finalizers, nodemaintenancev1beta1.NodeMaintenanceFinalizer) || ContainsString(instance.ObjectMeta.Finalizers, metav1.FinalizerOrphanDependents) {
			// Stop node maintenance - uncordon and remove live migration taint from the node.
			if err := r.stopNodeMaintenanceOnDeletion(instance.Spec.NodeName); err != nil {
				r.logger.Error(err, "error stopping node maintenance")
				if errors.IsNotFound(err) == false {
					return r.onReconcileError(instance, err)
				}
			}

			// Remove our finalizer from the list and update it.
			instance.ObjectMeta.Finalizers = RemoveString(instance.ObjectMeta.Finalizers, nodemaintenancev1beta1.NodeMaintenanceFinalizer)
			if err := r.Client.Update(context.Background(), instance); err != nil {
				return r.onReconcileError(instance, err)
			}
		}
		return reconcile.Result{}, nil
	}

	err = r.initMaintenanceStatus(instance)
	if err != nil {
		r.logger.Error(err, "Failed to update NodeMaintenance with \"Running\" status")
		return r.onReconcileError(instance, err)
	}

	nodeName := instance.Spec.NodeName

	r.logger.Info("Applying maintenance mode", "node", nodeName, "reason", instance.Spec.Reason)
	node, err := r.fetchNode(nodeName)
	if err != nil {
		return r.onReconcileError(instance, err)
	}

	r.setOwnerRefToNode(instance, node)

	updateOwnedLeaseFailed, err := r.obtainLease(node)
	if err != nil && updateOwnedLeaseFailed {
		instance.Status.ErrorOnLeaseCount += 1
		if instance.Status.ErrorOnLeaseCount > MaxAllowedErrorToUpdateOwnedLease {
			r.logger.Info("can't extend owned lease. uncordon for now")

			// Uncordon the node
			err = r.stopNodeMaintenanceImp(node)
			if err != nil {
				return r.onReconcileError(instance, fmt.Errorf("Failed to uncordon upon failure to obtain owned lease : %v ", err))
			}
			instance.Status.Phase = nodemaintenancev1beta1.MaintenanceFailed
		}
		return r.onReconcileError(instance, fmt.Errorf("Failed to extend lease owned by us : %v errorOnLeaseCount %d", err, instance.Status.ErrorOnLeaseCount))
	}
	if err != nil {
		instance.Status.ErrorOnLeaseCount = 0
		return r.onReconcileError(instance, err)
	} else {
		if instance.Status.Phase != nodemaintenancev1beta1.MaintenanceRunning || instance.Status.ErrorOnLeaseCount != 0 {
			instance.Status.Phase = nodemaintenancev1beta1.MaintenanceRunning
			instance.Status.ErrorOnLeaseCount = 0
		}
	}

	// Cordon node
	err = AddOrRemoveTaint(r.drainer.Client, node, true)
	if err != nil {
		return r.onReconcileError(instance, err)
	}

	if err = drain.RunCordonOrUncordon(r.drainer, node, true); err != nil {
		return r.onReconcileError(instance, err)
	}

	r.logger.Info("Evict all Pods from Node", "nodeName", nodeName)

	if err = drain.RunNodeDrain(r.drainer, nodeName); err != nil {
		r.logger.Info("Not all pods evicted", "nodeName", nodeName, "error", err)
		waitOnReconcile := WaitDurationOnDrainError
		return r.onReconcileErrorWithRequeue(instance, err, &waitOnReconcile)
	} else if instance.Status.Phase != nodemaintenancev1beta1.MaintenanceSucceeded {
		setLastUpdate(instance)
	}
	r.logger.Info("All pods evicted", "nodeName", nodeName)

	instance.Status.Phase = nodemaintenancev1beta1.MaintenanceSucceeded
	instance.Status.DrainProgress = 100
	instance.Status.PendingPods = nil
	err = r.Client.Status().Update(context.TODO(), instance)
	if err != nil {
		r.logger.Error(err, "Failed to update NodeMaintenance with \"Succeeded\" status")
		return r.onReconcileError(instance, err)
	}
	r.logger.Info("Reconcile completed", "nodeName", nodeName)

	return reconcile.Result{}, nil

}

// SetupWithManager sets up the controller with the Manager.
func (r *NodeMaintenanceReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := initDrainer(r, mgr.GetConfig()); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&nodemaintenancev1beta1.NodeMaintenance{}).
		Complete(r)
}

func onPodDeletedOrEvicted(pod *corev1.Pod, usingEviction bool) {
	var verbString string
	if usingEviction {
		verbString = "Evicted"
	} else {
		verbString = "Deleted"
	}
	msg := fmt.Sprintf("pod: %s:%s %s from node: %s", pod.ObjectMeta.Namespace, pod.ObjectMeta.Name, verbString, pod.Spec.NodeName)
	klog.Info(msg)
}

func SetLeaseNamespace(namespace string) {
	LeaseNamespace = namespace
}

func initDrainer(r *NodeMaintenanceReconciler, config *rest.Config) error {

	r.drainer = &drain.Helper{}

	//Continue even if there are pods not managed by a ReplicationController, ReplicaSet, Job, DaemonSet or StatefulSet.
	//This is required because VirtualMachineInstance pods are not owned by a ReplicaSet or DaemonSet controller.
	//This means that the drain operation can’t guarantee that the pods being terminated on the target node will get
	//re-scheduled replacements placed else where in the cluster after the pods are evicted.
	//medik8s has its own controllers which manage the underlying VirtualMachineInstance pods.
	//Each controller behaves differently to a VirtualMachineInstance being evicted.
	r.drainer.Force = true

	//Continue even if there are pods using emptyDir (local data that will be deleted when the node is drained).
	//This is necessary for removing any pod that utilizes an emptyDir volume.
	//The VirtualMachineInstance Pod does use emptryDir volumes,
	//however the data in those volumes are ephemeral which means it is safe to delete after termination.
	r.drainer.DeleteEmptyDirData = true

	//Ignore DaemonSet-managed pods.
	//This is required because every node running a VirtualMachineInstance will also be running our helper DaemonSet called virt-handler.
	//This flag indicates that it is safe to proceed with the eviction and to just ignore DaemonSets.
	r.drainer.IgnoreAllDaemonSets = true

	//Period of time in seconds given to each pod to terminate gracefully. If negative, the default value specified in the pod will be used.
	r.drainer.GracePeriodSeconds = -1

	// TODO - add logical value or attach from the maintenance CR
	//The length of time to wait before giving up, zero means infinite
	r.drainer.Timeout = DrainerTimeout

	cs, err := kubernetes.NewForConfig(config)
	if err != nil {
		return err
	}
	r.drainer.Client = cs
	r.drainer.DryRunStrategy = util.DryRunNone
	r.drainer.Ctx = context.Background()

	r.drainer.Out = writer{klog.Info}
	r.drainer.ErrOut = writer{klog.Error}
	r.drainer.OnPodDeletedOrEvicted = onPodDeletedOrEvicted
	return nil
}

func (r *NodeMaintenanceReconciler) setOwnerRefToNode(instance *nodemaintenancev1beta1.NodeMaintenance, node *corev1.Node) {

	for _, ref := range instance.ObjectMeta.GetOwnerReferences() {
		if ref.APIVersion == node.TypeMeta.APIVersion && ref.Kind == node.TypeMeta.Kind && ref.Name == node.ObjectMeta.GetName() && ref.UID == node.ObjectMeta.GetUID() {
			return
		}
	}

	r.logger.Info("setting owner ref to node")

	nodeMeta := node.TypeMeta
	ref := metav1.OwnerReference{
		APIVersion:         nodeMeta.APIVersion,
		Kind:               nodeMeta.Kind,
		Name:               node.ObjectMeta.GetName(),
		UID:                node.ObjectMeta.GetUID(),
		BlockOwnerDeletion: pointer.Bool(false),
		Controller:         pointer.Bool(false),
	}

	instance.ObjectMeta.SetOwnerReferences(append(instance.ObjectMeta.GetOwnerReferences(), ref))
}

func (r *NodeMaintenanceReconciler) obtainLease(node *corev1.Node) (bool, error) {
	r.logger.Info("Lease object supported, obtaining lease")
	lease, needUpdate, err := r.LeaseManager.CreateOrGetLease(context.Background(), node, LeaseDuration, LeaseHolderIdentity, LeaseNamespace)

	if err != nil {
		r.logger.Error(err, "failed to create or get existing lease")
		return false, err
	}

	if needUpdate {

		r.logger.Info("update lease")

		now := metav1.NowMicro()
		if updateOwnedLeaseFailed, err := r.LeaseManager.UpdateLease(context.Background(), node, lease, &now, LeaseDuration, DrainerTimeout, LeaseHolderIdentity); err != nil {
			return updateOwnedLeaseFailed, err
		}
	}

	return false, nil
}
func (r *NodeMaintenanceReconciler) stopNodeMaintenanceImp(node *corev1.Node) error {
	// Uncordon the node
	err := AddOrRemoveTaint(r.drainer.Client, node, false)
	if err != nil {
		return err
	}

	if err = drain.RunCordonOrUncordon(r.drainer, node, false); err != nil {
		return err
	}

	return nil
}

func (r *NodeMaintenanceReconciler) stopNodeMaintenanceOnDeletion(nodeName string) error {
	node, err := r.fetchNode(nodeName)
	if err != nil {
		// if CR is gathered as result of garbage collection: the node may have been deleted, but the CR has not yet been deleted, still we must clean up the lease!
		if errors.IsNotFound(err) {
			return nil
		}
		return err
	}
	return r.stopNodeMaintenanceImp(node)
}

func (r *NodeMaintenanceReconciler) fetchNode(nodeName string) (*corev1.Node, error) {
	node, err := r.drainer.Client.CoreV1().Nodes().Get(context.Background(), nodeName, metav1.GetOptions{})
	if err != nil && errors.IsNotFound(err) {
		r.logger.Error(err, "Node cannot be found", "nodeName", nodeName)
		return nil, err
	} else if err != nil {
		r.logger.Error(err, "Failed to get node", "nodeName", nodeName)
		return nil, err
	}
	return node, nil
}

func (r *NodeMaintenanceReconciler) initMaintenanceStatus(nm *nodemaintenancev1beta1.NodeMaintenance) error {
	if nm.Status.Phase == "" {
		nm.Status.Phase = nodemaintenancev1beta1.MaintenanceRunning
		setLastUpdate(nm)
		pendingList, errlist := r.drainer.GetPodsForDeletion(nm.Spec.NodeName)
		if errlist != nil {
			return fmt.Errorf("Failed to get pods for eviction while initializing status")
		}
		if pendingList != nil {
			nm.Status.PendingPods = GetPodNameList(pendingList.Pods())
		}
		nm.Status.EvictionPods = len(nm.Status.PendingPods)

		podlist, err := r.drainer.Client.CoreV1().Pods(metav1.NamespaceAll).List(
			context.Background(),
			metav1.ListOptions{
				FieldSelector: fields.SelectorFromSet(fields.Set{"spec.nodeName": nm.Spec.NodeName}).String(),
			})
		if err != nil {
			return err
		}
		nm.Status.TotalPods = len(podlist.Items)
		err = r.Client.Status().Update(context.TODO(), nm)
		return err
	}
	return nil
}

func (r *NodeMaintenanceReconciler) onReconcileErrorWithRequeue(nm *nodemaintenancev1beta1.NodeMaintenance, err error, duration *time.Duration) (reconcile.Result, error) {
	nm.Status.LastError = err.Error()
	setLastUpdate(nm)

	if nm.Spec.NodeName != "" {
		pendingList, _ := r.drainer.GetPodsForDeletion(nm.Spec.NodeName)
		if pendingList != nil {
			nm.Status.PendingPods = GetPodNameList(pendingList.Pods())
			if nm.Status.EvictionPods != 0 {
				nm.Status.DrainProgress = (nm.Status.EvictionPods - len(nm.Status.PendingPods)) * 100 / nm.Status.EvictionPods
			}
		}
	}

	updateErr := r.Client.Status().Update(context.TODO(), nm)
	if updateErr != nil {
		r.logger.Error(updateErr, "Failed to update NodeMaintenance with \"Failed\" status")
	}
	if duration != nil {
		r.logger.Info(FixedDurationReconcileLog)
		return reconcile.Result{RequeueAfter: *duration}, nil
	}
	r.logger.Info("Reconciling with exponential duration")
	return reconcile.Result{}, err
}

func (r *NodeMaintenanceReconciler) onReconcileError(nm *nodemaintenancev1beta1.NodeMaintenance, err error) (reconcile.Result, error) {
	return r.onReconcileErrorWithRequeue(nm, err, nil)

}

func setLastUpdate(nm *nodemaintenancev1beta1.NodeMaintenance) {
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
