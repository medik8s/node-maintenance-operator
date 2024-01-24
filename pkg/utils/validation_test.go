package utils

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var _ = Describe("Check OpenShift Existance Validation", func() {
	When("Validator doesn't aware of OpenShift", func() {
		It("should not support OpenShift", func() {
			openshiftCheck, err := NewOpenshiftValidator(cfg)
			Expect(err).NotTo(HaveOccurred(), "failed to check if we run on Openshift")
			Expect(openshiftCheck.IsOpenshiftSupported()).To(BeFalse())
		})
	})
})
