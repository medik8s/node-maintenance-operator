package v1beta1

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NodeMaintenance Validation", func() {

	const (
		nonExistingNodeName = "node-not-exists"
		existingNodeName    = "node-exists"
	)

	BeforeEach(func() {
		// create quorum ns on 1st run
		quorumNs := &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: EtcdQuorumPDBNamespace,
			},
		}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(quorumNs), &corev1.Namespace{}); err != nil {
			err := k8sClient.Create(context.Background(), quorumNs)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	Describe("creating NodeMaintenance", func() {

		Context("for not existing node", func() {

			It("should be rejected", func() {
				nm := getTestNMO(nonExistingNodeName)
				_, err := nm.ValidateCreate()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(ErrorNodeNotExists, nonExistingNodeName))
			})

		})

		Context("for node already in maintenance", func() {

			var node *corev1.Node
			var nmExisting *NodeMaintenance

			BeforeEach(func() {
				// add a node and node maintenance CR to fake client
				node = getTestNode(existingNodeName, false)
				err := k8sClient.Create(context.Background(), node)
				Expect(err).ToNot(HaveOccurred())

				nmExisting = getTestNMO(existingNodeName)
				err = k8sClient.Create(context.Background(), nmExisting)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				err := k8sClient.Delete(context.Background(), node)
				Expect(err).ToNot(HaveOccurred())

				err = k8sClient.Delete(context.Background(), nmExisting)
				Expect(err).ToNot(HaveOccurred())
			})

			It("should be rejected", func() {
				nm := getTestNMO(existingNodeName)
				Eventually(func() error {
					_, err := nm.ValidateCreate()
					return err
				}, time.Second, 200*time.Millisecond).Should(And(
					HaveOccurred(),
					WithTransform(func(err error) string { return err.Error() }, ContainSubstring(ErrorNodeMaintenanceExists, existingNodeName)),
				))
			})

		})

		Context("for master/control-plane node", func() {

			var node *corev1.Node

			BeforeEach(func() {
				node = getTestNode(existingNodeName, true)
				err := k8sClient.Create(context.Background(), node)
				Expect(err).ToNot(HaveOccurred())
			})

			AfterEach(func() {
				err := k8sClient.Delete(context.Background(), node)
				Expect(err).ToNot(HaveOccurred())
			})

			Context("with potential quorum violation", func() {

				BeforeEach(func() {
					pdb := getTestPDB()
					Expect(k8sClient.Create(context.Background(), pdb)).To(Succeed())
					DeferCleanup(k8sClient.Delete, context.Background(), pdb)

				})
				When("node has etcd guard pod", func() {
					var guardPod *corev1.Pod
					BeforeEach(func() {
						guardPod = getPodGuard(existingNodeName)
						Expect(k8sClient.Create(context.Background(), guardPod)).To(Succeed())
						setPodConditionReady(context.Background(), guardPod, corev1.ConditionTrue)
						// delete with force as the guard pod deletion takes time and won't happen immediately
						var force client.GracePeriodSeconds = 0
						DeferCleanup(k8sClient.Delete, context.Background(), guardPod, force)
					})
					It("should be allowed if the pod is on Fail state", func() {
						testGuardPod := &corev1.Pod{}
						Expect(k8sClient.Get(context.Background(), client.ObjectKeyFromObject(guardPod), testGuardPod)).To(Succeed())
						setPodConditionReady(context.Background(), testGuardPod, corev1.ConditionFalse)

						nm := getTestNMO(existingNodeName)
						Expect(nm.ValidateCreate()).Error().NotTo(HaveOccurred())
					})
					It("should be rejected if the pod is on True state", func() {
						nm := getTestNMO(existingNodeName)
						_, err := nm.ValidateCreate()
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(ErrorControlPlaneQuorumViolation, node.Name))
					})
				})
				When("node doesn't have etcd guard pod", func() {
					It("should be allowed", func() {
						nm := getTestNMO(existingNodeName)
						Expect(nm.ValidateCreate()).Error().NotTo(HaveOccurred())
					})
				})
			})

			Context("without potential quorum violation", func() {

				BeforeEach(func() {
					pdb := getTestPDB()
					Expect(k8sClient.Create(context.Background(), pdb)).To(Succeed())
					DeferCleanup(k8sClient.Delete, context.Background(), pdb)

					pdb.Status.DisruptionsAllowed = 1
					Expect(k8sClient.Status().Update(context.Background(), pdb)).To(Succeed())
				})

				It("should be allowed", func() {
					nm := getTestNMO(existingNodeName)
					Expect(nm.ValidateCreate()).Error().NotTo(HaveOccurred())
				})

			})

			Context("without etcd quorum guard PDB", func() {

				It("should be rejected", func() {
					nm := getTestNMO(existingNodeName)
					_, err := nm.ValidateCreate()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(ErrorControlPlaneQuorumViolation, node.Name))
				})

			})
		})

	})

	Describe("updating NodeMaintenance", func() {

		Context("with new nodeName", func() {

			It("should be rejected", func() {
				nmOld := getTestNMO(existingNodeName)
				nm := getTestNMO("newNodeName")
				_, err := nm.ValidateUpdate(nmOld)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(ErrorNodeNameUpdateForbidden))
			})

		})
	})
})

func getTestNMO(nodeName string) *NodeMaintenance {
	return &NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-" + nodeName,
		},
		Spec: NodeMaintenanceSpec{
			NodeName: nodeName,
		},
	}
}

func getTestNode(name string, isControlPlane bool) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if isControlPlane {
		node.ObjectMeta.Labels = map[string]string{
			"node-role.kubernetes.io/control-plane": "",
		}
	}
	return node
}

func getTestPDB() *policyv1.PodDisruptionBudget {
	return &policyv1.PodDisruptionBudget{
		ObjectMeta: metav1.ObjectMeta{
			Namespace: EtcdQuorumPDBNamespace,
			Name:      EtcdQuorumPDBNewName,
		},
	}
}

// getPodGuard returns guard pod with expected label and Ready condition is True for a given nodeName
func getPodGuard(nodeName string) *corev1.Pod {
	dummyContainer := corev1.Container{
		Name:  "container-name",
		Image: "foo",
	}
	return &corev1.Pod{
		TypeMeta: metav1.TypeMeta{Kind: "Pod"},
		ObjectMeta: metav1.ObjectMeta{
			Name:      "guard-" + nodeName,
			Namespace: EtcdQuorumPDBNamespace,
			Labels: map[string]string{
				"app": "guard",
			},
		},
		Spec: corev1.PodSpec{
			NodeName: nodeName,
			Containers: []corev1.Container{
				dummyContainer,
			},
		},
	}
}

func setPodConditionReady(ctx context.Context, pod *corev1.Pod, readyVal corev1.ConditionStatus) {
	pod.Status.Conditions = []corev1.PodCondition{{Type: corev1.PodReady, Status: readyVal}}
	Expect(k8sClient.Status().Update(context.Background(), pod)).To(Succeed())
}
