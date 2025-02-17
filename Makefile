## Tool Versions

# See https://github.com/kubernetes-sigs/kustomize for the last version
KUSTOMIZE_VERSION ?= v5@v5.0.0
# https://github.com/kubernetes-sigs/controller-tools/releases for the last version
CONTROLLER_GEN_VERSION ?= v0.14.0
# See https://pkg.go.dev/sigs.k8s.io/controller-runtime/tools/setup-envtest?tab=versions for the last version
ENVTEST_VERSION ?= v0.0.0-20240112123317-48d9a7b44e54
# See https://pkg.go.dev/golang.org/x/tools/cmd/goimports?tab=versions for the last version
GOIMPORTS_VERSION ?= v0.17.0
# See https://github.com/onsi/ginkgo/releases for the last version
GINKGO_VERSION ?= v2.22.0
# See github.com/operator-framework/operator-registry/releases for the last version
OPM_VERSION ?= v1.35.0
# See github.com/operator-framework/operator-sdk/releases for the last version
OPERATOR_SDK_VERSION ?= v1.32.0
# GO_VERSION refers to the version of Golang to be downloaded when running dockerized version
GO_VERSION = 1.23
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.28
# See https://github.com/slintes/sort-imports/releases for the last version
SORT_IMPORTS_VERSION = v0.2.1
# OCP Version: for OKD bundle community
OCP_VERSION ?= 4.14
# update for major version updates to YQ_VERSION! see https://github.com/mikefarah/yq
# NOTE: v4.42.1 is the latest supporting go 1.20
YQ_API_VERSION = v4
YQ_VERSION = v4.42.1

BLUE_ICON_PATH = "./config/assets/nmo_blue_icon.png"

# IMAGE_REGISTRY used to indicate the registery/group for the operator, bundle and catalog
IMAGE_REGISTRY ?= quay.io/medik8s
export IMAGE_REGISTRY

# When no version is set, use latest as image tags
DEFAULT_VERSION := 0.0.1
ifeq ($(origin VERSION), undefined)
IMAGE_TAG = latest
else ifeq ($(VERSION), $(DEFAULT_VERSION))
IMAGE_TAG = latest
else
IMAGE_TAG = v$(VERSION)
endif
export IMAGE_TAG

CHANNELS = stable
export CHANNELS
DEFAULT_CHANNEL = stable
export DEFAULT_CHANNEL

# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
CI_VERSION := 9.9.9-dummy
VERSION ?= $(DEFAULT_VERSION)
PREVIOUS_VERSION ?= $(DEFAULT_VERSION)
export VERSION

# CHANNELS define the bundle channels used in the bundle.
# Add a new line here if you would like to change its default config. (E.g CHANNELS = "candidate,fast,stable")
# To re-generate a bundle for other specific channels without changing the standard setup, you can:
# - use the CHANNELS as arg of the bundle target (e.g make bundle CHANNELS=candidate,fast,stable)
# - use environment variables to overwrite this value (e.g export CHANNELS="candidate,fast,stable")
ifneq ($(origin CHANNELS), undefined)
BUNDLE_CHANNELS := --channels=$(CHANNELS)
endif

# DEFAULT_CHANNEL defines the default channel used in the bundle.
# Add a new line here if you would like to change its default config. (E.g DEFAULT_CHANNEL = "stable")
# To re-generate a bundle for any other default channel without changing the default setup, you can:
# - use the DEFAULT_CHANNEL as arg of the bundle target (e.g make bundle DEFAULT_CHANNEL=stable)
# - use environment variables to overwrite this value (e.g export DEFAULT_CHANNEL="stable")
ifneq ($(origin DEFAULT_CHANNEL), undefined)
BUNDLE_DEFAULT_CHANNEL := --default-channel=$(DEFAULT_CHANNEL)
endif
BUNDLE_METADATA_OPTS ?= $(BUNDLE_CHANNELS) $(BUNDLE_DEFAULT_CHANNEL)

OPERATOR_NAME ?= node-maintenance-operator
OPERATOR_NAMESPACE ?= openshift-workload-availability

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# medik8s/node-maintenance-operator-bundle:$VERSION and medik8s/node-maintenance-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= $(IMAGE_REGISTRY)/$(OPERATOR_NAME)

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:$(IMAGE_TAG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(IMAGE_TAG)

# Image URL to use all building/pushing image targets
IMG ?= $(IMAGE_TAG_BASE):$(IMAGE_TAG)


# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# Use kubectl, fallback to oc
KUBECTL = kubectl
ifeq (,$(shell which kubectl))
KUBECTL=oc
endif

# Setting SHELL to bash allows bash commands to be executed by recipes.
# This is a requirement for 'setup-envtest.sh' in the test target.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Run go in a container
# --rm                                                          = remove container when stopped
# -v $$(pwd):/home/go/src/github.com/medik8s/node-maintenance-operator = bind mount current dir in container
# -u $$(id -u)                                                  = use current user (else new / modified files will be owned by root)
# -w /home/go/src/github.com/medik8s/node-maintenance-operator         = working dir
# -e ...                                                        = some env vars, especially set cache to a user writable dir
# --entrypoint /bin bash ... -c                                 = run bash -c on start; that means the actual command(s) need be wrapped in double quotes, see e.g. check target which will run: bash -c "make test"
export DOCKER_GO=docker run --rm -v $$(pwd):/home/go/src/github.com/medik8s/$(OPERATOR_NAME) \
	-u $$(id -u) -w /home/go/src/github.com/medik8s/$(OPERATOR_NAME) \
	-e "GOPATH=/go" -e "GOFLAGS=-mod=vendor" -e "XDG_CACHE_HOME=/tmp/.cache" \
	-e "VERSION=$(VERSION)" -e "IMAGE_REGISTRY=$(IMAGE_REGISTRY)" \
	--entrypoint /bin/bash golang:$(GO_VERSION) -c

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: manifests
manifests: controller-gen ## Generate WebhookConfiguration, ClusterRole and CustomResourceDefinition objects.
	$(CONTROLLER_GEN) rbac:roleName=manager-role crd webhook paths="./..." output:crd:artifacts:config=config/crd/bases

generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
.PHONY:generate
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: goimports ## Run go goimports against code - goimports = go fmt + fixing imports.
	$(GOIMPORTS) -w ./api ./controllers ./test

.PHONY: vet
vet: ## Run go vet against code - report likely mistakes in packages.
	go vet ./api/... ./controllers/... ./test/...

.PHONY: go-tidy
go-tidy: # Run go mod tidy - add missing and remove unused modules.
	go mod tidy

.PHONY: go-vendor
go-vendor:  # Run go mod vendor - make vendored copy of dependencies.
	go mod vendor

.PHONY: go-verify
go-verify: go-tidy go-vendor # Run go mod verify - verify dependencies have expected content
	go mod verify

.PHONY: test-imports
test-imports: sort-imports ## Check for sorted imports
	$(SORT_IMPORTS) .

.PHONY: fix-imports
fix-imports: sort-imports ## Sort imports
	$(SORT_IMPORTS) -w .

.PHONY: test
test: test-no-verify verify-unchanged ## Generate and format code, run tests, generate manifests and bundle, and verify no uncommitted changes

.PHONY: test-no-verify
test-no-verify: go-verify manifests generate fmt fix-imports vet envtest ginkgo ## Generate and format code, and run tests
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path --bin-dir $(LOCALBIN))" \
	$(GINKGO) -r --keep-going --randomize-all --require-suite --vv --coverprofile cover.out ./controllers/... ./api/... ./pkg/...

.PHONY: bundle-run
bundle-run: operator-sdk ## Run bundle image. Default NS is "openshift-workload-availability", redefine OPERATOR_NAMESPACE to override it.
	$(OPERATOR_SDK) -n $(OPERATOR_NAMESPACE) run bundle $(BUNDLE_IMG)

.PHONY: bundle-run-update
bundle-run-update: operator-sdk ## Update bundle image.
# An older bundle image CSV should exist in the cluster, and in the same namespace,
# Default NS is "openshift-workload-availability", redefine OPERATOR_NAMESPACE to override it.
	$(OPERATOR_SDK) -n $(OPERATOR_NAMESPACE) run bundle-upgrade $(BUNDLE_IMG)

.PHONY: bundle-cleanup
bundle-cleanup: operator-sdk ## Remove bundle installed via bundle-run
	$(OPERATOR_SDK) -n $(OPERATOR_NAMESPACE) cleanup $(OPERATOR_NAME)

##@ Bundle Creation Addition
## Some addition to bundle creation in the bundle
DEFAULT_ICON_BASE64 := $(shell base64 --wrap=0 ${BLUE_ICON_PATH})
export ICON_BASE64 ?= ${DEFAULT_ICON_BASE64}
export CSV ?= "./bundle/manifests/$(OPERATOR_NAME).clusterserviceversion.yaml"

.PHONY: bundle-update
bundle-update: ## Update CSV fields and validate the bundle directory
	sed -r -i "s|containerImage: .*|containerImage: $(IMG)|;" ${CSV}
	sed -r -i "s|createdAt: .*|createdAt: `date '+%Y-%m-%d %T'`|;" ${CSV}
	sed -r -i "s|base64data:.*|base64data: ${ICON_BASE64}|;" ${CSV}
	$(MAKE) bundle-validate

.PHONY: add-replaces-field
add-replaces-field: ## Add replaces field to the CSV
	# add replaces field when building versioned bundle
	@if [ $(VERSION) != $(DEFAULT_VERSION) ]; then \
		if [ $(PREVIOUS_VERSION) == $(DEFAULT_VERSION) ]; then \
			echo "Error: PREVIOUS_VERSION must be set for versioned builds"; \
			exit 1; \
		elif [ $(shell ./hack/semver_cmp.sh $(VERSION) $(PREVIOUS_VERSION)) != 1 ]; then \
			echo "Error: VERSION ($(VERSION)) must be greater than PREVIOUS_VERSION ($(PREVIOUS_VERSION))"; \
			exit 1; \
		else \
		  	# preferring sed here, in order to have "replaces" near "version" \
			sed -r -i "/  version: $(VERSION)/ a\  replaces: $(OPERATOR_NAME).v$(PREVIOUS_VERSION)" ${CSV}; \
		fi \
	fi

.PHONY: bundle-reset-date
bundle-reset-date: ## Reset bundle's createdAt
	sed -r -i "s|createdAt: .*|createdAt: \"\"|;" ${CSV}

.PHONY: bundle-community
bundle-community: ## Update displayName, and description fields in the bundle's CSV
	sed -r -i "s|displayName: Node Maintenance Operator|displayName: Node Maintenance Operator - Community Edition |;" ${CSV}
	$(MAKE) bundle-update


.PHONY: bundle-community-k8s
bundle-community-k8s: bundle-k8s bundle-community ## Generate bundle manifests and metadata customized to Red Hat community release

.PHONY: bundle-community-okd
bundle-community-okd: bundle bundle-community  ## Generate bundle manifests and metadata customized to Red Hat community release
	$(MAKE) add-replaces-field
	$(MAKE) add-ocp-annotations
	echo -e "\n  # Annotations for OCP\n  com.redhat.openshift.versions: \"v${OCP_VERSION}\"" >> bundle/metadata/annotations.yaml


.PHONY: add-ocp-annotations
add-ocp-annotations: yq ## Add OCP annotations
	$(YQ) -i '.metadata.annotations."operators.openshift.io/valid-subscription" = "[\"OpenShift Kubernetes Engine\", \"OpenShift Container Platform\", \"OpenShift Platform Plus\"]"' ${CSV}
	# new infrastructure annotations see https://docs.engineering.redhat.com/display/CFC/Best_Practices#Best_Practices-(New)RequiredInfrastructureAnnotations
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/disconnected" = "true"' ${CSV}
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/fips-compliant" = "false"' ${CSV}
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/proxy-aware" = "false"' ${CSV}
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/tls-profiles" = "false"' ${CSV}
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/token-auth-aws" = "false"' ${CSV}
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/token-auth-azure" = "false"' ${CSV}
	$(YQ) -i '.metadata.annotations."features.operators.openshift.io/token-auth-gcp" = "false"' ${CSV}


##@ Build

.PHONY: build
build: ## Build manager binary.
	./hack/build.sh

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./main.go

.PHONY: docker-build
docker-build: check ## Build docker image with the manager.
	docker build -t ${IMG} .

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif
.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build config/default | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/default | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Default Tool Binaries
KUSTOMIZE_DIR ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN_DIR ?= $(LOCALBIN)/controller-gen
ENVTEST_DIR ?= $(LOCALBIN)/setup-envtest
GOIMPORTS_DIR ?= $(LOCALBIN)/goimports
GINKGO_DIR ?= $(LOCALBIN)/ginkgo
OPM_DIR = $(LOCALBIN)/opm
OPERATOR_SDK_DIR ?= $(LOCALBIN)/operator-sdk
SORT_IMPORTS_DIR ?= $(LOCALBIN)/sort-imports
YQ_DIR ?= $(LOCALBIN)/yq

## Specific Tool Binaries
KUSTOMIZE = $(KUSTOMIZE_DIR)/$(KUSTOMIZE_VERSION)/kustomize
CONTROLLER_GEN = $(CONTROLLER_GEN_DIR)/$(CONTROLLER_GEN_VERSION)/controller-gen
ENVTEST = $(ENVTEST_DIR)/$(ENVTEST_VERSION)/setup-envtest
GOIMPORTS = $(GOIMPORTS_DIR)/$(GOIMPORTS_VERSION)/goimports
GINKGO = $(GINKGO_DIR)/$(GINKGO_VERSION)/ginkgo
OPM = $(OPM_DIR)/$(OPM_VERSION)/opm
OPERATOR_SDK = $(OPERATOR_SDK_DIR)/$(OPERATOR_SDK_VERSION)/operator-sdk
SORT_IMPORTS = $(SORT_IMPORTS_DIR)/$(SORT_IMPORTS_VERSION)/sort-imports
YQ = $(YQ_DIR)/$(YQ_API_VERSION)-$(YQ_VERSION)/yq

.PHONY: kustomize
kustomize: ## Download kustomize locally if necessary.
	$(call go-install-tool,$(KUSTOMIZE),$(KUSTOMIZE_DIR),sigs.k8s.io/kustomize/kustomize/$(KUSTOMIZE_VERSION))

.PHONY: controller-gen
controller-gen: ## Download controller-gen locally if necessary.
	$(call go-install-tool,$(CONTROLLER_GEN),$(CONTROLLER_GEN_DIR),sigs.k8s.io/controller-tools/cmd/controller-gen@${CONTROLLER_GEN_VERSION})

.PHONY: envtest
envtest: ## Download envtest-setup locally if necessary.
	$(call go-install-tool,$(ENVTEST),$(ENVTEST_DIR),sigs.k8s.io/controller-runtime/tools/setup-envtest@${ENVTEST_VERSION})

.PHONY: goimports
goimports: ## Download goimports locally if necessary.
	$(call go-install-tool,$(GOIMPORTS),$(GOIMPORTS_DIR),golang.org/x/tools/cmd/goimports@${GOIMPORTS_VERSION})

.PHONY: ginkgo
ginkgo: ## Download ginkgo locally if necessary.
	$(call go-install-tool,$(GINKGO),$(GINKGO_DIR),github.com/onsi/ginkgo/v2/ginkgo@${GINKGO_VERSION})

.PHONY: sort-imports
sort-imports: ## Download sort-imports locally if necessary.
	$(call go-install-tool,$(SORT_IMPORTS),$(SORT_IMPORTS_DIR),github.com/slintes/sort-imports@$(SORT_IMPORTS_VERSION))

.PHONY: yq
yq: ## Download yq locally if necessary.
	$(call go-install-tool,$(YQ),$(YQ_DIR), github.com/mikefarah/yq/$(YQ_API_VERSION)@$(YQ_VERSION))

# go-install-tool will delete old package $2, then 'go install' any package $3 to $1.
define go-install-tool
@[ -f $(1) ]|| { \
	set -e ;\
	rm -rf $(2) ;\
	TMP_DIR=$$(mktemp -d) ;\
	cd $$TMP_DIR ;\
	go mod init tmp ;\
	BIN_DIR=$$(dirname $(1)) ;\
	mkdir -p $$BIN_DIR ;\
	echo "Downloading $(3)" ;\
	GOBIN=$$BIN_DIR GOFLAGS='' go install $(3) ;\
	rm -rf $$TMP_DIR ;\
}
endef

.PHONY: bundle
bundle: manifests operator-sdk kustomize ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(MAKE) bundle-reset-date bundle-validate

.PHONY: bundle-k8s
bundle-k8s: bundle # Generate bundle manifests and metadata for Kubernetes, then validate generated files.
	$(KUSTOMIZE) build config/manifests-k8s | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(MAKE) bundle-validate

.PHONY: bundle-validate
bundle-validate: operator-sdk ## Validate the bundle directory with additional validators (suite=operatorframework), such as Kubernetes deprecated APIs (https://kubernetes.io/docs/reference/using-api/deprecation-guide/) based on bundle.CSV.Spec.MinKubeVersion
	$(OPERATOR_SDK) bundle validate ./bundle --select-optional suite=operatorframework

.PHONY: bundle-build
bundle-build: bundle-update ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
opm: ## Download opm locally if necessary.
	$(call url-install-tool, $(OPM), $(OPM_DIR),github.com/operator-framework/operator-registry/releases/download/$(OPM_VERSION)/$${OS}-$${ARCH}-opm)

.PHONY: operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
	$(call url-install-tool, $(OPERATOR_SDK), $(OPERATOR_SDK_DIR),github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH})

# url-install-tool will delete old package $2, then download $3 to $1.
define url-install-tool
@[ -f $(1) ]|| { \
	set -e ;\
	rm -rf $(2) ;\
	mkdir -p $(dir $(1)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(1) $(3) ;\
	chmod +x $(1) ;\
	}
endef

# Build a file-based catalog image
# https://docs.openshift.com/container-platform/4.14/operators/admin/olm-managing-custom-catalogs.html#olm-managing-custom-catalogs-fb
# NOTE: CATALOG_DIR and CATALOG_DOCKERFILE items won't be deleted in case of recipe's failure
CATALOG_DIR := catalog
CATALOG_DOCKERFILE := ${CATALOG_DIR}.Dockerfile
CATALOG_INDEX := $(CATALOG_DIR)/index.yaml

.PHONY: add_channel_entry_for_the_bundle
add_channel_entry_for_the_bundle:
	@echo "---" >> ${CATALOG_INDEX}
	@echo "schema: olm.channel" >> ${CATALOG_INDEX}
	@echo "package: ${OPERATOR_NAME}" >> ${CATALOG_INDEX}
	@echo "name: ${CHANNELS}" >> ${CATALOG_INDEX}
	@echo "entries:" >> ${CATALOG_INDEX}
	@echo "  - name: ${OPERATOR_NAME}.v${VERSION}" >> ${CATALOG_INDEX}

.PHONY: build-tools
build-tools: ## Download & build all the tools locally if necessary.
	$(MAKE) kustomize controller-gen envtest goimports ginkgo opm operator-sdk sort-imports

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a file-based catalog image.
	# Remove the catalog directory and Dockerfile
	-rm -r ${CATALOG_DIR} ${CATALOG_DOCKERFILE}
	@mkdir -p ${CATALOG_DIR}
	$(OPM) generate dockerfile ${CATALOG_DIR}
	$(OPM) init ${OPERATOR_NAME} \
		--default-channel=${CHANNELS} \
		--description=./README.md \
		--icon=${BLUE_ICON_PATH} \
		--output yaml \
		> ${CATALOG_INDEX}
	$(OPM) render ${BUNDLE_IMG} --output yaml >> ${CATALOG_INDEX}
	$(MAKE) add_channel_entry_for_the_bundle
	$(OPM) validate ${CATALOG_DIR}
	docker build . -f ${CATALOG_DOCKERFILE} -t ${CATALOG_IMG}
	# Clean up the catalog directory and Dockerfile
	rm -r ${CATALOG_DIR} ${CATALOG_DOCKERFILE}

.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

##@ Targets used by CI

.PHONY: test-scorecard
test-scorecard: operator-sdk ## Run Scorecard testing for the bundle directory on OPERATOR_NAMESPACE
	$(OPERATOR_SDK) scorecard ./bundle -n $(OPERATOR_NAMESPACE)

.PHONY: check
check: ## Dockerized version of make test
	$(DOCKER_GO) "make test"

.PHONY: verify-unchanged
verify-unchanged: ## Verify there are no un-committed changes
	./hack/verify-unchanged.sh

.PHONY: container-build
container-build: check ## Build containers
	$(DOCKER_GO) "make bundle"
	make docker-build bundle-build

.PHONY: bundle-build-community
bundle-build-community: bundle-community ## Run bundle community changes in CSV, and then build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: container-build-community
container-build-community: docker-build bundle-build-community ## Build containers for community

.PHONY: container-push
container-push: docker-push bundle-push catalog-build catalog-push## Push containers (NOTE: catalog can't be build before bundle was pushed)

.PHONY: container-build-and-push-community
container-build-and-push-community: container-build-community container-push ## Build four images, update CSV for community, and push all the images to Quay (docker, bundle, and catalog).

.PHONY: cluster-functest
cluster-functest: ginkgo ## Run e2e tests in a real cluster
	./hack/functest.sh $(GINKGO_VERSION)
