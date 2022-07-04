package controllers

import (
	"context"
	"reflect"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/utils/pointer"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	nodemaintenanceapi "github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

var _ = Describe("NodeMaintenance", func() {

	var r *NodeMaintenanceReconciler
	var nm *nodemaintenanceapi.NodeMaintenance
	var req reconcile.Request
	var clObjs []client.Object

	checkSuccesfulReconcile := func() {
		maintenance := &nodemaintenanceapi.NodeMaintenance{}
		err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nm), maintenance)
		Expect(err).NotTo(HaveOccurred())
		Expect(maintenance.Status.Phase).To(Equal(nodemaintenanceapi.MaintenanceSucceeded))
		Expect(maintenance.Status.DrainProgress).To(Equal(100))
	}

	checkFailedReconcile := func() {
		maintenance := &nodemaintenanceapi.NodeMaintenance{}
		err := k8sClient.Get(context.TODO(), client.ObjectKeyFromObject(nm), maintenance)
		Expect(err).NotTo(HaveOccurred())
		Expect(len(maintenance.Status.LastError)).NotTo(Equal(0))
	}

	reconcileMaintenance := func(nm *nodemaintenanceapi.NodeMaintenance) {
		r.Reconcile(context.Background(), req)
	}

	taintExist := func(node *corev1.Node, key string, effect corev1.TaintEffect) bool {
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

	BeforeEach(func() {

		startTestEnv()

		// Create a ReconcileNodeMaintenance object with the scheme and fake client
		// TODO add reconciler to manager in suite_test.go and don't call reconcile funcs manually
		r = &NodeMaintenanceReconciler{
			Client: k8sClient,
			Scheme: scheme.Scheme,
			logger: ctrl.Log.WithName("unit test"),
		}
		initDrainer(r, cfg)

		// in test pods are not evicted, so don't wait forever for them
		r.drainer.SkipWaitForDeleteTimeoutSeconds = 0

		var objs []client.Object
		nm, objs = getTestObjects()
		clObjs = append(objs, nm)

		// create test ns on 1st run
		testNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test",
			},
		}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(testNs), &corev1.Namespace{}); err != nil {
			err := k8sClient.Create(context.Background(), testNs)
			Expect(err).ToNot(HaveOccurred())
		}

		for _, o := range clObjs {
			err := k8sClient.Create(context.Background(), o)
			Expect(err).ToNot(HaveOccurred())
		}

		// Mock request to simulate Reconcile() being called on an event for a watched resource .
		req = reconcile.Request{
			NamespacedName: types.NamespacedName{
				Name: nm.ObjectMeta.Name,
			},
		}
	})

	AfterEach(func() {
		stopTestEnv()
	})

	Context("Node maintenance controller initialization test", func() {

		It("Node maintenance should be initialized properly", func() {
			r.initMaintenanceStatus(nm)
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
			r.initMaintenanceStatus(nm)
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
			r.initMaintenanceStatus(nmCopy)
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

	Context("Node maintenance controller taint function test", func() {
		It("should add medik8s NoSchedule taint and keep other existing taints", func() {
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, node)
			Expect(err).NotTo(HaveOccurred())
			AddOrRemoveTaint(r.drainer.Client, node, true)
			taintedNode := &corev1.Node{}
			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, taintedNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(taintExist(taintedNode, "medik8s.io/drain", corev1.TaintEffectNoSchedule)).To(BeTrue())
			Expect(taintExist(taintedNode, "node.kubernetes.io/unschedulable", corev1.TaintEffectNoSchedule)).To(BeTrue())
			Expect(taintExist(taintedNode, "test", corev1.TaintEffectPreferNoSchedule)).To(BeTrue())

			// there is a not-ready taint now as well... skip count tests
			//Expect(len(taintedNode.Spec.Taints)).To(Equal(3))
		})

		It("should remove medik8s NoSchedule taint and keep other existing taints", func() {
			node := &corev1.Node{}
			err := k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, node)
			Expect(err).NotTo(HaveOccurred())
			Expect(taintExist(node, "medik8s.io/drain", corev1.TaintEffectNoSchedule)).To(BeFalse())
			AddOrRemoveTaint(r.drainer.Client, node, true)
			taintedNode := &corev1.Node{}
			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, taintedNode)
			Expect(err).ToNot(HaveOccurred())
			Expect(taintExist(taintedNode, "medik8s.io/drain", corev1.TaintEffectNoSchedule)).To(BeTrue())
			AddOrRemoveTaint(r.drainer.Client, taintedNode, false)
			unTaintedNode := &corev1.Node{}
			err = k8sClient.Get(context.TODO(), client.ObjectKey{Name: "node01"}, unTaintedNode)
			Expect(err).NotTo(HaveOccurred())
			Expect(taintExist(unTaintedNode, "medik8s.io/drain", corev1.TaintEffectNoSchedule)).To(BeFalse())
			Expect(taintExist(unTaintedNode, "test", corev1.TaintEffectPreferNoSchedule)).To(BeTrue())

			//Expect(len(unTaintedNode.Spec.Taints)).To(Equal(1))
		})
	})

	Context("Node maintenance controller reconciles a maintenance CR for a node in the cluster", func() {

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

	})
})

func getTestObjects() (*nodemaintenanceapi.NodeMaintenance, []client.Object) {
	nm := getTestNM()

	return nm, []client.Object{
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node01",
			},
			Spec: corev1.NodeSpec{
				Taints: []corev1.Taint{{
					Key:    "test",
					Effect: corev1.TaintEffectPreferNoSchedule},
				},
			},
		},
		&corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "node02",
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "test-pod-1",
			},
			Spec: corev1.PodSpec{
				NodeName: "node01",
				Containers: []corev1.Container{
					{
						Name:  "c1",
						Image: "i1",
					},
				},
				TerminationGracePeriodSeconds: pointer.Int64Ptr(0),
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
		&corev1.Pod{
			ObjectMeta: metav1.ObjectMeta{
				Namespace: "test",
				Name:      "test-pod-2",
			},
			Spec: corev1.PodSpec{
				NodeName: "node01",
				Containers: []corev1.Container{
					{
						Name:  "c1",
						Image: "i1",
					},
				},
				TerminationGracePeriodSeconds: pointer.Int64Ptr(0),
			},
			Status: corev1.PodStatus{
				Conditions: []corev1.PodCondition{
					{
						Type:   corev1.PodReady,
						Status: corev1.ConditionTrue,
					},
				},
			},
		},
	}
}

func getTestNM() *nodemaintenanceapi.NodeMaintenance {
	return &nodemaintenanceapi.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "node-maintenance",
		},
		Spec: nodemaintenanceapi.NodeMaintenanceSpec{
			NodeName: "node01",
			Reason:   "test reason",
		},
	}
}
