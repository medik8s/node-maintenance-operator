package controllers

import (
	"context"
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

	nodemaintenanceapi "github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

const (
	taintedNodeName = "node01"
	invalidNodeName = "non-existing"
	dummyTaintKey   = "dummy-key"
	testNamespace   = "test-namespace"
	succeedTimeout  = "1s"
	succeedPollTime = "200ms"
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
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(testNs), &corev1.Namespace{}); err != nil {
			Expect(k8sClient.Create(context.Background(), testNs)).To(Succeed())
		}
		nodeOne = getNode(taintedNodeName)
	})
	Context("Functionality test", func() {
		Context("Testing initMaintenanceStatus", func() {
			var nm, nmCopy *nodemaintenanceapi.NodeMaintenance
			BeforeEach(func() {
				Expect(k8sClient.Create(context.Background(), nodeOne)).To(Succeed())
				DeferCleanup(k8sClient.Delete, context.Background(), nodeOne)

				podOne, podTwo = getTestPod("test-pod-1", taintedNodeName), getTestPod("test-pod-2", taintedNodeName)
				Expect(k8sClient.Create(context.Background(), podOne)).To(Succeed())
				Expect(k8sClient.Create(context.Background(), podTwo)).To(Succeed())
				DeferCleanup(cleanupPod, context.Background(), podOne)
				DeferCleanup(cleanupPod, context.Background(), podTwo)
				nm = getTestNM("node-maintenance-cr", taintedNodeName)
			})
			When("Status was initalized", func() {
				It("should be set for running with 2 pods to drain", func() {
					initMaintenanceStatus(nm, r.drainer, r.Client)
					Expect(nm.Status.Phase).To(Equal(nodemaintenanceapi.MaintenanceRunning))
					Expect(len(nm.Status.PendingPods)).To(Equal(2))
					Expect(nm.Status.EvictionPods).To(Equal(2))
					Expect(nm.Status.TotalPods).To(Equal(2))
					Expect(nm.Status.DrainProgress).To(Equal(0))
					Expect(nm.Status.LastUpdate.IsZero()).To(BeFalse())
				})
			})
			When("Owner ref was set", func() {
				It("should be set properly", func() {
					initMaintenanceStatus(nm, r.drainer, r.Client)
					By("Setting owner ref for a modified nm CR")
					node := &corev1.Node{}
					Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					setOwnerRefToNode(nm, node, r.logger)
					Expect(len(nm.ObjectMeta.GetOwnerReferences())).To(Equal(1))
					ref := nm.ObjectMeta.GetOwnerReferences()[0]
					Expect(ref.Name).To(Equal(node.ObjectMeta.Name))
					Expect(ref.UID).To(Equal(node.ObjectMeta.UID))
					Expect(ref.APIVersion).To(Equal(node.TypeMeta.APIVersion))
					Expect(ref.Kind).To(Equal(node.TypeMeta.Kind))

					By("Setting owner ref for an empty nm CR")
					maintenance := &nodemaintenanceapi.NodeMaintenance{}
					setOwnerRefToNode(maintenance, node, r.logger)
					Expect(len(maintenance.ObjectMeta.GetOwnerReferences())).To(Equal(1))
				})
			})
			When("Node Maintenance CR was initalized", func() {
				It("Should block another initalization", func() {
					nmCopy = nm.DeepCopy()
					nmCopy.Status.Phase = nodemaintenanceapi.MaintenanceFailed
					initMaintenanceStatus(nmCopy, r.drainer, r.Client)
					Expect(nmCopy.Status.Phase).NotTo(Equal(nodemaintenanceapi.MaintenanceRunning))
					Expect(len(nmCopy.Status.PendingPods)).NotTo(Equal(2))
					Expect(nmCopy.Status.EvictionPods).NotTo(Equal(2))
					Expect(nmCopy.Status.TotalPods).NotTo(Equal(2))
					Expect(nmCopy.Status.DrainProgress).To(Equal(0))
					Expect(nmCopy.Status.LastUpdate.IsZero()).To(BeTrue())
				})
			})
		})

		Context("Testing exclude remediation label", func() {
			BeforeEach(func() {
				Expect(k8sClient.Create(context.Background(), nodeOne)).To(Succeed())
				DeferCleanup(k8sClient.Delete, context.Background(), nodeOne)
			})
			When("Adding exclude remediation label", func() {
				It("should have the new label", func() {
					node := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					Expect(len(node.Labels)).To(Equal(0))
					Expect(addExcludeRemediationLabel(context.Background(), node, r.Client, testLog)).To(Succeed())
					labeledNode := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, labeledNode)).To(Succeed())
					Expect(isLabelExist(labeledNode, commonLabels.ExcludeFromRemediation)).To(BeTrue())
					Expect(len(labeledNode.Labels)).To(Equal(1))
				})
			})
			When("Adding and removing exclude remediation label", func() {
				It("should keep other exiting labels", func() {
					node := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					Expect(len(node.Labels)).To(Equal(0))
					Expect(addExcludeRemediationLabel(context.Background(), node, r.Client, testLog)).To(Succeed())
					labeledNode := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, labeledNode)).To(Succeed())
					Expect(isLabelExist(labeledNode, commonLabels.ExcludeFromRemediation)).To(BeTrue())
					Expect(len(labeledNode.Labels)).To(Equal(1))
					Expect(removeExcludeRemediationLabel(context.Background(), node, r.Client, testLog)).To(Succeed())
					unlabeledNode := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, unlabeledNode)).To(Succeed())
					Expect(isLabelExist(unlabeledNode, commonLabels.ExcludeFromRemediation)).To(BeFalse())
					Expect(len(unlabeledNode.Labels)).To(Equal(0))
				})
			})
		})
		Context("Testing Taints", func() {
			BeforeEach(func() {
				Expect(k8sClient.Create(context.Background(), nodeOne)).To(Succeed())
				DeferCleanup(k8sClient.Delete, context.Background(), nodeOne)
			})
			When("Adding new taint", func() {
				It("should add new taint and keep other existing taints", func() {
					node := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					Expect(AddOrRemoveTaint(r.drainer.Client, node, true)).To(Succeed())
					taintedNode := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, taintedNode)).To(Succeed())
					Expect(isTaintExist(taintedNode, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeTrue())
					Expect(isTaintExist(taintedNode, NodeUnschedulableTaint.Key, NodeUnschedulableTaint.Effect)).To(BeTrue())
					Expect(isTaintExist(taintedNode, dummyTaintKey, corev1.TaintEffectPreferNoSchedule)).To(BeTrue())
					// there is also a not-ready taint
					Expect(len(taintedNode.Spec.Taints)).To(Equal(4))
				})
			})

			When("Adding and then removing a taint", func() {
				It("should keep other existing taints", func() {
					node := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, node)).To(Succeed())
					Expect(isTaintExist(node, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeFalse())
					Expect(AddOrRemoveTaint(r.drainer.Client, node, true)).To(Succeed())
					taintedNode := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, taintedNode)).To(Succeed())
					Expect(isTaintExist(taintedNode, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeTrue())
					Expect(AddOrRemoveTaint(r.drainer.Client, taintedNode, false)).To(Succeed())
					unTaintedNode := &corev1.Node{}
					Expect(k8sClient.Get(context.Background(), client.ObjectKey{Name: taintedNodeName}, unTaintedNode)).To(Succeed())
					Expect(isTaintExist(unTaintedNode, medik8sDrainTaint.Key, medik8sDrainTaint.Effect)).To(BeFalse())
					Expect(isTaintExist(unTaintedNode, dummyTaintKey, corev1.TaintEffectPreferNoSchedule)).To(BeTrue())
					// there is also a not-ready taint
					Expect(len(unTaintedNode.Spec.Taints)).To(Equal(2))
				})
			})
		})
	})
	Context("Reconciliation", func() {

		It("should reconcile once without failing", func() {
			reconcileMaintenance(nm)
			checkSuccesfulReconcile()
		})

		It("should reconcile and cordon node", func() {
			reconcileMaintenance(nm)
			checkSuccesfulReconcile()
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: nm.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(node.Spec.Unschedulable).To(Equal(true))
		})

		It("should reconcile and taint node", func() {
			reconcileMaintenance(nm)
			checkSuccesfulReconcile()
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: nm.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(taintExist(node, "medik8s.io/drain", corev1.TaintEffectNoSchedule)).To(BeTrue())
		})

		It("should fail on non existing node", func() {
			nmFail := getTestNM()
			nmFail.Spec.NodeName = "non-existing"
			err := k8sClient.Delete(context.TODO(), nm)
			Expect(err).NotTo(HaveOccurred())
			err = k8sClient.Create(context.TODO(), nmFail)
			Expect(err).NotTo(HaveOccurred())
			reconcileMaintenance(nm)
			checkFailedReconcile()
		})

		It("add/remove Exclude remediation label", func() {
			reconcileMaintenance(nm)
			checkSuccesfulReconcile()
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: nm.Spec.NodeName}, node)
			Expect(err).NotTo(HaveOccurred())
			//Label added on CR creation
			Expect(node.Labels[labels.ExcludeFromRemediation]).To(Equal("true"))

			Expect(k8sClient.Delete(context.Background(), nm)).To(Succeed())
			reconcileMaintenance(nm)
			//Re-fetch node after reconcile
			Expect(k8sClient.Get(context.TODO(), client.ObjectKey{Name: nm.Spec.NodeName}, node)).NotTo(HaveOccurred())
			_, exist := node.Labels[labels.ExcludeFromRemediation]
			Expect(exist).To(BeFalse())
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
			Name: nodeName,
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
	if err := k8sClient.Delete(context.Background(), pod, force); err != nil {
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

func getTestNM(crName, nodeName string) *nodemaintenanceapi.NodeMaintenance {
	return &nodemaintenanceapi.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name: crName,
		},
		Spec: nodemaintenanceapi.NodeMaintenanceSpec{
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
