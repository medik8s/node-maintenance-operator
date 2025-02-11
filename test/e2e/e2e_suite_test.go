// Copyright 2018 The Operator-SDK Authors
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	"github.com/onsi/ginkgo/v2/reporters"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/medik8s/node-maintenance-operator/api/v1beta1"
)

const (
	junitDir = "/tmp/artifacts"
)

var (
	// The ns the operator is running in
	operatorNsName string
	// The ns for test deployments
	testNsName    string
	testNamespace *corev1.Namespace
	// namespace leases are created in
	leaseNs = "medik8s-leases"
)

var _ = BeforeSuite(func() {
	operatorNsName = os.Getenv("OPERATOR_NS")
	Expect(operatorNsName).ToNot(BeEmpty(), "OPERATOR_NS env var not set, can't start e2e test")

	testNsName = os.Getenv("TEST_NAMESPACE")
	Expect(testNsName).ToNot(BeEmpty(), "TEST_NAMESPACE env var not set, can't start e2e test")
	testNamespace = &corev1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: testNsName,
		},
	}

	// create test namespace
	err := Client.Create(context.TODO(), testNamespace)
	if errors.IsAlreadyExists(err) {
		logWarnln("test namespace already exists, that is unexpected")
		return
	}
	Expect(err).ToNot(HaveOccurred())

	// wait until webhooks are up and running by trying to create a CR and ignoring unexpected errors
	testCR := getNodeMaintenance("webhook-test", "some-not-existing-node-name")
	_ = createCRIgnoreUnrelatedErrors(testCR)
})

var _ = AfterSuite(func() {
	// Delete nodeMaintenances
	if err := Client.DeleteAllOf(context.TODO(), &v1beta1.NodeMaintenance{}); err != nil {
		logWarnf("failed to clean up node maintenances: %v", err)
	}

	// Delete test namespace
	if err := Client.Delete(context.TODO(), testNamespace); err != nil {
		logWarnf("failed to clean up test namespace: %v", err)
	}
})

func TestNodeMaintenance(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Node Maintenance Operator e2e tests")
}

// NewJUnitReporter with the given name. testSuiteName must be a valid filename part
func NewJUnitReporter(testSuiteName string) *reporters.JUnitReporter {
	return reporters.NewJUnitReporter(fmt.Sprintf("%s/%s_%s.xml", junitDir, "unit_report", testSuiteName))
}
