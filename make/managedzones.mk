
##@ ManagedZones

## Targets to help configure ManagedZones for local-setup

define patch-config
	envsubst \
		< $1 \
		> $2
endef

ndef = $(if $(value $(1)),,$(error $(1) not set))


LOCAL_SETUP_AWS_MZ_DIR=config/local-setup/managedzone/aws
LOCAL_SETUP_AWS_MZ_CONFIG=${LOCAL_SETUP_AWS_MZ_DIR}/managed-zone-config.env
LOCAL_SETUP_AWS_MZ_CREDS=${LOCAL_SETUP_AWS_MZ_DIR}/aws-credentials.env

LOCAL_SETUP_GCP_MZ_DIR=config/local-setup/managedzone/gcp
LOCAL_SETUP_GCP_MZ_CONFIG=${LOCAL_SETUP_GCP_MZ_DIR}/managed-zone-config.env
LOCAL_SETUP_GCP_MZ_CREDS=${LOCAL_SETUP_GCP_MZ_DIR}/gcp-credentials.env

LOCAL_SETUP_AZURE_MZ_DIR=config/local-setup/managedzone/azure
LOCAL_SETUP_AZURE_MZ_CONFIG=${LOCAL_SETUP_AZURE_MZ_DIR}/managed-zone-config.env
LOCAL_SETUP_AZURE_MZ_CREDS=${LOCAL_SETUP_AZURE_MZ_DIR}/azure-credentials.env

.PHONY: local-setup-aws-mz-generate
local-setup-aws-mz-generate: local-setup-aws-mz-config local-setup-aws-mz-credentials ## Generate AWS ManagedZone configuration and credentials for local-setup

.PHONY: local-setup-aws-mz-clean
local-setup-aws-mz-clean: ## Remove AWS ManagedZone configuration and credentials
	rm -f ${LOCAL_SETUP_AWS_MZ_CONFIG}
	rm -f ${LOCAL_SETUP_AWS_MZ_CREDS}

.PHONY: local-setup-aws-mz-config
local-setup-aws-mz-config: $(LOCAL_SETUP_AWS_MZ_CONFIG)
$(LOCAL_SETUP_AWS_MZ_CONFIG):
	$(call ndef,AWS_DNS_PUBLIC_ZONE_ID)
	$(call ndef,AWS_ZONE_ROOT_DOMAIN)
	$(call patch-config,${LOCAL_SETUP_AWS_MZ_CONFIG}.template,${LOCAL_SETUP_AWS_MZ_CONFIG})

.PHONY: local-setup-aws-mz-credentials
local-setup-aws-mz-credentials: $(LOCAL_SETUP_AWS_MZ_CREDS)
$(LOCAL_SETUP_AWS_MZ_CREDS):
	$(call ndef,AWS_ACCESS_KEY_ID)
	$(call ndef,AWS_SECRET_ACCESS_KEY)
	$(call patch-config,${LOCAL_SETUP_AWS_MZ_CREDS}.template,${LOCAL_SETUP_AWS_MZ_CREDS})

.PHONY: local-setup-gcp-mz-generate
local-setup-gcp-mz-generate: local-setup-gcp-mz-config local-setup-gcp-mz-credentials ## Generate GCP ManagedZone configuration and credentials for local-setup

.PHONY: local-setup-gcp-mz-clean
local-setup-gcp-mz-clean: ## Remove GCP ManagedZone configuration and credentials
	rm -f ${LOCAL_SETUP_GCP_MZ_CONFIG}
	rm -f ${LOCAL_SETUP_GCP_MZ_CREDS}

.PHONY: local-setup-gcp-mz-config
local-setup-gcp-mz-config: $(LOCAL_SETUP_GCP_MZ_CONFIG)
$(LOCAL_SETUP_GCP_MZ_CONFIG):
	$(call ndef,GCP_ZONE_NAME)
	$(call ndef,GCP_ZONE_DNS_NAME)
	$(call patch-config,${LOCAL_SETUP_GCP_MZ_CONFIG}.template,${LOCAL_SETUP_GCP_MZ_CONFIG})

.PHONY: local-setup-gcp-mz-credentials
local-setup-gcp-mz-credentials: $(LOCAL_SETUP_GCP_MZ_CREDS)
$(LOCAL_SETUP_GCP_MZ_CREDS):
	$(call ndef,GCP_GOOGLE_CREDENTIALS)
	$(call ndef,GCP_PROJECT_ID)
	$(call patch-config,${LOCAL_SETUP_GCP_MZ_CREDS}.template,${LOCAL_SETUP_GCP_MZ_CREDS})


.PHONY: local-setup-azure-mz-generate
local-setup-azure-mz-generate: local-setup-azure-mz-config local-setup-azure-mz-credentials ## Generate Azure ManagedZone configuration and credentials for local-setup

.PHONY: local-setup-azure-mz-clean
local-setup-azure-mz-clean: ## Remove Azure ManagedZone configuration and credentials
	rm -f ${LOCAL_SETUP_AZURE_MZ_CONFIG}
	rm -f ${LOCAL_SETUP_AZURE_MZ_CREDS}

.PHONY: local-setup-azure-mz-config
local-setup-azure-mz-config: $(LOCAL_SETUP_AZURE_MZ_CONFIG)
$(LOCAL_SETUP_AZURE_MZ_CONFIG):
	$(call ndef,KUADRANT_AZURE_DNS_ZONE_ID)
	$(call patch-config,${LOCAL_SETUP_AZURE_MZ_CONFIG}.template,${LOCAL_SETUP_AZURE_MZ_CONFIG})

.PHONY: local-setup-azure-mz-credentials
local-setup-azure-mz-credentials: $(LOCAL_SETUP_AZURE_MZ_CREDS)
$(LOCAL_SETUP_AZURE_MZ_CREDS):
	$(call ndef,KUADRANT_AZURE_CREDENTIALS)
	$(call patch-config,${LOCAL_SETUP_AZURE_MZ_CREDS}.template,${LOCAL_SETUP_AZURE_MZ_CREDS})

.PHONY: local-setup-managedzones
local-setup-managedzones: TARGET_NAMESPACE=dnstest
local-setup-managedzones: kustomize ## Create AWS, Azure and GCP managedzones in the 'TARGET_NAMESPACE' namespace
	@if [[ -f ${LOCAL_SETUP_GCP_MZ_CONFIG} && -f ${LOCAL_SETUP_GCP_MZ_CREDS} ]]; then\
		echo "local-setup: creating managedzone for gcp config and credentials in ${TARGET_NAMESPACE}";\
		${KUSTOMIZE} build ${LOCAL_SETUP_GCP_MZ_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply -f -;\
	fi
	@if [[ -f ${LOCAL_SETUP_AWS_MZ_CONFIG} && -f ${LOCAL_SETUP_AWS_MZ_CREDS} ]]; then\
		echo "local-setup: creating managedzone for aws config and credentials in ${TARGET_NAMESPACE}";\
		${KUSTOMIZE} build ${LOCAL_SETUP_AWS_MZ_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -;\
	fi
	@if [[ -f ${LOCAL_SETUP_AZURE_MZ_CONFIG} && -f ${LOCAL_SETUP_AZURE_MZ_CREDS} ]]; then\
		echo "local-setup: creating managedzone for azure config and credentials in ${TARGET_NAMESPACE}";\
		${KUSTOMIZE} build ${LOCAL_SETUP_AZURE_MZ_DIR} | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -;\
	fi
