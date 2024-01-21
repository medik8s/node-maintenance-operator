package utils

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

type OpenshiftValidator struct {
// isOpenshiftSupported will be set to true in case the operator was installed on OpenShift cluster
 isOpenshiftSupported bool
}

// NewOpenshiftValidator initialization function for OpenshiftValidator
func NewOpenshiftValidator(config *rest.Config) (*OpenshiftValidator, error) {
	v := &OpenshiftValidator{}
	if err := v.validateIsOpenshift(config); err != nil {
		return nil, err
	}
	return v, nil
}

// IsOpenshiftSupported returns whether the cluster supports OpenShift
func (v *OpenshiftValidator) IsOpenshiftSupported() bool {
	return v.isOpenshiftSupported
}
// validateIsOpenshift returns true if the cluster has the openshift config group
func (v *OpenshiftValidator) validateIsOpenshift(config *rest.Config) error {
	dc, err := discovery.NewDiscoveryClientForConfig(config)
	if err != nil {
		return err
	}
	apiGroups, err := dc.ServerGroups()
	if err != nil {
		return err
	}

	kind := schema.GroupVersionKind{Group: "config.openshift.io", Version: "v1", Kind: "ClusterVersion"}
	for _, apiGroup := range apiGroups.Groups {
		for _, supportedVersion := range apiGroup.Versions {
			if supportedVersion.GroupVersion == kind.GroupVersion().String() {
				v.isOpenshiftSupported = true
				return nil
			}
		}
	}
	return nil
}