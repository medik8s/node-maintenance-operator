package controllers

import (
	"context"
	"fmt"
	"reflect"
	"time"

	commonLabels "github.com/medik8s/common/pkg/labels"
	"github.com/medik8s/common/pkg/lease"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

const (
	taintedNodeName = "node01"
	invalidNodeName = "non-existing"
	dummyTaintKey   = "dummy-key"
	dummyLabelKey   = "dummyLabel"
	dummyLabelValue = "true"
	testNamespace   = "test-namespace"
)

var testLog = ctrl.Log.WithName("nmo-controllers-unit-test")
var _ = Describe("Node Maintenance", func() {

	var (
		nodeOne        *corev1.Node
		podOne, podTwo *corev1.Pod
	)

	BeforeEach(func() {
		// create testNamespace ns
		testNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: testNamespace,
			},
		}
		if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testNs), &corev1.Namespace{}); err != nil {
			Expect(k8sClient.Create(ctx, testNs)).To(Succeed())
		}
		nodeOne = getNode(taintedNodeName)
	})
	Context("Functionality test", func() {
		Context("Testing initMaintenanceStatus", func() {
			var nm *v1beta1.NodeMaintenance
			BeforeEach(func() {
				Expect(k8sClient.Create(ctx, nodeOne)).To(Succeed())
				DeferCleanup(k8sClient.Delete, ctx, nodeOne)

				podOne, podTwo = getTestPod("test-pod-1", taintedNodeName), getTestPod("test-pod-2", taintedNodeName)
				Expect(k8sClient.Create(ctx, podOne)).To(Succeed())
				Expect(k8sClient.Create(ctx, podTwo)).To(Succeed())
				DeferCleanup(cleanupPod, ctx, podOne)
				DeferCleanup(cleanupPod, ctx, podTwo)
				nm = getTestNM("node-maintenance-cr-initMaintenanceStatus", taintedNodeName)
			})
			When("Status was initalized", func() {
				It("should be set for running with 2 pods to drain", func() {
					Expect(initMaintenanceStatus(nm, r.drainer, r.Client)).To(HaveOccurred())
					// status was initialized but the function will fail on updating the CR status, since we don't create a nm CR here
					Expect(nm.Status.Phase).To(Equal(v1beta1.MaintenanceRunning))
					Expect(len(nm.Status.PendingPods)).To(Equal(2))
					Expect(nm.Status.EvictionPods).To(Equal(2))
					Expect(nm.Status.TotalPods).To(Equal(2))
					Expect(nm.Status.DrainProgress).To(Equal(0))
					Expect(nm.Status.LastUpdate).NotTo(BeZero())
				})
			})
			When("Owner ref was set", func() {
				It("should be set properly", func() {
					Expect(initMaintenanceStatus(nm, r.drainer, r.Client)).To(HaveOccurred())
					// status was initialized but the function will fail on updating the CR status, since we don't create a nm CR here
					By("Setting owner ref for a modified nm CR")
					node := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					setOwnerRefToNode(nm, node, r.logger)
					Expect(nm.ObjectMeta.GetOwnerReferences()).To(HaveLen(1))
					ref := nm.ObjectMeta.GetOwnerReferences()[0]
					Expect(ref.Name).To(Equal(node.ObjectMeta.Name))
					Expect(ref.UID).To(Equal(node.ObjectMeta.UID))
					Expect(ref.APIVersion).To(Equal(node.TypeMeta.APIVersion))
					Expect(ref.Kind).To(Equal(node.TypeMeta.Kind))

					By("Setting owner ref for an empty nm CR")
					maintenance := &v1beta1.NodeMaintenance{}
					setOwnerRefToNode(maintenance, node, r.logger)
					Expect(maintenance.ObjectMeta.GetOwnerReferences()).To(HaveLen(1))
				})
			})
			When("Node Maintenance CR was initalized", func() {
				It("Should not modify the CR after initalization", func() {
					nmCopy := nm.DeepCopy()
					nmCopy.Status.Phase = v1beta1.MaintenanceFailed
					Expect(initMaintenanceStatus(nmCopy, r.drainer, r.Client)).To(Succeed())
					// status was not initialized thus the function succeeds
					Expect(nmCopy.Status.Phase).To(Equal(v1beta1.MaintenanceFailed))
					Expect(len(nmCopy.Status.PendingPods)).To(Equal(0))
					Expect(nmCopy.Status.EvictionPods).To(Equal(0))
					Expect(nmCopy.Status.TotalPods).To(Equal(0))
					Expect(nmCopy.Status.DrainProgress).To(Equal(0))
					Expect(nmCopy.Status.LastUpdate).To(BeZero())
				})
			})
		})

		Context("Testing exclude remediation label", func() {
			BeforeEach(func() {
				Expect(k8sClient.Create(ctx, nodeOne)).To(Succeed())
				DeferCleanup(k8sClient.Delete, ctx, nodeOne)
			})
			When("Adding and removing exclude remediation label", func() {
				It("should keep the dummy label", func() {
					node := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					Expect(len(node.Labels)).To(Equal(1))
					By("Adding exclude remediation label")
					Expect(addExcludeRemediationLabel(ctx, node, r.Client, testLog)).To(Succeed())
					labeledNode := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, labeledNode)).To(Succeed())
					Expect(isLabelExist(labeledNode, commonLabels.ExcludeFromRemediation)).To(BeTrue())
					Expect(len(labeledNode.Labels)).To(Equal(2))
					By("Removing exclude remediation label")
					Expect(removeExcludeRemediationLabel(ctx, node, r.Client, testLog)).To(Succeed())
					unlabeledNode := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, unlabeledNode)).To(Succeed())
					Expect(isLabelExist(unlabeledNode, commonLabels.ExcludeFromRemediation)).To(BeFalse())
					By("Finding dummy label")
					Expect(isLabelExist(unlabeledNode, dummyLabelKey)).To(BeTrue())
					Expect(len(unlabeledNode.Labels)).To(Equal(1))
				})
			})
		})
		Context("Testing Taints", func() {
			BeforeEach(func() {
				Expect(k8sClient.Create(ctx, nodeOne)).To(Succeed())
				DeferCleanup(k8sClient.Delete, ctx, nodeOne)
			})
			When("Adding and then removing a taint", func() {
				It("should keep the dummy taint", func() {
					node := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					Expect(isTaintExist(node, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeFalse())
					By("Adding drain taint")
					Expect(AddOrRemoveTaint(r.drainer.Client, node, true)).To(Succeed())
					taintedNode := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, taintedNode)).To(Succeed())
					Expect(isTaintExist(taintedNode, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeTrue())
					By("Removing drain taint")
					Expect(AddOrRemoveTaint(r.drainer.Client, taintedNode, false)).To(Succeed())
					unTaintedNode := &corev1.Node{}
					Expect(k8sClient.Get(ctx, client.ObjectKey{Name: taintedNodeName}, unTaintedNode)).To(Succeed())
					Expect(isTaintExist(unTaintedNode, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeFalse())
					By("Finding dummy taint")
					Expect(isTaintExist(unTaintedNode, dummyTaintKey, corev1.TaintEffectPreferNoSchedule)).To(BeTrue())
				})
			})
		})
	})

	Context("Reconciliation", func() {
		var nm *v1beta1.NodeMaintenance
		BeforeEach(func() {
			Expect(k8sClient.Create(ctx, nodeOne)).To(Succeed())
			DeferCleanup(k8sClient.Delete, ctx, nodeOne)

			podOne, podTwo = getTestPod("test-pod-1", taintedNodeName), getTestPod("test-pod-2", taintedNodeName)
			Expect(k8sClient.Create(ctx, podOne)).To(Succeed())
			Expect(k8sClient.Create(ctx, podTwo)).To(Succeed())
			DeferCleanup(cleanupPod, ctx, podOne)
			DeferCleanup(cleanupPod, ctx, podTwo)
		})
		JustBeforeEach(func() {
			// Sleep for a second to ensure dummy reconciliation has begun running before the unit tests
			time.Sleep(1 * time.Second)
		})

		When("nm CR is valid", func() {
			BeforeEach(func() {
				nm = getTestNM("node-maintenance-cr", taintedNodeName)
				Expect(k8sClient.Create(ctx, nm)).To(Succeed())
			})
			It("should cordon node and add proper taints", func() {
				By("check nm CR status was success")
				maintenance := &v1beta1.NodeMaintenance{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nm), maintenance)).To(Succeed())

				Expect(maintenance.Status.Phase).To(Equal(v1beta1.MaintenanceSucceeded))
				Expect(len(maintenance.Status.PendingPods)).To(Equal(0))
				Expect(maintenance.Status.EvictionPods).To(Equal(2))
				Expect(maintenance.Status.TotalPods).To(Equal(2))
				Expect(maintenance.Status.DrainProgress).To(Equal(100))
				Expect(maintenance.Status.LastError).To(Equal(""))
				Expect(maintenance.Status.LastUpdate).NotTo(BeZero())
				Expect(maintenance.Status.ErrorOnLeaseCount).To(Equal(0))

				By("Check whether node was cordoned")
				node := &corev1.Node{}
				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: nm.Spec.NodeName}, node)).To(Succeed())
				Expect(node.Spec.Unschedulable).To(Equal(true))

				By("Check node taints")
				Expect(isTaintExist(node, medik8sDrainTaint.Key, corev1.TaintEffectNoSchedule)).To(BeTrue())
				Expect(isTaintExist(node, nodeUnschedulableTaint.Key, corev1.TaintEffectNoSchedule)).To(BeTrue())

				By("Check add/remove Exclude remediation label")
				// Label added on CR creation
				Expect(node.Labels[commonLabels.ExcludeFromRemediation]).To(Equal("true"))
				// Re-fetch node after nm CR deletion
				Expect(k8sClient.Delete(ctx, nm)).To(Succeed())
				// Sleep for a second to ensure dummy reconciliation has begun running before the unit tests
				time.Sleep(1 * time.Second)

				Expect(k8sClient.Get(ctx, client.ObjectKey{Name: nm.Spec.NodeName}, node)).NotTo(HaveOccurred())
				_, exist := node.Labels[commonLabels.ExcludeFromRemediation]
				Expect(exist).To(BeFalse())
			})
		})
		When("nm CR is invalid for a missing node", func() {
			BeforeEach(func() {
				nm = getTestNM("non-existing-node-cr", invalidNodeName)
				Expect(k8sClient.Create(ctx, nm)).To(Succeed())
				DeferCleanup(k8sClient.Delete, ctx, nm)
			})
			It("should fail on non existing node", func() {
				By("check nm CR status and whether LastError was updated")
				maintenance := &v1beta1.NodeMaintenance{}
				Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(nm), maintenance)).To(Succeed())

				Expect(maintenance.Status.Phase).To(Equal(v1beta1.MaintenanceRunning))
				Expect(len(maintenance.Status.PendingPods)).To(Equal(0))
				Expect(maintenance.Status.EvictionPods).To(Equal(0))
				Expect(maintenance.Status.TotalPods).To(Equal(0))
				Expect(maintenance.Status.DrainProgress).To(Equal(0))
				Expect(maintenance.Status.LastError).To(Equal(fmt.Sprintf("nodes \"%s\" not found", invalidNodeName)))
				Expect(maintenance.Status.LastUpdate).NotTo(BeZero())
				Expect(maintenance.Status.ErrorOnLeaseCount).To(Equal(0))
			})
		})
	})
})

// isLabelExist checks whether a node label has the value true
func isLabelExist(node *corev1.Node, label string) bool {
	return node.Labels[label] == "true"
}

// isTaintExist checks whether a node has a specific taint
func isTaintExist(node *corev1.Node, key string, effect corev1.TaintEffect) bool {
	checkTaint := corev1.Taint{
		Key:    key,
		Effect: effect,
	}
	taints := node.Spec.Taints
	for _, taint := range taints {
		if reflect.DeepEqual(taint, checkTaint) {
			return true
		}
	}
	return false
}

func getNode(nodeName string) *corev1.Node {
	return &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   nodeName,
			Labels: map[string]string{dummyLabelKey: dummyLabelValue},
		},
		Spec: corev1.NodeSpec{
			Taints: []corev1.Taint{{
				Key:    dummyTaintKey,
				Effect: corev1.TaintEffectPreferNoSchedule},
			},
		},
	}
}

func getTestPod(podName, nodeName string) *corev1.Pod {
	return &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: testNamespace,
			Name:      podName,
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				{
					Name:  "c1",
					Image: "i1",
				},
			},
			TerminationGracePeriodSeconds: ptr.To[int64](0),
		},
		Status: corev1.PodStatus{
			Conditions: []corev1.PodCondition{
				{
					Type:   corev1.PodReady,
					Status: corev1.ConditionTrue,
				},
			},
		},
	}
}

// cleanupPod deletes the pod only if it exists
func cleanupPod(ctx context.Context, pod *corev1.Pod) {

	podErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})
	if podErr != nil {
		// There is no to pod to delete/clean
		testLog.Info("Cleanup: pods is missing", "pod", pod.Name)
		return
	}
	var force client.GracePeriodSeconds = 0
	if err := k8sClient.Delete(ctx, pod, force); err != nil {
		if !apierrors.IsNotFound(err) {
			ConsistentlyWithOffset(1, func() error {
				podErr := k8sClient.Get(ctx, client.ObjectKeyFromObject(pod), &corev1.Pod{})
				if apierrors.IsNotFound(podErr) {
					testLog.Info("Cleanup: Got error 404", "name", pod.Name)
					return nil
				}
				return podErr
			}, "4s", "100ms").Should(BeNil(), "pod should be deleted")
		}
	}
}

func getTestNM(crName, nodeName string) *v1beta1.NodeMaintenance {
	return &v1beta1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name: crName,
		},
		Spec: v1beta1.NodeMaintenanceSpec{
			NodeName: nodeName,
			Reason:   "test reason",
		},
	}
}

type mockLeaseManager struct {
	lease.Manager
}

func (mock *mockLeaseManager) RequestLease(_ context.Context, _ client.Object, _ time.Duration) error {
	return nil
}
