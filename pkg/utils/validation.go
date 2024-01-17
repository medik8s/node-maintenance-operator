package utils

import (
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/rest"
)

// IsOpenshiftSupported will be set to true in case the operator was installed on OpenShift cluster
var	IsOpenshiftSupported bool

// ValidateIsOpenshift returns true if the cluster has the openshift config group
func ValidateIsOpenshift(config *rest.Config) error {
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
				IsOpenshiftSupported = true
				return nil
			}
		}
	}
	return nil
}