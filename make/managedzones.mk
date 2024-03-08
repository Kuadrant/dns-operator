
##@ ManagedZones

## Targets to help configure ManagedZones for local-setup

define patch-config
	envsubst \
		< $1 \
		> $2
endef

ndef = $(if $(value $(1)),,$(error $(1) not set))

LOCAL_SETUP_AWS_MZ_CONFIG=config/local-setup/managedzone/aws/managed-zone-config.env
LOCAL_SETUP_AWS_MZ_CREDS=config/local-setup/managedzone/aws/aws-credentials.env
LOCAL_SETUP_GCP_MZ_CONFIG=config/local-setup/managedzone/gcp/managed-zone-config.env
LOCAL_SETUP_GCP_MZ_CREDS=config/local-setup/managedzone/gcp/gcp-credentials.env

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

.PHONY: local-setup-managedzones
local-setup-managedzones: TARGET_NAMESPACE=dnstest
local-setup-managedzones: kustomize ## Create AWS and GCP managedzones in the 'TARGET_NAMESPACE' namespace
	@if [[ -f "config/local-setup/managedzone/gcp/managed-zone-config.env" && -f "config/local-setup/managedzone/gcp/gcp-credentials.env" ]]; then\
		echo "local-setup: creating managedzone for gcp config and credentials in ${TARGET_NAMESPACE}";\
		${KUSTOMIZE} build config/local-setup/managedzone/gcp | $(KUBECTL) -n ${TARGET_NAMESPACE} apply -f -;\
	fi
	@if [[ -f "config/local-setup/managedzone/aws/managed-zone-config.env" && -f "config/local-setup/managedzone/aws/aws-credentials.env" ]]; then\
		echo "local-setup: creating managedzone for aws config and credentials in ${TARGET_NAMESPACE}";\
		${KUSTOMIZE} build config/local-setup/managedzone/aws | $(KUBECTL) -n ${TARGET_NAMESPACE} apply  -f -;\
	fi
