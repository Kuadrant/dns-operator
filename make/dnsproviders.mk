
##@ DNS Providers

## Targets to help configure DNS Providers for local-setup

define patch-config
	envsubst \
		< $1 \
		> $2
endef

ndef = $(if $(value $(1)),,$(error $(1) not set))

LOCAL_SETUP_AWS_DIR=config/local-setup/dns-provider/aws
LOCAL_SETUP_GCP_DIR=config/local-setup/dns-provider/gcp
LOCAL_SETUP_AZURE_DIR=config/local-setup/dns-provider/azure
LOCAL_SETUP_INMEM_DIR=config/local-setup/dns-provider/inmemory
LOCAL_SETUP_COREDNS_DIR=config/local-setup/dns-provider/coredns
LOCAL_SETUP_AWS_CREDS=${LOCAL_SETUP_AWS_DIR}/aws-credentials.env
LOCAL_SETUP_GCP_CREDS=${LOCAL_SETUP_GCP_DIR}/gcp-credentials.env
LOCAL_SETUP_AZURE_CREDS=${LOCAL_SETUP_AZURE_DIR}/azure-credentials.env
LOCAL_SETUP_COREDNS_CREDS=${LOCAL_SETUP_COREDNS_DIR}/coredns-credentials.env

.PHONY: local-setup-aws-generate
local-setup-aws-generate: local-setup-aws-credentials ## Generate AWS DNS Provider credentials for local-setup

.PHONY: local-setup-aws-clean
local-setup-aws-clean: ## Remove AWS DNS Provider credentials
	rm -f ${LOCAL_SETUP_AWS_CREDS}

.PHONY: local-setup-aws-credentials
local-setup-aws-credentials: $(LOCAL_SETUP_AWS_CREDS)
$(LOCAL_SETUP_AWS_CREDS):
	$(call ndef,AWS_ACCESS_KEY_ID)
	$(call ndef,AWS_SECRET_ACCESS_KEY)
	$(call patch-config,${LOCAL_SETUP_AWS_CREDS}.template,${LOCAL_SETUP_AWS_CREDS})

.PHONY: local-setup-gcp-generate
local-setup-gcp-generate: local-setup-gcp-credentials ## Generate GCP DNS Provider credentials for local-setup

.PHONY: local-setup-gcp-clean
local-setup-gcp-clean: ## Remove GCP DNS Provider credentials
	rm -f ${LOCAL_SETUP_GCP_CREDS}

.PHONY: local-setup-gcp-credentials
local-setup-gcp-credentials: $(LOCAL_SETUP_GCP_CREDS)
$(LOCAL_SETUP_GCP_CREDS):
	$(call ndef,GCP_GOOGLE_CREDENTIALS)
	$(call ndef,GCP_PROJECT_ID)
	$(call patch-config,${LOCAL_SETUP_GCP_CREDS}.template,${LOCAL_SETUP_GCP_CREDS})

.PHONY: local-setup-azure-generate
local-setup-azure-generate: local-setup-azure-credentials ## Generate Azure DNS Provider credentials for local-setup

.PHONY: local-setup-azure-clean
local-setup-azure-clean: ## Remove Azure DNS Provider credentials
	rm -f ${LOCAL_SETUP_AZURE_CREDS}

.PHONY: local-setup-azure-credentials
local-setup-azure-credentials: $(LOCAL_SETUP_AZURE_CREDS)
$(LOCAL_SETUP_AZURE_CREDS):
	$(call ndef,KUADRANT_AZURE_CREDENTIALS)
	$(call patch-config,${LOCAL_SETUP_AZURE_CREDS}.template,${LOCAL_SETUP_AZURE_CREDS})

.PHONY: local-setup-coredns-generate-from-clusters
local-setup-coredns-generate-from-clusters: export COREDNS_NAMESERVERS=$(shell hack/coredns-server-list.sh kind-${KIND_CLUSTER_NAME_PREFIX} ${CLUSTER_COUNT} || echo "")
local-setup-coredns-generate-from-clusters: export COREDNS_ZONES=k.example.com
local-setup-coredns-generate-from-clusters: local-setup-coredns-clean local-setup-coredns-generate ## Generate CoreDNS DNS Provider credentials for local-setup from the running coredns services on local kind clusters

.PHONY: local-setup-coredns-generate
local-setup-coredns-generate: local-setup-coredns-credentials ## Generate CoreDNS DNS Provider credentials for local-setup

.PHONY: local-setup-coredns-clean
local-setup-coredns-clean: ## Remove CoreDNS DNS Provider credentials
	rm -f ${LOCAL_SETUP_COREDNS_CREDS}

.PHONY: local-setup-coredns-credentials
local-setup-coredns-credentials: $(LOCAL_SETUP_COREDNS_CREDS)
$(LOCAL_SETUP_COREDNS_CREDS):
	$(call ndef,COREDNS_NAMESERVERS)
	$(call ndef,COREDNS_ZONES)
	$(call patch-config,${LOCAL_SETUP_COREDNS_CREDS}.template,${LOCAL_SETUP_COREDNS_CREDS})

.PHONY: local-setup-dns-providers
local-setup-dns-providers: TARGET_NAMESPACE=dnstest
local-setup-dns-providers: kustomize ## Create AWS, Azure and GCP DNS Providers in the 'TARGET_NAMESPACE' namespace
	@if [[ -f ${LOCAL_SETUP_GCP_CREDS} ]]; then\
		echo "local-setup: creating dns provider for gcp in ${TARGET_NAMESPACE}";\
		$(KUSTOMIZE) build ${LOCAL_SETUP_GCP_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply -f -;\
	fi
	@if [[ -f ${LOCAL_SETUP_AWS_CREDS} ]]; then\
		echo "local-setup: creating dns provider for aws in ${TARGET_NAMESPACE}";\
		$(KUSTOMIZE) build ${LOCAL_SETUP_AWS_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -;\
	fi
	@if [[ -f ${LOCAL_SETUP_AZURE_CREDS} ]]; then\
		echo "local-setup: creating dns provider for azure in ${TARGET_NAMESPACE}";\
		$(KUSTOMIZE) build ${LOCAL_SETUP_AZURE_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -;\
	fi
	@if [[ -f ${LOCAL_SETUP_COREDNS_CREDS} ]]; then\
    	echo "local-setup: creating dns provider for coredns in ${TARGET_NAMESPACE}";\
    	$(KUSTOMIZE) build ${LOCAL_SETUP_COREDNS_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -;\
    fi

	@echo "local-setup: creating dns provider for inmemory in ${TARGET_NAMESPACE}"
	@$(KUSTOMIZE) build ${LOCAL_SETUP_INMEM_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -
