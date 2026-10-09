package v1beta1

import (
	"context"
	"errors"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nodemaintenancev1beta1 "github.com/medik8s/node-maintenance-operator/v5/api/v1beta1"
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
				Name: etcdQuorumPDBNamespace,
			},
		}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(quorumNs), &corev1.Namespace{}); err != nil {
			Expect(k8sClient.Create(context.Background(), quorumNs)).To(Succeed())
		}
	})

	Describe("creating NodeMaintenance", func() {

		Context("for not existing node", func() {

			It("should be rejected", func() {
				nm := getTestNMO(nonExistingNodeName)
				err := k8sClient.Create(context.Background(), nm)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(errorNodeNotExists, nonExistingNodeName))
			})

		})

		Context("for node already in maintenance", func() {

			var node *corev1.Node
			var nmExisting *nodemaintenancev1beta1.NodeMaintenance

			BeforeEach(func() {
				// add a node and node maintenance CR to fake client
				node = getTestNode(existingNodeName, false)
				Expect(k8sClient.Create(context.Background(), node)).To(Succeed())

				nmExisting = getTestNMO(existingNodeName)
				Expect(k8sClient.Create(context.Background(), nmExisting)).To(Succeed())
			})

			AfterEach(func() {
				Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
				Expect(k8sClient.Delete(context.Background(), nmExisting)).To(Succeed())
			})

			It("should be rejected", func() {
				nm := getTestNMO(existingNodeName)
				Eventually(func() error {
					err := k8sClient.Create(context.Background(), nm)
					return err
				}, time.Second, 200*time.Millisecond).Should(And(
					HaveOccurred(),
					WithTransform(func(err error) string { return err.Error() }, ContainSubstring(errorNodeMaintenanceExists, existingNodeName)),
				))
			})

		})

		Context("for master/control-plane node", func() {

			var node *corev1.Node

			BeforeEach(func() {
				node = getTestNode(existingNodeName, true)
				Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
			})

			AfterEach(func() {
				Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
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
						Expect(k8sClient.Create(context.Background(), nm)).Error().NotTo(HaveOccurred())
						DeferCleanup(k8sClient.Delete, context.Background(), nm)
					})
					It("should be rejected if the pod is on True state", func() {
						nm := getTestNMO(existingNodeName)
						err := k8sClient.Create(context.Background(), nm)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorControlPlaneQuorumViolation, node.Name))
					})
					It("should also be rejected on Kubernetes when the guard PDB exists", func() {
						validator := &nodeMaintenanceValidator{client: k8sClient, isOpenShift: false}
						err := validator.validateControlPlaneQuorum(existingNodeName)
						Expect(err).To(HaveOccurred())
						Expect(err.Error()).To(ContainSubstring(errorControlPlaneQuorumViolation, node.Name))
					})
				})
				When("node doesn't have etcd guard pod", func() {
					It("should be allowed", func() {
						nm := getTestNMO(existingNodeName)
						Expect(k8sClient.Create(context.Background(), nm)).To(Succeed())
						DeferCleanup(k8sClient.Delete, context.Background(), nm)
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
					Expect(k8sClient.Create(context.Background(), nm)).To(Succeed())
					DeferCleanup(k8sClient.Delete, context.Background(), nm)
				})
			})

			Context("without etcd quorum guard PDB", func() {

				It("should be rejected", func() {
					nm := getTestNMO(existingNodeName)
					err := k8sClient.Create(context.Background(), nm)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(errorControlPlaneQuorumViolation, node.Name))
				})

				It("should be allowed on Kubernetes", func() {
					validator := &nodeMaintenanceValidator{client: k8sClient, isOpenShift: false}
					Expect(validator.validateControlPlaneQuorum(existingNodeName)).To(Succeed())
				})

			})

			Context("with the pre 4.11 etcd quorum guard PDB", func() {

				BeforeEach(func() {
					pdb := getTestPDB()
					pdb.Name = etcdQuorumPDBOldName
					Expect(k8sClient.Create(context.Background(), pdb)).To(Succeed())
					DeferCleanup(k8sClient.Delete, context.Background(), pdb)

					guardPod := getPodGuard(existingNodeName)
					Expect(k8sClient.Create(context.Background(), guardPod)).To(Succeed())
					setPodConditionReady(context.Background(), guardPod, corev1.ConditionTrue)
					var force client.GracePeriodSeconds = 0
					DeferCleanup(k8sClient.Delete, context.Background(), guardPod, force)
				})

				It("should be rejected on Kubernetes as well", func() {
					validator := &nodeMaintenanceValidator{client: k8sClient, isOpenShift: false}
					err := validator.validateControlPlaneQuorum(existingNodeName)
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(errorControlPlaneQuorumViolation, node.Name))
				})

			})
		})

		Context("for worker node on Kubernetes with an etcd quorum guard PDB", func() {

			BeforeEach(func() {
				node := getTestNode(existingNodeName, false)
				Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
				DeferCleanup(k8sClient.Delete, context.Background(), node)

				pdb := getTestPDB()
				Expect(k8sClient.Create(context.Background(), pdb)).To(Succeed())
				DeferCleanup(k8sClient.Delete, context.Background(), pdb)
			})

			It("should be allowed, the quorum check applies to control-plane nodes only", func() {
				validator := &nodeMaintenanceValidator{client: k8sClient, isOpenShift: false}
				Expect(validator.validateControlPlaneQuorum(existingNodeName)).To(Succeed())
			})

		})

		Context("when the etcd quorum guard PDB lookup fails on Kubernetes", func() {

			It("should be rejected, the quorum check fails closed", func() {
				failingClient := pdbErrorClient{
					Client: k8sClient,
					err: apierrors.NewForbidden(
						schema.GroupResource{Group: "policy", Resource: "poddisruptionbudgets"},
						etcdQuorumPDBNewName,
						errors.New("not allowed to read PDBs"),
					),
				}

				validator := &nodeMaintenanceValidator{client: failingClient, isOpenShift: false}
				err := validator.validateControlPlaneQuorum(existingNodeName)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("could not check for etcd quorum PDB"))
			})

		})

	})

	Describe("updating NodeMaintenance", func() {

		Context("with new nodeName", func() {

			var node *corev1.Node

			BeforeEach(func() {
				node = getTestNode(existingNodeName, false)
				Expect(k8sClient.Create(context.Background(), node)).To(Succeed())
			})

			AfterEach(func() {
				Expect(k8sClient.Delete(context.Background(), node)).To(Succeed())
			})

			It("should be rejected", func() {
				nm := getTestNMO(existingNodeName)
				Expect(k8sClient.Create(context.Background(), nm)).To(Succeed())
				DeferCleanup(k8sClient.Delete, context.Background(), nm)
				nm.Spec.NodeName = "new-node-name"
				err := k8sClient.Update(context.Background(), nm)
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(errorNodeNameUpdateForbidden))
			})

		})
	})
})

// pdbErrorClient fails every PodDisruptionBudget read with err, and delegates
// everything else, for covering the fail-closed path of the quorum validation.
type pdbErrorClient struct {
	client.Client
	err error
}

func (c pdbErrorClient) Get(ctx context.Context, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
	if _, isPDB := obj.(*policyv1.PodDisruptionBudget); isPDB {
		return c.err
	}
	return c.Client.Get(ctx, key, obj, opts...)
}

func getTestNMO(nodeName string) *nodemaintenancev1beta1.NodeMaintenance {
	return &nodemaintenancev1beta1.NodeMaintenance{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-" + nodeName,
		},
		Spec: nodemaintenancev1beta1.NodeMaintenanceSpec{
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
			Namespace: etcdQuorumPDBNamespace,
			Name:      etcdQuorumPDBNewName,
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
			Namespace: etcdQuorumPDBNamespace,
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
