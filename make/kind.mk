
##@ Kind

## Targets to help install and use kind for development https://kind.sigs.k8s.io

KIND_CLUSTER_NAME_PREFIX ?= kuadrant-dns-local
KIND_CLUSTER_NAME ?= $(KIND_CLUSTER_NAME_PREFIX)

.PHONY: kind-create-cluster
kind-create-cluster: kind ## Create the "kuadrant-dns-local" kind cluster.
	$(KIND) create cluster --name $(KIND_CLUSTER_NAME) --config hack/kind-cluster.yaml

.PHONY: kind-delete-cluster
kind-delete-cluster: kind ## Delete the "kuadrant-dns-local" kind cluster.
	- $(KIND) delete cluster --name $(KIND_CLUSTER_NAME)

.PHONY: kind-load-image
kind-load-image: kind ## Load image to "kuadrant-dns-local" kind cluster.
	$(KIND) load docker-image $(IMG) --name $(KIND_CLUSTER_NAME)
