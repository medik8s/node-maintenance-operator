package e2e

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"reflect"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	coordv1 "k8s.io/api/coordination/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/utils/pointer"
	"sigs.k8s.io/controller-runtime/pkg/client"

	nmo "github.com/medik8s/node-maintenance-operator/api/v1beta1"
	nodemaintenance "github.com/medik8s/node-maintenance-operator/controllers"
)

var (
	retryInterval   = time.Second * 5
	timeout         = time.Second * 120
	testDeployment  = "test-deployment"
	testMaintenance = "test-maintenance"
	podLabel        = map[string]string{"test": "drain"}
)

var _ = Describe("Starting Maintenance", func() {

	var controlPlaneNodes, workers []string
	var controPlaneMaintenance *nmo.NodeMaintenance

	BeforeEach(func() {
		if controlPlaneNodes == nil {
			// do this once only
			controlPlaneNodes, workers = getNodes()
			Expect(controlPlaneNodes).ToNot(BeEmpty(), "No master/control-plane nodes found")
			Expect(workers).ToNot(BeEmpty(), "No worker nodes found")
		}
	})

	Context("for the 1st master/control-plane node", func() {

		var err error

		JustBeforeEach(func() {
			if controPlaneMaintenance == nil {
				// do this once only
				controlPlaneNode := controlPlaneNodes[0]
				controPlaneMaintenance = getNodeMaintenance(fmt.Sprintf("test-1st-control-plane-%s", controlPlaneNode), controlPlaneNode)
				err = createCRIgnoreUnrelatedErrors(controPlaneMaintenance)
			}
		})

		It("should succeed", func() {
			if len(controlPlaneNodes) < 3 {
				Skip("cluster has less than 3 master/control-plane nodes and is to small for running this test")
			}
			// FLAKY Test - Possibly needs more time. It was tested with 0 seconds and got ErrorControlPlaneQuorumViolation error
			ExpectWithOffset(1, err).ToNot(HaveOccurred())
		})

		It("should fail", func() {
			if len(controlPlaneNodes) >= 3 {
				Skip("with 3 or more master/control-plane it should not fail")
			}
			// we have 1 control-plane node only
			// on Openshift the etcd-quorum-guard PDB should prevent setting maintenance
			// on k8s the fake etcd-quorum-guard PDB should do as well
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(nmo.ErrorControlPlaneQuorumViolation), "Unexpected error message")
		})
	})

	Context("for the 2nd master/control-plane node", func() {

		AfterEach(func() {
			// after testing 2nd control-plane node we can restore 1st control-plane node
			if controPlaneMaintenance != nil {
				if err := Client.Delete(context.TODO(), controPlaneMaintenance); err != nil {
					logWarnf("failed to delete NodeMaintenance for 1st master/control-plane node: %v\n", err)
				}
				controPlaneMaintenance = nil
			}
		})

		It("should fail", func() {
			if len(controlPlaneNodes) < 3 {
				Skip("cluster has less than 3 master/control-plane nodes and is too small for running this test")
			}
			if len(controlPlaneNodes) > 3 {
				logWarnf("there are %v master/control-plane nodes, which is unexpected. Skipping quorum validation for 2nd master/control-plane node!\n", len(controlPlaneNodes))
				Skip("unexpected big cluster, no clue if 2nd master/control-plane maintenance is fine or not")
			}

			// FLAKY Test - Possibly needs more time. It was tested with 10 seconds and got nil.
			// the etcd-quorum-guard PDB needs some time to be updated after the 1st control-plane node was set into maintenance
			time.Sleep(20 * time.Second)

			controlPlaneNode := controlPlaneNodes[1]
			nodeMaintenance := getNodeMaintenance(fmt.Sprintf("test-2nd-control-plane-%s", controlPlaneNode), controlPlaneNode)

			err := createCRIgnoreUnrelatedErrors(nodeMaintenance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(nmo.ErrorControlPlaneQuorumViolation), "Unexpected error message")
		})
	})

	Context("for a not existing node", func() {
		It("should fail", func() {
			nodeName := "doesNotExist"
			nodeMaintenance := getNodeMaintenance("test-unexisting", nodeName)
			err := createCRIgnoreUnrelatedErrors(nodeMaintenance)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf(nmo.ErrorNodeNotExists, nodeName)), "Unexpected error message")
		})
	})

	Context("for a worker node", func() {

		var maintenanceNodeName string
		var nodeMaintenance *nmo.NodeMaintenance
		var startTime time.Time

		BeforeEach(func() {
			// do this once only
			if nodeMaintenance == nil {
				startTime = time.Now()
				createTestDeployment()
				maintenanceNodeName = getTestDeploymentNodeName()
				nodeMaintenance = getNodeMaintenance(testMaintenance, maintenanceNodeName)
			}
		})

		It("should succeed", func() {
			err := createCRIgnoreUnrelatedErrors(nodeMaintenance)
			Expect(err).ToNot(HaveOccurred())
		})

		It("should prevent creating another maintenance for the same node", func() {
			nmDuplicate := getNodeMaintenance("test-duplicate", maintenanceNodeName)
			err := createCRIgnoreUnrelatedErrors(nmDuplicate)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(fmt.Sprintf(nmo.ErrorNodeMaintenanceExists, maintenanceNodeName)), "Unexpected error message")
		})

		It("should prevent updating the node name", func() {
			nmCopy := nodeMaintenance.DeepCopy()
			nmCopy.Spec.NodeName = "some-random-nodename"
			err := Client.Patch(context.TODO(), nmCopy, client.MergeFrom(nodeMaintenance), &client.PatchOptions{})
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring(nmo.ErrorNodeNameUpdateForbidden), "Unexpected error message")
		})

		It("should report started maintenance", func() {
			Eventually(func() (bool, error) {
				nm := &nmo.NodeMaintenance{}
				if err := Client.Get(context.TODO(), types.NamespacedName{Name: nodeMaintenance.Name}, nm); err != nil {
					return false, err
				}

				if nm.Status.Phase != nmo.MaintenanceRunning {
					logInfof("phase: %s\n", nm.Status.Phase)
					return false, nil
				}

				return true, nil
			}, 60*time.Second, 5*time.Second).Should(BeTrue(), "maintenance did not start in time")
		})

		It("should report succeeded maintenance", func() {
			Eventually(func() (bool, error) {
				nm := &nmo.NodeMaintenance{}
				if err := Client.Get(context.TODO(), types.NamespacedName{Name: nodeMaintenance.Name}, nm); err != nil {
					return false, err
				}

				if nm.Status.Phase != nmo.MaintenanceSucceeded {
					logInfof("phase: %s\n", nm.Status.Phase)
					return false, nil
				}

				return true, nil
			}, 300*time.Second, 10*time.Second).Should(BeTrue(), "maintenance did not succeed in time")
		})

		It("should have been reconciled with fixed duration at least once", func() {
			// check operator log showing it reconciled with fixed duration because of drain timeout
			// it should be caused by the test deployment's termination graceperiod > drain timeout
			Expect(getOperatorLogs()).To(ContainSubstring(nodemaintenance.FixedDurationReconcileLog))
		})

		It("should result in unschedulable and tainted node", func() {
			node := &corev1.Node{}
			err := Client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: maintenanceNodeName}, node)
			Expect(err).ToNot(HaveOccurred(), "failed to get node")
			Expect(node.Spec.Unschedulable).To(BeTrue(), "node should have been unschedulable")
			Expect(isTainted(node)).To(BeTrue(), "node should have had the medik8s taint")
		})

		It("should result in a valid lease object", func() {
			hasValidLease(maintenanceNodeName, startTime)
		})

		It("should move test workload to another worker node", func() {
			if len(workers) < 2 {
				Skip("this doesn't work with 1 worker node only")
			}
			waitForTestDeployment(1)
			nodeName := getTestDeploymentNodeName()
			Expect(nodeName).ToNot(Equal(maintenanceNodeName), "workload should run on a new node now")
		})

		Context("ending maintenance", func() {

			It("should succeed", func() {
				err := Client.Delete(context.TODO(), nodeMaintenance)
				Expect(err).ToNot(HaveOccurred(), "failed to delete node maintenance")
			})

			It("should result in resetted node status", func() {
				// FLAKY Test - Possibly needs more time. It was tested with 60 seconds
				Eventually(func() (bool, error) {
					node := &corev1.Node{}
					if err := Client.Get(context.TODO(), types.NamespacedName{Namespace: "", Name: maintenanceNodeName}, node); err != nil {
						return false, err
					}
					if node.Spec.Unschedulable {
						logInfoln("node is still unschedulable")
						return false, nil
					}
					if isTainted(node) {
						logInfoln("node is still tainted")
						return false, nil
					}
					return true, nil
				}, 120*time.Second, 10*time.Second).Should(BeTrue(), "node should be resetted")
			})

			It("should have invalidated the lease", func() {
				isLeaseInvalidated(maintenanceNodeName)
			})

			It("test deployment should still be running", func() {
				waitForTestDeployment(1)
			})

		})

	})
})

func getNodes() ([]string, []string) {
	controlPlaneNodes := make([]string, 0)
	workers := make([]string, 0)

	nodesList := &corev1.NodeList{}
	err := Client.List(context.TODO(), nodesList, &client.ListOptions{})
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "Couldn't get node names")

	for _, node := range nodesList.Items {
		if node.Labels == nil {
			logWarnf("node %s has no role label, skipping it\n", node.Name)
			continue
		}
		if _, exists := node.Labels["node-role.kubernetes.io/master"]; exists {
			controlPlaneNodes = append(controlPlaneNodes, node.Name)
		} else if _, exists := node.Labels["node-role.kubernetes.io/control-plane"]; exists {
			controlPlaneNodes = append(controlPlaneNodes, node.Name)
		} else {
			workers = append(workers, node.Name)
		}
	}
	logInfof("master/control-plane nodes: %v\n", controlPlaneNodes)
	logInfof("worker nodes: %v\n", workers)
	return controlPlaneNodes, workers
}

func getNodeMaintenance(name, nodeName string) *nmo.NodeMaintenance {
	return &nmo.NodeMaintenance{
		TypeMeta: metav1.TypeMeta{
			Kind:       "NodeMaintenance",
			APIVersion: "nodemaintenance.medik8s.io/v1beta1",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name: "nodemaintenance-" + name,
		},
		Spec: nmo.NodeMaintenanceSpec{
			NodeName: nodeName,
			Reason:   "Set maintenance on node for e2e testing",
		},
	}
}

// Ignore errors like
// - connect: connection refused
// - no endpoints available for service "node-maintenance-operator-service"
// They can be caused by webhooks not being ready yet or unavailable control-plane nodes
func createCRIgnoreUnrelatedErrors(nm *nmo.NodeMaintenance) error {
	var err error

	Eventually(func() string {
		if err = Client.Create(context.TODO(), nm); err != nil {
			logInfof("CR creation failed with error: %v\n", err)
			return err.Error()
		}
		return ""
	}, 60*time.Second, 5*time.Second).ShouldNot(Or(
		ContainSubstring("connect"),
		ContainSubstring("no endpoints available"),
	), "webhook isn't working")

	return err
}

func createTestDeployment() {
	dep := &appsv1.Deployment{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "apps/v1",
			Kind:       "Deployment",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      testDeployment,
			Namespace: testNsName,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: pointer.Int32Ptr(1),
			Selector: &metav1.LabelSelector{
				MatchLabels: podLabel,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Namespace: testNsName,
					Labels:    podLabel,
				},
				Spec: corev1.PodSpec{
					Affinity: &corev1.Affinity{
						NodeAffinity: &corev1.NodeAffinity{
							RequiredDuringSchedulingIgnoredDuringExecution: &corev1.NodeSelector{
								NodeSelectorTerms: []corev1.NodeSelectorTerm{
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "node-role.kubernetes.io/master",
												Operator: corev1.NodeSelectorOpDoesNotExist,
											},
										},
									},
									{
										MatchExpressions: []corev1.NodeSelectorRequirement{
											{
												Key:      "node-role.kubernetes.io/control-plane",
												Operator: corev1.NodeSelectorOpDoesNotExist,
											},
										},
									},
								},
							},
						},
					},
					Containers: []corev1.Container{{
						Image:   "busybox",
						Name:    "testpodbusybox",
						Command: []string{"/bin/sh"},
						Args:    []string{"-c", "while true; do echo hello; sleep 10;done"},
					}},
					// make sure we run into the drain timeout at least once
					TerminationGracePeriodSeconds: pointer.Int64Ptr(int64(nodemaintenance.DrainerTimeout.Seconds()) + 50),
				},
			},
		},
	}

	err := Client.Create(context.TODO(), dep)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed to create test deployment")
	waitForTestDeployment(2)
}

func waitForTestDeployment(offset int) {

	EventuallyWithOffset(offset, func() error {
		deployment, err := KubeClient.AppsV1().Deployments(testNsName).Get(context.TODO(), testDeployment, metav1.GetOptions{})
		if err != nil {
			if apierrors.IsNotFound(err) {
				logInfoln("test deployment not found yet")
				return err
			}
			logInfof("unexpected error while waiting for test deployment: %v", err)
			return err
		}

		if int(deployment.Status.AvailableReplicas) >= 1 {
			return nil
		}
		logInfoln("test deployment not available yet")
		return fmt.Errorf("test deploymemt not ready yet")

	}, timeout, retryInterval).ShouldNot(HaveOccurred(), "test deployment failed")

}

func getTestDeploymentNodeName() string {
	pods := getTestDeploymentPods()
	nodeName := pods.Items[0].Spec.NodeName
	return nodeName
}

func getTestDeploymentPods() *corev1.PodList {
	labelSelector := labels.SelectorFromSet(podLabel)
	pods := &corev1.PodList{}
	err := Client.List(context.TODO(), pods, &client.ListOptions{LabelSelector: labelSelector})
	ExpectWithOffset(2, err).ToNot(HaveOccurred(), "failed to get test pods")
	ExpectWithOffset(2, pods.Size()).ToNot(BeZero(), "no test pods found")
	return pods
}

func getOperatorLogs() string {
	pod := getOperatorPod()
	podName := pod.ObjectMeta.Name
	podLogOpts := corev1.PodLogOptions{
		Container: "manager",
	}

	req := KubeClient.CoreV1().Pods(pod.Namespace).GetLogs(podName, &podLogOpts)
	podLogs, err := req.Stream(context.Background())
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed to stream operator logs")
	defer podLogs.Close()

	buf := new(bytes.Buffer)
	_, err = io.Copy(buf, podLogs)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed to copy operator logs")
	return buf.String()
}

func getOperatorPod() *corev1.Pod {
	pods, err := KubeClient.CoreV1().Pods(operatorNsName).List(context.Background(), metav1.ListOptions{LabelSelector: "node-maintenance-operator="})
	ExpectWithOffset(2, err).ToNot(HaveOccurred(), "failed to get operator pods")
	ExpectWithOffset(2, len(pods.Items)).ToNot(BeZero(), "no operator pod found")
	return &pods.Items[0]
}

func isTainted(node *corev1.Node) bool {
	medik8sDrainTaint := corev1.Taint{
		Key:    "medik8s.io/drain",
		Effect: corev1.TaintEffectNoSchedule,
	}
	taints := node.Spec.Taints
	for _, taint := range taints {
		if reflect.DeepEqual(taint, medik8sDrainTaint) {
			return true
		}
	}
	return false
}

func hasValidLease(nodeName string, startTime time.Time) {
	lease := &coordv1.Lease{}
	err := Client.Get(context.TODO(), types.NamespacedName{Namespace: operatorNsName, Name: nodeName}, lease)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed to get lease")

	ExpectWithOffset(1, *lease.Spec.LeaseDurationSeconds).To(Equal(int32(nodemaintenance.LeaseDuration.Seconds())))
	ExpectWithOffset(1, *lease.Spec.HolderIdentity).To(Equal(nodemaintenance.LeaseHolderIdentity))

	// renew and aquire time should be between maintenance start and now
	checkTime := time.Now()
	ExpectWithOffset(1, lease.Spec.AcquireTime.Time).To(BeTemporally(">", startTime), "acquire time should be after start time")
	ExpectWithOffset(1, lease.Spec.AcquireTime.Time).To(BeTemporally("<", checkTime), "acquire time should be before now")
	ExpectWithOffset(1, lease.Spec.RenewTime.Time).To(BeTemporally(">", startTime), "renew time should be after start time")
	ExpectWithOffset(1, lease.Spec.RenewTime.Time).To(BeTemporally("<", checkTime), "renew time should be before now")

	// renewal checks would take too long, lease time is 1 hour...
}

func isLeaseInvalidated(nodeName string) {
	lease := &coordv1.Lease{}
	err := Client.Get(context.TODO(), types.NamespacedName{Namespace: operatorNsName, Name: nodeName}, lease)
	ExpectWithOffset(1, err).ToNot(HaveOccurred(), "failed to get lease")

	ExpectWithOffset(1, lease.Spec.AcquireTime).To(BeNil())
	ExpectWithOffset(1, lease.Spec.LeaseDurationSeconds).To(BeNil())
	ExpectWithOffset(1, lease.Spec.RenewTime).To(BeNil())
	ExpectWithOffset(1, lease.Spec.LeaseTransitions).To(BeNil())
}
