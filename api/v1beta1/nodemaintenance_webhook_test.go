package v1beta1

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	policyv1 "k8s.io/api/policy/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var _ = Describe("NodeMaintenance Validation", func() {

	const nonExistingNodeName = "node-not-exists"
	const existingNodeName = "node-exists"

	BeforeEach(func() {
		// create quorum ns on 1st run
		quorumNs := &v1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: EtcdQuorumPDBNamespace,
			},
		}
		if err := k8sClient.Get(context.Background(), client.ObjectKeyFromObject(quorumNs), &v1.Namespace{}); err != nil {
			err := k8sClient.Create(context.Background(), quorumNs)
			Expect(err).ToNot(HaveOccurred())
		}
	})

	Describe("creating NodeMaintenance", func() {

		Context("for not existing node", func() {

			It("should be rejected", func() {
				nm := getTestNMO(nonExistingNodeName)
				err := nm.ValidateCreate()
				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring(ErrorNodeNotExists, nonExistingNodeName))
			})

		})

		Context("for node already in maintenance", func() {

			var node *v1.Node
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
					return nm.ValidateCreate()
				}, time.Second, 200*time.Millisecond).Should(And(
					HaveOccurred(),
					WithTransform(func(err error) string { return err.Error() }, ContainSubstring(ErrorNodeMaintenanceExists, existingNodeName)),
				))
			})

		})

		Context("for master/control-plane node", func() {

			var node *v1.Node

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

				var pdb *policyv1.PodDisruptionBudget

				BeforeEach(func() {
					pdb = getTestPDB()
					err := k8sClient.Create(context.Background(), pdb)
					Expect(err).ToNot(HaveOccurred())
				})

				AfterEach(func() {
					err := k8sClient.Delete(context.Background(), pdb)
					Expect(err).ToNot(HaveOccurred())
				})

				It("should be rejected", func() {
					nm := getTestNMO(existingNodeName)
					err := nm.ValidateCreate()
					Expect(err).To(HaveOccurred())
					Expect(err.Error()).To(ContainSubstring(ErrorControlPlaneQuorumViolation))
				})

			})

			Context("without potential quorum violation", func() {

				var pdb *policyv1.PodDisruptionBudget

				BeforeEach(func() {
					pdb = getTestPDB()
					err := k8sClient.Create(context.Background(), pdb)
					Expect(err).ToNot(HaveOccurred())

					pdb.Status.DisruptionsAllowed = 1
					err = k8sClient.Status().Update(context.Background(), pdb)
					Expect(err).ToNot(HaveOccurred())
				})

				AfterEach(func() {
					err := k8sClient.Delete(context.Background(), pdb)
					Expect(err).ToNot(HaveOccurred())
				})

				It("should not be rejected", func() {
					nm := getTestNMO(existingNodeName)
					Eventually(func() error {
						return nm.ValidateCreate()
					}, time.Second, 200*time.Millisecond).ShouldNot(HaveOccurred())
				})

			})

			Context("without etcd quorum guard PDB", func() {

				It("should not be rejected", func() {
					nm := getTestNMO(existingNodeName)
					Eventually(func() error {
						return nm.ValidateCreate()
					}, time.Second, 200*time.Millisecond).ShouldNot(HaveOccurred())
				})

			})
		})

	})

	Describe("updating NodeMaintenance", func() {

		Context("with new nodeName", func() {

			It("should be rejected", func() {
				nmOld := getTestNMO(existingNodeName)
				nm := getTestNMO("newNodeName")
				err := nm.ValidateUpdate(nmOld)
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

func getTestNode(name string, isControlPlane bool) *v1.Node {
	node := &v1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
	}
	if isControlPlane {
		node.ObjectMeta.Labels = map[string]string{
			LabelNameRoleControlPlane: "",
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
