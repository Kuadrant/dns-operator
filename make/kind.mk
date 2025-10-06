
##@ Kind

## Targets to help install and use kind for development https://kind.sigs.k8s.io

KIND_CLUSTER_NAME_PREFIX ?= kuadrant-dns-local
KIND_CLUSTER_NAME ?= $(KIND_CLUSTER_NAME_PREFIX)

## Location to generate cluster kubeconfigs
KUBECONFIGS_DIR ?= $(shell pwd)/tmp/kubeconfigs
KIND_ALL_KUBECONFIG=$(KUBECONFIGS_DIR)/kuadrant-local-all.kubeconfig
KIND_ALL_INTERNAL_KUBECONFIG=$(KUBECONFIGS_DIR)/kuadrant-local-all.internal.kubeconfig

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create the "kuadrant-dns-local" kind cluster.
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config hack/kind-cluster.yaml
	$(KIND) export kubeconfig -q -n $(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_KUBECONFIG)
	$(KIND) export kubeconfig -q --internal -n $(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_INTERNAL_KUBECONFIG)

.PHONY: kind-delete-cluster
kind-delete-cluster: kind ## Delete the "kuadrant-dns-local" kind cluster.
	- $(KIND) delete cluster --name $(KIND_CLUSTER_NAME)
	- kubectl config delete-context kind-$(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_INTERNAL_KUBECONFIG) || true
	- kubectl config delete-cluster kind-$(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_INTERNAL_KUBECONFIG) || true
	- kubectl config delete-user kind-$(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_INTERNAL_KUBECONFIG) || true
	- kubectl config delete-context kind-$(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_KUBECONFIG) || true
	- kubectl config delete-cluster kind-$(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_KUBECONFIG) || true
	- kubectl config delete-user kind-$(KIND_CLUSTER_NAME) --kubeconfig $(KIND_ALL_KUBECONFIG) || true

.PHONY: kind-delete-all-clusters
kind-delete-all-clusters: kind ## Delete the all "kuadrant-dns-local*" kind clusters.
	- $(KIND) get clusters | grep $(KIND_CLUSTER_NAME_PREFIX) | xargs -I % sh -c "$(KIND) delete cluster --name %"
	- rm -f $(KIND_ALL_INTERNAL_KUBECONFIG)
	- rm -f $(KIND_ALL_KUBECONFIG)

.PHONY: kind-load-image
kind-load-image: kind ## Load image to "kuadrant-dns-local" kind cluster.
	$(eval TMP_DIR := $(shell mktemp -d))
	$(CONTAINER_TOOL) save -o $(TMP_DIR)/image.tar $(IMG) \
	   && KIND_EXPERIMENTAL_PROVIDER=$(CONTAINER_TOOL) $(KIND) load image-archive $(TMP_DIR)/image.tar --name $(KIND_CLUSTER_NAME) ; \
	   EXITVAL=$$? ; \
	   rm -rf $(TMP_DIR) ; \
	   exit $${EXITVAL}
