module github.com/medik8s/node-maintenance-operator

go 1.18

require (
	github.com/go-logr/logr v1.2.3
	github.com/onsi/ginkgo v1.16.5
	github.com/onsi/gomega v1.17.0
	github.com/sirupsen/logrus v1.8.1
	k8s.io/api v0.23.5
	k8s.io/apiextensions-apiserver v0.23.5 // indirect
	k8s.io/apimachinery v0.23.5
	k8s.io/client-go v0.23.5
	k8s.io/klog v1.0.0
	k8s.io/kubectl v0.23.5
	k8s.io/utils v0.0.0-20210820185131-d34e5cb4466e
	sigs.k8s.io/controller-runtime v0.11.0
)
