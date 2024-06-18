
##@ Kustomize Overlay Generation

## Targets to help create deployment kustomizations (overlays)

CLUSTER_NAME ?= $(KIND_CLUSTER_NAME)

DEPLOYMENT_COUNT ?= 2
DEPLOYMENT_NAMESPACE ?= dns-operator
DEPLOYMENT_NAME_SUFFIX ?= 1
DEPLOYMENT_WATCH_NAMESPACES ?=

## Location to generate cluster overlays
CLUSTER_OVERLAY_DIR ?= $(shell pwd)/tmp/overlays
$(CLUSTER_OVERLAY_DIR):
	mkdir -p $(CLUSTER_OVERLAY_DIR)

.PHONY: generate-cluster-overlay
generate-cluster-overlay: remove-cluster-overlay ## Generate a cluster overlay with namespaced deployments for the current cluster (CLUSTER_NAME)
	@n=1 ; while [[ $$n -le $(DEPLOYMENT_COUNT) ]] ; do \
		$(MAKE) -s generate-operator-deployment-overlay DEPLOYMENT_NAME_SUFFIX=$$n DEPLOYMENT_NAMESPACE=${DEPLOYMENT_NAMESPACE}-$$n DEPLOYMENT_WATCH_NAMESPACES=${DEPLOYMENT_NAMESPACE}-$$n ;\
		((n = n + 1)) ;\
	done ;\

.PHONY: remove-cluster-overlay
remove-cluster-overlay: ## Remove an existing cluster overlay for the current cluster (CLUSTER_NAME)
	rm -rf $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)

.PHONY: generate-operator-deployment-overlay
generate-operator-deployment-overlay: ## Generate a DNS Operator deployment overlay for the current cluster (CLUSTER_NAME)
	# Generate dns-operator deployment overlay
	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE)/dns-operator
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE)/dns-operator && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add resource "../../../../../config/local-setup/dns-operator" && \
	$(KUSTOMIZE) edit set namesuffix -- -$(DEPLOYMENT_NAME_SUFFIX)  && \
	$(KUSTOMIZE) edit add patch --kind Deployment --patch '[{"op": "replace", "path": "/spec/template/spec/containers/0/env/0", "value": {"name": "WATCH_NAMESPACES", "value": "$(DEPLOYMENT_WATCH_NAMESPACES)"}}]'

	# Generate managedzones overlay
	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE)/managedzones
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE)/managedzones && \
	touch kustomization.yaml

	@if [[ -f "config/local-setup/managedzone/gcp/managed-zone-config.env" && -f "config/local-setup/managedzone/gcp/gcp-credentials.env" ]]; then\
		cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE)/managedzones && \
		$(KUSTOMIZE) edit add resource "../../../../../config/local-setup/managedzone/gcp" ;\
	fi
	@if [[ -f "config/local-setup/managedzone/aws/managed-zone-config.env" && -f "config/local-setup/managedzone/aws/aws-credentials.env" ]]; then\
		cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE)/managedzones && \
		$(KUSTOMIZE) edit add resource "../../../../../config/local-setup/managedzone/aws" ;\
	fi

	# Generate namespace overlay with dns-operator and managedzones resources
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/namespace-$(DEPLOYMENT_NAMESPACE) && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit set namespace $(DEPLOYMENT_NAMESPACE)  && \
	$(KUSTOMIZE) edit add resource "./dns-operator" && \
	$(KUSTOMIZE) edit add resource "./managedzones"

	# Generate cluster overlay with namespace resources
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME) && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add resource namespace-$(DEPLOYMENT_NAMESPACE)
