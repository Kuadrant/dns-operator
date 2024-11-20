
##@ Kustomize Overlay Generation

## Targets to help create deployment kustomizations (overlays)

CLUSTER_NAME ?= $(KIND_CLUSTER_NAME)

DEPLOYMENT_COUNT ?= 2
DEPLOYMENT_NAMESPACE ?= kuadrant-dns-operator
DEPLOYMENT_NAME_SUFFIX ?= 1
DEPLOYMENT_WATCH_NAMESPACES ?=

GCP_CREDENTIALS_FILE ?= config/local-setup/dns-provider/gcp/gcp-credentials.env
AWS_CREDENTIALS_FILE ?= config/local-setup/dns-provider/aws/aws-credentials.env
AZURE_CREDENTIALS_FILE ?= config/local-setup/dns-provider/azure/azure-credentials.env

## Location to generate cluster overlays
CLUSTER_OVERLAY_DIR ?= $(shell pwd)/tmp/overlays
$(CLUSTER_OVERLAY_DIR):
	mkdir -p $(CLUSTER_OVERLAY_DIR)

USE_REMOTE_CONFIG ?= false
DNS_OPERATOR_GITREF ?= main

config_path_for = $(shell if [ $(USE_REMOTE_CONFIG) = 'true' ]; then echo "github.com/kuadrant/dns-operator/$(1)?ref=$(DNS_OPERATOR_GITREF)"; else realpath -m --relative-to=$(2) $(shell pwd)/$(1); fi)

.PHONY: generate-cluster-overlay
generate-cluster-overlay: ## Generate a cluster overlay with namespaced deployments for the current cluster (CLUSTER_NAME)
	# Generate cluster overlay
	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME) && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/crd",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME))

	# Generate common dns provider kustomization
	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add secret dns-provider-credentials-inmemory --disableNameSuffixHash --from-literal=INMEM_INIT_ZONES=kuadrant.local --type "kuadrant.io/inmemory"

	# Add dns providers that require credentials if credential files exist
	@if [[ -f $(GCP_CREDENTIALS_FILE) ]]; then\
		cp config/local-setup/dns-provider/gcp/gcp-credentials.env $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers/ ;\
		cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers && \
		$(KUSTOMIZE) edit add secret dns-provider-credentials-gcp --disableNameSuffixHash --from-env-file=gcp-credentials.env --type "kuadrant.io/gcp" ;\
	fi
	@if [[ -f $(AWS_CREDENTIALS_FILE) ]]; then\
		cp config/local-setup/dns-provider/aws/aws-credentials.env $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers/ ;\
		cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers && \
		$(KUSTOMIZE) edit add secret dns-provider-credentials-aws --disableNameSuffixHash --from-env-file=aws-credentials.env --type "kuadrant.io/aws" ;\
	fi
	@if [[ -f $(AZURE_CREDENTIALS_FILE) ]]; then\
		cp config/local-setup/dns-provider/azure/azure-credentials.env $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers/ ;\
		cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-providers && \
		$(KUSTOMIZE) edit add secret dns-provider-credentials-azure --disableNameSuffixHash --from-env-file=azure-credentials.env --type "kuadrant.io/azure" ;\
	fi

	# Add dns operator deployments based on the number of deployments requested
	@n=1 ; while [[ $$n -le $(DEPLOYMENT_COUNT) ]] ; do \
		$(MAKE) -s generate-operator-deployment-overlay DEPLOYMENT_NAME_SUFFIX=$$n DEPLOYMENT_NAMESPACE=${DEPLOYMENT_NAMESPACE}-$$n DEPLOYMENT_WATCH_NAMESPACES=${DEPLOYMENT_NAMESPACE}-$$n ;\
		cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME) && $(KUSTOMIZE) edit add resource ${DEPLOYMENT_NAMESPACE}-$$n && cd - > /dev/null ;\
		((n = n + 1)) ;\
	done ;\

.PHONY: remove-cluster-overlay
remove-cluster-overlay: ## Remove an existing cluster overlay for the current cluster (CLUSTER_NAME)
	rm -rf $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)

.PHONY: remove-all-cluster-overlays
remove-all-cluster-overlays: ## Remove all existing cluster overlays (kuadrant-dns-local*)
	rm -rf $(CLUSTER_OVERLAY_DIR)/kuadrant-dns-local*

.PHONY: generate-operator-deployment-overlay
generate-operator-deployment-overlay: DEPLOYMENT_REPLICAS=1
generate-operator-deployment-overlay: ## Generate a DNS Operator deployment overlay for the current cluster (CLUSTER_NAME)
	# Generate dns-operator deployment overlay
	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-operator
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-operator && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/local-setup/dns-operator",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-operator) && \
	$(KUSTOMIZE) edit add resource $(call config_path_for,"config/prometheus",$(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/dns-operator)

	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/$(DEPLOYMENT_NAMESPACE)/dns-operator
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/$(DEPLOYMENT_NAMESPACE)/dns-operator && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit add resource "../../dns-operator" && \
	$(KUSTOMIZE) edit set namesuffix -- -$(DEPLOYMENT_NAME_SUFFIX)  && \
	$(KUSTOMIZE) edit add patch --kind Deployment --patch '[{"op": "replace", "path": "/spec/template/spec/containers/0/env/0", "value": {"name": "WATCH_NAMESPACES", "value": "$(DEPLOYMENT_WATCH_NAMESPACES)"}}]' && \
	$(KUSTOMIZE) edit add patch --kind Deployment --patch '[{"op": "replace", "path": "/spec/replicas", "value": ${DEPLOYMENT_REPLICAS}}]'

	mkdir -p $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/$(DEPLOYMENT_NAMESPACE)/dns-providers
	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/$(DEPLOYMENT_NAMESPACE)/dns-providers && \
	touch kustomization.yaml && \
    $(KUSTOMIZE) edit add resource "../../dns-providers"

	cd $(CLUSTER_OVERLAY_DIR)/$(CLUSTER_NAME)/$(DEPLOYMENT_NAMESPACE) && \
	touch kustomization.yaml && \
	$(KUSTOMIZE) edit set namespace $(DEPLOYMENT_NAMESPACE)  && \
	$(KUSTOMIZE) edit add resource "./dns-operator" && \
	$(KUSTOMIZE) edit add resource "./dns-providers" && \
	$(KUSTOMIZE) edit add label -f app.kubernetes.io/part-of:kuadrant
