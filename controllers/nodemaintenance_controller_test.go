package controllers

import (
	"context"
	"reflect"
	"time"

	"github.com/medik8s/common/pkg/lease"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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
		nodeOne *corev1.Node
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
		Context("Initialization test", func() {

			It("Node maintenance should be initialized properly", func() {
				_ = r.initMaintenanceStatus(nm)
				maintenance := &nodemaintenanceapi.NodeMaintenance{}
				err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nm), maintenance)
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenance.Status.Phase).To(Equal(nodemaintenanceapi.MaintenanceRunning))
				Expect(len(maintenance.Status.PendingPods)).To(Equal(2))
				Expect(maintenance.Status.EvictionPods).To(Equal(2))
				Expect(maintenance.Status.TotalPods).To(Equal(2))
				Expect(maintenance.Status.DrainProgress).To(Equal(0))
				Expect(maintenance.Status.LastUpdate.IsZero()).To(BeFalse())
			})

			It("owner ref should be set properly", func() {
				_ = r.initMaintenanceStatus(nm)
				maintenance := &nodemaintenanceapi.NodeMaintenance{}
				err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nm), maintenance)
				node := &corev1.Node{}
				err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, node)
				Expect(err).ToNot(HaveOccurred())
				r.setOwnerRefToNode(maintenance, node)
				Expect(len(maintenance.ObjectMeta.GetOwnerReferences())).To(Equal(1))
				ref := maintenance.ObjectMeta.GetOwnerReferences()[0]
				Expect(ref.Name).To(Equal(node.ObjectMeta.Name))
				Expect(ref.UID).To(Equal(node.ObjectMeta.UID))
				Expect(ref.APIVersion).To(Equal(node.TypeMeta.APIVersion))
				Expect(ref.Kind).To(Equal(node.TypeMeta.Kind))
				r.setOwnerRefToNode(maintenance, node)
				Expect(len(maintenance.ObjectMeta.GetOwnerReferences())).To(Equal(1))
			})

			It("Should not init Node maintenance if already set", func() {
				nmCopy := nm.DeepCopy()
				nmCopy.Status.Phase = nodemaintenanceapi.MaintenanceRunning
				_ = r.initMaintenanceStatus(nmCopy)
				maintenance := &nodemaintenanceapi.NodeMaintenance{}
				err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nm), maintenance)
				Expect(err).NotTo(HaveOccurred())
				Expect(maintenance.Status.Phase).NotTo(Equal(nodemaintenanceapi.MaintenanceRunning))
				Expect(len(maintenance.Status.PendingPods)).NotTo(Equal(2))
				Expect(maintenance.Status.EvictionPods).NotTo(Equal(2))
				Expect(maintenance.Status.TotalPods).NotTo(Equal(2))
				Expect(maintenance.Status.DrainProgress).To(Equal(0))
				Expect(maintenance.Status.LastUpdate.IsZero()).To(BeTrue())
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

type mockLeaseManager struct {
	lease.Manager
}

func (mock *mockLeaseManager) RequestLease(_ context.Context, _ client.Object, _ time.Duration) error {
	return nil
}
