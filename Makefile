# VERSION defines the project version for the bundle.
# Update this value when you upgrade the version of your project.
# To re-generate a bundle for another specific version without changing the standard setup, you can:
# - use the VERSION as arg of the bundle target (e.g make bundle VERSION=0.0.2)
# - use environment variables to overwrite this value (e.g export VERSION=0.0.2)
VERSION ?= 0.0.0

# Organization in the container resgistry
DEFAULT_ORG = kuadrant
ORG ?= $(DEFAULT_ORG)

# Repo in the container registry
DEFAULT_REPO = dns-operator
REPO ?= $(DEFAULT_REPO)

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

# IMAGE_TAG_BASE defines the docker.io namespace and part of the image name for remote images.
# This variable is used to construct full image tags for bundle and catalog images.
#
# For example, running 'make bundle-build bundle-push catalog-build catalog-push' will build and push both
# quay.io/kuadrant/dns-operator-bundle:$VERSION and quay.io/kuadrant/dns-operator-catalog:$VERSION.
IMAGE_TAG_BASE ?= quay.io/kuadrant/dns-operator

# BUNDLE_IMG defines the image:tag used for the bundle.
# You can use it as an arg. (E.g make bundle-build BUNDLE_IMG=<some-registry>/<project-name-bundle>:<tag>)
BUNDLE_IMG ?= $(IMAGE_TAG_BASE)-bundle:latest

# BUNDLE_GEN_FLAGS are the flags passed to the operator-sdk generate bundle command
BUNDLE_GEN_FLAGS ?= -q --overwrite --version $(VERSION) $(BUNDLE_METADATA_OPTS)

# USE_IMAGE_DIGESTS defines if images are resolved via tags or digests
# You can enable this value if you would like to use SHA Based Digests
# To enable set flag to true
USE_IMAGE_DIGESTS ?= false
ifeq ($(USE_IMAGE_DIGESTS), true)
	BUNDLE_GEN_FLAGS += --use-image-digests
endif

# Set the Operator SDK version to use. By default, what is installed on the system is used.
# This is useful for CI or a project to utilize a specific version of the operator-sdk toolkit.
OPERATOR_SDK_VERSION ?= v1.33.0

# Image URL to use all building/pushing image targets
DEFAULT_IMG ?= $(IMAGE_TAG_BASE):latest
IMG ?= $(DEFAULT_IMG)
# ENVTEST_K8S_VERSION refers to the version of kubebuilder assets to be downloaded by envtest binary.
ENVTEST_K8S_VERSION = 1.27.1

# Get the currently used golang install path (in GOPATH/bin, unless GOBIN is set)
ifeq (,$(shell go env GOBIN))
GOBIN=$(shell go env GOPATH)/bin
else
GOBIN=$(shell go env GOBIN)
endif

# CONTAINER_TOOL defines the container tool to be used for building images.
# Be aware that the target commands are only tested with Docker which is
# scaffolded by default. However, you might want to replace it to use other
# tools. (i.e. podman)
CONTAINER_TOOL ?= docker

# Setting SHELL to bash allows bash commands to be executed by recipes.
# Options are set to exit when a recipe line exits non-zero or a piped command fails.
SHELL = /usr/bin/env bash -o pipefail
.SHELLFLAGS = -ec

# Ginkgo options
DEFAULT_GINKGO_FLAGS ?= -v
GINKGO_FLAGS ?= $(DEFAULT_GINKGO_FLAGS)

# To enable set flag to true
GINKGO_PARALLEL ?= false
ifeq ($(GINKGO_PARALLEL), true)
	GINKGO_FLAGS += -p
endif

# To enable set flag to true
GINKGO_DRYRUN ?= false
ifeq ($(GINKGO_DRYRUN), true)
	GINKGO_FLAGS += --dry-run
endif

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

.PHONY: manifests-gen-base-csv
manifests-gen-base-csv: yq ## Generate base CSV for the current configuration (VERSION, IMG, CHANNELS etc..)
	$(YQ) -i '.metadata.annotations.containerImage = "$(IMG)"' config/manifests/bases/dns-operator.clusterserviceversion.yaml
	$(YQ) -i '.metadata.name = "dns-operator.v$(VERSION)"' config/manifests/bases/dns-operator.clusterserviceversion.yaml
	$(YQ) -i '.spec.version = "$(VERSION)"' config/manifests/bases/dns-operator.clusterserviceversion.yaml
	$(YQ) -i 'del(.spec.replaces)' config/manifests/bases/dns-operator.clusterserviceversion.yaml
	
.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: fmt
fmt: ## Run go fmt against code.
	go fmt ./...

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: lint
lint: golangci-lint ## Run golangci-lint against code.
	$(GOLANGCI_LINT) -v run ./...

.PHONY: imports
imports: openshift-goimports ## Run openshift goimports against code.
	$(OPENSHIFT_GOIMPORTS) -m github.com/kuadrant/dns-operator -i github.com/kuadrant
	$(OPENSHIFT_GOIMPORTS) -p coredns/plugin -m github.com/kuadrant/coredns-kuadrant -i github.com/kuadrant

.PHONY: test
test: test-unit test-integration ## Run tests.

.PHONY: test-unit
test-unit: manifests generate fmt vet ## Run unit tests.
	go test ./... -tags=unit -coverprofile cover-unit.out

.PHONY: test-integration
test-integration: GINKGO_FLAGS=
test-integration: manifests generate fmt vet envtest ginkgo ## Run integration tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" $(GINKGO) $(GINKGO_FLAGS) -tags=integration ./internal/controller -coverprofile cover-integration.out

.PHONY: test-e2e
test-e2e: ginkgo
	$(GINKGO) $(GINKGO_FLAGS) -tags=e2e ./test/e2e

.PHONY: test-e2e-multi
test-e2e-multi: ginkgo
	$(GINKGO) $(GINKGO_FLAGS) -tags=e2e --label-filter=multi_record ./test/e2e

.PHONY: test-e2e-single
test-e2e-single: ginkgo
	$(GINKGO) $(GINKGO_FLAGS) -tags=e2e --label-filter=single_record ./test/e2e

.PHONY: test-scale
test-scale: export JOB_ITERATIONS := 1
test-scale: export NUM_RECORDS := 1
test-scale: export DNS_PROVIDER := inmemory
test-scale: export SKIP_CLEANUP := false
test-scale: export PROMETHEUS_URL ?= http://127.0.0.1:9090
test-scale: export PROMETHEUS_TOKEN ?= ""
test-scale: export KUADRANT_ZONE_ROOT_DOMAIN ?= kuadrant.local
test-scale: kube-burner
	@echo "test-scale: JOB_ITERATIONS=${JOB_ITERATIONS} NUM_RECORDS=${NUM_RECORDS} DNS_PROVIDER=${DNS_PROVIDER} KUADRANT_ZONE_ROOT_DOMAIN=${KUADRANT_ZONE_ROOT_DOMAIN} SKIP_CLEANUP=${SKIP_CLEANUP} PROMETHEUS_URL=${PROMETHEUS_URL} PROMETHEUS_TOKEN=${PROMETHEUS_TOKEN}"
	cd test/scale && $(KUBE_BURNER) init -c config.yaml --log-level debug

.PHONY: local-setup-cluster
local-setup-cluster: DEPLOY=false
local-setup-cluster: DEPLOYMENT_SCOPE=cluster
local-setup-cluster: DELEGATION_ROLE=primary
local-setup-cluster: $(KIND) ## Setup local development kind cluster, dependencies and optionally deploy the dns operator DEPLOY=false|true
	@echo "local-setup: creating cluster KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME} DEPLOY=${DEPLOY} DEPLOYMENT_SCOPE=${DEPLOYMENT_SCOPE} DELEGATION_ROLE=${DELEGATION_ROLE}"
	@$(MAKE) -s kind-delete-cluster
	@$(MAKE) -s kind-create-cluster
	@$(MAKE) -s install
	@$(MAKE) -s install-metallb

	@if [ ${DEPLOYMENT_SCOPE} = "cluster" ]; then\
		if [ ${DELEGATION_ROLE} = "primary" ]; then\
			echo "local-setup: installing bind9" ;\
			$(MAKE) -s install-bind9 ;\
			echo "local-setup: installing coredns" ;\
			$(MAKE) -s coredns-docker-build coredns-kind-load-image;\
			$(MAKE) -s install-coredns COREDNS_KUSTOMIZATION=config/local-setup/coredns ;\
		fi ;\
		if [ ${DEPLOY} = "true" ]; then\
			echo "local-setup: deploying operator (cluster scoped) to ${KIND_CLUSTER_NAME}" ;\
			$(MAKE) -s local-deploy DELEGATION_MODE=${DELEGATION_ROLE} ;\
		else\
			echo "local-setup: deploying operator (cluster scoped) to ${KIND_CLUSTER_NAME}, no manager Deployment" ;\
			$(MAKE) -s deploy DEPLOY_KUSTOMIZATION=config/local-setup/dns-operator/cluster-scoped ;\
		fi ;\
	else\
		echo "local-setup: installing bind9" ;\
		$(MAKE) -s install-bind9 ;\
		echo "local-setup: deploying operator (namespace scoped) to ${KIND_CLUSTER_NAME}" ;\
		$(MAKE) -s local-deploy-namespaced ;\
	fi ;\

	@if [ ${DEPLOY} = "true" ]; then\
		echo "local-setup: waiting for dns operator deployments" ;\
		$(KUBECTL) wait --timeout=60s --for=condition=Available deployments --all -l app.kubernetes.io/part-of=dns-operator -A ;\
	fi ;\

	@echo "local-setup: Check dns operator deployments"
	$(KUBECTL) get deployments -l app.kubernetes.io/part-of=dns-operator -A

.PHONY: local-setup-cluster-dns-providers
local-setup-cluster-dns-providers: DEPLOYMENT_SCOPE=cluster
local-setup-cluster-dns-providers: TEST_NAMESPACE=dnstest
local-setup-cluster-dns-providers:
	@echo "local-setup: creating dns providers KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME} DEPLOYMENT_SCOPE=${DEPLOYMENT_SCOPE} TEST_NAMESPACE=${TEST_NAMESPACE}"
	$(KUBECTL) config use-context kind-$(KIND_CLUSTER_NAME)
	@if [ ${DEPLOYMENT_SCOPE} = "cluster" ]; then\
		$(KUBECTL) create namespace ${TEST_NAMESPACE} --dry-run=client -o yaml | $(KUBECTL) apply -f - ;\
		$(MAKE) -s local-setup-dns-providers TARGET_NAMESPACE=${TEST_NAMESPACE} ;\
	else\
		$(MAKE) -s generate-common-dns-provider-kustomization ;\
		$(KUSTOMIZE) build --enable-helm $(CLUSTER_OVERLAY_DIR)/$(KIND_CLUSTER_NAME) | $(KUBECTL) apply -f - ;\
	fi ;\

	@echo "local-setup: Check dns providers"
	$(KUBECTL) get secrets -l app.kubernetes.io/part-of=dns-operator -A

.PHONY: local-setup
local-setup: CLUSTER_COUNT=1
local-setup: ## Setup local development kind cluster(s)
	@echo "local-setup: CLUSTER_COUNT=${CLUSTER_COUNT}"
	@n=1 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
		$(MAKE) -s local-setup-cluster KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME_PREFIX}-$$n SUBNET_OFFSET=$$n;\
		((n = n + 1)) ;\
	done ;\

	$(MAKE) local-setup-coredns-generate-from-clusters
	@n=1 ; while [[ $$n -le $(CLUSTER_COUNT) ]] ; do \
		$(MAKE) -s local-setup-cluster-dns-providers KIND_CLUSTER_NAME=${KIND_CLUSTER_NAME_PREFIX}-$$n ;\
		((n = n + 1)) ;\
	done ;\

	@echo "local-setup: Complete!!"

.PHONY: local-cleanup
local-cleanup: ## Delete local clusters
	$(MAKE) kind-delete-all-clusters
	$(MAKE) remove-all-cluster-overlays

.PHONY: local-deploy
local-deploy: docker-build kind-load-image ## Deploy the dns operator into local kind cluster from the current code
	$(KUBECTL) config use-context kind-$(KIND_CLUSTER_NAME)
	$(MAKE) deploy

.PHONY: local-deploy-namespaced
local-deploy-namespaced: docker-build kind-load-image ## Deploy the dns operator(s) into local kind cluster using the generated deployment overlays
	$(KUBECTL) config use-context kind-$(KIND_CLUSTER_NAME)
	$(MAKE) deploy-namespaced

##@ Build

.PHONY: build
build: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
build: DIRTY=$(shell hack/check-git-dirty.sh || echo "unknown")
build: manifests generate fmt vet ## Build manager binary.
	go build -ldflags "-X main.version=v${VERSION} -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" -o bin/manager cmd/main.go


RUN_METRICS_ADDR=":8080"
RUN_HEALTH_ADDR=":8081"
RUN_DELEGATION_ROLE="primary"
DEFAULT_RUN_FLAGS ?= --zap-devel --provider inmemory,aws,google,azure,coredns,endpoint --delegation-role=${RUN_DELEGATION_ROLE} --metrics-bind-address=${RUN_METRICS_ADDR} --health-probe-bind-address=${RUN_HEALTH_ADDR}
RUN_FLAGS ?= $(DEFAULT_RUN_FLAGS)

.PHONY: run
run: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
run: DIRTY=$(shell hack/check-git-dirty.sh || echo "unknown")
run: manifests generate fmt vet ## Run a controller from your host.
	go run -ldflags "-X main.version=v${VERSION} -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" --race ./cmd/main.go ${RUN_FLAGS}

.PHONY: run-primary
run-primary: run ## Run a controller from your host with the primary delegation role(default).

.PHONY: run-secondary
run-secondary: ## Run a controller from your host with the secondary delegation role.
	$(MAKE) run RUN_FLAGS="${DEFAULT_RUN_FLAGS} --delegation-role=secondary --metrics-bind-address=:8082 --health-probe-bind-address=:8083"

.PHONY: run-with-probes
run-with-probes: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
run-with-probes: DIRTY=$(shell hack/check-git-dirty.sh || echo "unknown")
run-with-probes: manifests generate fmt vet ## Run a controller from your host.
	go run -ldflags "-X main.version=v${VERSION} -X main.gitSHA=${GIT_SHA} -X main.dirty=${DIRTY}" --race  ./cmd/main.go --zap-devel --provider inmemory,aws,google,azure

# If you wish built the manager image targeting other platforms you can use the --platform flag.
# (i.e. docker build --platform linux/arm64 ). However, you must enable docker buildKit for it.
# More info: https://docs.docker.com/develop/develop-images/build_enhancements/
.PHONY: docker-build
docker-build: GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")
docker-build: DIRTY=$(shell hack/check-git-dirty.sh || echo "unknown")
docker-build: ## Build docker image with the manager.
	$(CONTAINER_TOOL) build -t ${IMG} . --build-arg VERSION=v$(VERSION) --build-arg GIT_SHA=$(GIT_SHA) --build-arg DIRTY=$(DIRTY)

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	$(CONTAINER_TOOL) push ${IMG}

# PLATFORMS defines the target platforms for  the manager image be build to provide support to multiple
# architectures. (i.e. make docker-buildx IMG=myregistry/mypoperator:0.0.1). To use this option you need to:
# - able to use docker buildx . More info: https://docs.docker.com/build/buildx/
# - have enable BuildKit, More info: https://docs.docker.com/develop/develop-images/build_enhancements/
# - be able to push the image for your registry (i.e. if you do not inform a valid value via IMG=<myregistry/image:<tag>> then the export will fail)
# To properly provided solutions that supports more than one platform you should use this option.
PLATFORMS ?= linux/arm64,linux/amd64,linux/s390x,linux/ppc64le
.PHONY: docker-buildx
docker-buildx: test ## Build and push docker image for the manager for cross-platform support
	# copy existing Dockerfile and insert --platform=${BUILDPLATFORM} into Dockerfile.cross, and preserve the original Dockerfile
	sed -e '1 s/\(^FROM\)/FROM --platform=\$$\{BUILDPLATFORM\}/; t' -e ' 1,// s//FROM --platform=\$$\{BUILDPLATFORM\}/' Dockerfile > Dockerfile.cross
	- $(CONTAINER_TOOL) buildx create --name project-v3-builder
	$(CONTAINER_TOOL) buildx use project-v3-builder
	- $(CONTAINER_TOOL) buildx build --push --platform=$(PLATFORMS) --tag ${IMG} -f Dockerfile.cross .
	- $(CONTAINER_TOOL) buildx rm project-v3-builder
	rm Dockerfile.cross

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
ifeq ($(DELEGATION_MODE), secondary)
deploy: DEPLOY_KUSTOMIZATION=config/local-setup/dns-operator/secondary
else
deploy: DEPLOY_KUSTOMIZATION=config/deploy/local
endif
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build ${DEPLOY_KUSTOMIZATION} | $(KUBECTL) apply -f -

.PHONY: deploy-namespaced
deploy-namespaced: manifests kustomize remove-cluster-overlay generate-cluster-overlay ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}
	$(KUSTOMIZE) build --enable-helm $(CLUSTER_OVERLAY_DIR)/$(KIND_CLUSTER_NAME) | $(KUBECTL) apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/deploy/local | $(KUBECTL) delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: install-olm
install-olm: operator-sdk
	$(OPERATOR_SDK) olm install

.PHONY: uninstall-olm
uninstall-olm: operator-sdk
	$(OPERATOR_SDK) olm uninstall

deploy-catalog: kustomize yq ## Deploy operator to the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | $(KUBECTL) apply -f -

undeploy-catalog: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | $(KUBECTL) delete -f -

##@ Build Dependencies

## Location to install dependencies to
LOCALBIN ?= $(shell pwd)/bin
$(LOCALBIN):
	mkdir -p $(LOCALBIN)

## Tool Binaries
KUBECTL ?= kubectl
KUSTOMIZE ?= $(LOCALBIN)/kustomize
CONTROLLER_GEN ?= $(LOCALBIN)/controller-gen
ENVTEST ?= $(LOCALBIN)/setup-envtest
GOVULNCHECK ?= $(LOCALBIN)/govulncheck
OPENSHIFT_GOIMPORTS ?= $(LOCALBIN)/openshift-goimports
KIND = $(LOCALBIN)/kind
ACT = $(LOCALBIN)/act
YQ = $(LOCALBIN)/yq
GINKGO ?= $(LOCALBIN)/ginkgo
GOLANGCI_LINT ?= $(LOCALBIN)/golangci-lint
HELM ?= $(LOCALBIN)/helm
KUBE_BURNER ?= $(LOCALBIN)/kube-burner
KUBECTL_DNS ?= $(LOCALBIN)/kubectl_kuadrant-dns

## Tool Versions
KUSTOMIZE_VERSION ?= v5.5.0
CONTROLLER_TOOLS_VERSION ?= v0.14.0
OPENSHIFT_GOIMPORTS_VERSION ?= c70783e636f2213cac683f6865d88c5edace3157
KIND_VERSION = v0.30.0
ACT_VERSION = latest
YQ_VERSION := v4.34.2
GINKGO_VERSION ?= v2.22.0
GOLANGCI_LINT_VERSION ?= v2.1.6
HELM_VERSION = v3.15.0
KUBE_BURNER_VERSION = v1.11.1

.PHONY: kustomize
kustomize: $(KUSTOMIZE) ## Download kustomize locally if necessary. If wrong version is installed, it will be removed before downloading.
$(KUSTOMIZE): $(LOCALBIN)
	@if test -x $(LOCALBIN)/kustomize && ! $(LOCALBIN)/kustomize version | grep -q $(KUSTOMIZE_VERSION); then \
		echo "$(LOCALBIN)/kustomize version is not expected $(KUSTOMIZE_VERSION). Removing it before installing."; \
		rm -rf $(LOCALBIN)/kustomize; \
	fi
	test -s $(LOCALBIN)/kustomize || GOBIN=$(LOCALBIN) GO111MODULE=on go install sigs.k8s.io/kustomize/kustomize/v5@$(KUSTOMIZE_VERSION)

.PHONY: controller-gen
controller-gen: $(CONTROLLER_GEN) ## Download controller-gen locally if necessary. If wrong version is installed, it will be overwritten.
$(CONTROLLER_GEN): $(LOCALBIN)
	test -s $(LOCALBIN)/controller-gen && $(LOCALBIN)/controller-gen --version | grep -q $(CONTROLLER_TOOLS_VERSION) || \
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-tools/cmd/controller-gen@$(CONTROLLER_TOOLS_VERSION)

.PHONY: envtest
envtest: $(ENVTEST) ## Download envtest-setup locally if necessary.
$(ENVTEST): $(LOCALBIN)
	test -s $(LOCALBIN)/setup-envtest || GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

.PHONY: govulncheck
govulncheck: $(GOVULNCHECK) ## Download govulncheck locally if necessary.
$(GOVULNCHECK): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install golang.org/x/vuln/cmd/govulncheck@latest

.PHONY: operator-sdk
OPERATOR_SDK ?= $(LOCALBIN)/operator-sdk
operator-sdk: ## Download operator-sdk locally if necessary.
ifeq (,$(wildcard $(OPERATOR_SDK)))
ifeq (, $(shell which operator-sdk 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPERATOR_SDK)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPERATOR_SDK) https://github.com/operator-framework/operator-sdk/releases/download/$(OPERATOR_SDK_VERSION)/operator-sdk_$${OS}_$${ARCH} ;\
	chmod +x $(OPERATOR_SDK) ;\
	}
else
OPERATOR_SDK = $(shell which operator-sdk)
endif
endif

.PHONY: openshift-goimports
openshift-goimports: $(OPENSHIFT_GOIMPORTS) ## Download openshift-goimports locally if necessary
$(OPENSHIFT_GOIMPORTS): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/openshift-eng/openshift-goimports@$(OPENSHIFT_GOIMPORTS_VERSION)

.PHONY: kind
kind: $(KIND) ## Download kind locally if necessary.
$(KIND): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/kind@$(KIND_VERSION)

.PHONY: act
act: $(ACT) ## Download act locally if necessary.
$(ACT): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/nektos/act@$(ACT_VERSION)

.PHONY: yq
yq: $(YQ) ## Download yq locally if necessary.
$(YQ): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install github.com/mikefarah/yq/v4@$(YQ_VERSION)

.PHONY: helm
helm: $(HELM) ## Download helm locally if necessary.
$(HELM):
	@{ \
	set -e ;\
	mkdir -p $(dir $(HELM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	wget -O helm.tar.gz https://get.helm.sh/helm-$(HELM_VERSION)-$${OS}-$${ARCH}.tar.gz ;\
	tar -zxvf helm.tar.gz ;\
	mv $${OS}-$${ARCH}/helm $(HELM) ;\
	chmod +x $(HELM) ;\
	rm -rf $${OS}-$${ARCH} helm.tar.gz ;\
	}

.PHONY: ginkgo
ginkgo: $(GINKGO) ## Download ginkgo locally if necessary
$(GINKGO): $(LOCALBIN)
	GOBIN=$(LOCALBIN) go install -mod=mod github.com/onsi/ginkgo/v2/ginkgo@$(GINKGO_VERSION)

.PHONY: golangci-lint
golangci-lint: $(GOLANGCI_LINT) ## Download golangci-lint locally if necessary.
$(GOLANGCI_LINT): $(LOCALBIN)
	curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $(LOCALBIN) $(GOLANGCI_LINT_VERSION)

.PHONY: kube-burner
kube-burner: $(KUBE_BURNER) ## Download kube-burner locally if necessary.
$(KUBE_BURNER):
	@{ \
	set -e ;\
	mkdir -p $(dir $(KUBE_BURNER)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	wget -O kube-burner.tar.gz https://github.com/kube-burner/kube-burner/releases/download/v1.11.1/kube-burner-V1.11.1-linux-x86_64.tar.gz ;\
	tar -zxvf kube-burner.tar.gz ;\
	mv kube-burner $(KUBE_BURNER) ;\
	chmod +x $(KUBE_BURNER) ;\
	rm -rf $${OS}-$${ARCH} kube-burner.tar.gz ;\
	}

.PHONY: kubectl-dns
kubectl-dns: $(KUBECTL_DNS) ## Build the kubectl_kuadrant-dns locally if required.
$(KUBECTL_DNS):
	$(MAKE) build-cli


.PHONY: bundle
bundle: manifests manifests-gen-base-csv kustomize operator-sdk ## Generate bundle manifests and metadata, then validate generated files.
	$(OPERATOR_SDK) generate kustomize manifests -q
	cd config/manager && $(KUSTOMIZE) edit set image controller=$(IMG)
	$(KUSTOMIZE) build config/manifests | $(OPERATOR_SDK) generate bundle $(BUNDLE_GEN_FLAGS)
	$(MAKE) bundle-post-generate
	$(OPERATOR_SDK) bundle validate ./bundle
	$(MAKE) bundle-ignore-createdAt

bundle-operator-image-url: $(YQ) ## Read operator image reference URL from the manifest bundle.
	@$(YQ) '.metadata.annotations.containerImage' bundle/manifests/dns-operator.clusterserviceversion.yaml

# Since operator-sdk 1.26.0, `make bundle` changes the `createdAt` field from the bundle
# even if it is patched:
#   https://github.com/operator-framework/operator-sdk/pull/6136
# This code checks if only the createdAt field. If is the only change, it is ignored.
# Else, it will do nothing.
# https://github.com/operator-framework/operator-sdk/issues/6285#issuecomment-1415350333
# https://github.com/operator-framework/operator-sdk/issues/6285#issuecomment-1532150678
.PHONY: bundle-ignore-createdAt
bundle-ignore-createdAt:
	git diff --quiet -I'^    createdAt: ' ./bundle && git checkout ./bundle || true

.PHONY: bundle-post-generate
bundle-post-generate:
	$(YQ) -i '.annotations."com.redhat.openshift.versions" = "v4.12-v4.14"' bundle/metadata/annotations.yaml
	V="$(CATALOG_IMG)" $(YQ) eval '.spec.image = strenv(V)' -i config/deploy/olm/catalogsource.yaml
	@if [ "$(CHANNELS)" != "" ]; then\
		V="$(CHANNELS)" $(YQ) eval '.spec.channel = strenv(V)' -i config/deploy/olm/subscription.yaml; \
	fi

.PHONY: bundle-build
bundle-build: ## Build the bundle image.
	docker build -f bundle.Dockerfile -t $(BUNDLE_IMG) .

.PHONY: bundle-push
bundle-push: ## Push the bundle image.
	$(MAKE) docker-push IMG=$(BUNDLE_IMG)

.PHONY: opm
OPM = $(LOCALBIN)/opm
opm: ## Download opm locally if necessary.
ifeq (,$(wildcard $(OPM)))
ifeq (,$(shell which opm 2>/dev/null))
	@{ \
	set -e ;\
	mkdir -p $(dir $(OPM)) ;\
	OS=$(shell go env GOOS) && ARCH=$(shell go env GOARCH) && \
	curl -sSLo $(OPM) https://github.com/operator-framework/operator-registry/releases/download/v1.23.0/$${OS}-$${ARCH}-opm ;\
	chmod +x $(OPM) ;\
	}
else
OPM = $(shell which opm)
endif
endif

# A comma-separated list of bundle images (e.g. make catalog-build BUNDLE_IMGS=example.com/operator-bundle:v0.1.0,example.com/operator-bundle:v0.2.0).
# These images MUST exist in a registry and be pull-able.
BUNDLE_IMGS ?= $(BUNDLE_IMG)

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:latest

# Set CATALOG_BASE_IMG to an existing catalog image tag to add $BUNDLE_IMGS to that image.
ifneq ($(origin CATALOG_BASE_IMG), undefined)
FROM_INDEX_OPT := --from-index $(CATALOG_BASE_IMG)
endif

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# This recipe invokes 'opm' in 'semver' bundle add mode. For more information on add modes, see:
# https://github.com/operator-framework/community-operators/blob/7f1438c/docs/packaging-operator.md#updating-your-existing-operator
.PHONY: catalog-build
catalog-build: opm ## Build a catalog image.
	mkdir -p tmp/catalog
	cd tmp/catalog && $(OPM) index add --container-tool docker --mode semver --bundles $(BUNDLE_IMGS) $(FROM_INDEX_OPT) --generate
	cd tmp/catalog && docker build -t $(CATALOG_IMG) -f index.Dockerfile .

print-bundle-image: ## Pring bundle images.
	@echo $(BUNDLE_IMG)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

##@ Release

.PHONY: prepare-release
RELEASE_FILE = $(shell pwd)/make/release.mk
prepare-release: IMG_TAG=v$(VERSION)
prepare-release: ## Generates a makefile that will override environment variables for a specific release and runs bundle.
	echo -e "#Release default values\\nIMG=$(IMAGE_TAG_BASE):$(IMG_TAG)\nBUNDLE_IMG=$(IMAGE_TAG_BASE)-bundle:$(IMG_TAG)\n\
	CATALOG_IMG=$(IMAGE_TAG_BASE)-catalog:$(IMG_TAG)\nCHANNELS=$(CHANNELS)\nBUNDLE_CHANNELS=--channels=$(CHANNELS)" > $(RELEASE_FILE)
	$(MAKE) bundle
	$(MAKE) helm-build VERSION=$(VERSION)

.PHONY: read-release-version
read-release-version: ## Reads release version
	@echo "v$(VERSION)"

# Include last to avoid changing MAKEFILE_LIST used above
include ./make/*.mk
