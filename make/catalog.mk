##@ Operator Catalog

# The image tag given to the resulting catalog image (e.g. make catalog-build CATALOG_IMG=example.com/operator-catalog:v0.2.0).
CATALOG_IMG ?= $(IMAGE_TAG_BASE)-catalog:$(IMAGE_TAG)

CATALOG_FILE = catalog/dns-operator-catalog/operator.yaml
CATALOG_DOCKERFILE = catalog/dns-operator-catalog.Dockerfile

# Quay image default expiry
QUAY_IMAGE_EXPIRY ?= never

# A LABEL that can be appended to a generated Dockerfile to set the Quay image expiration through Docker arguments.
define QUAY_EXPIRY_TIME_LABEL

# Quay image expiry
ARG QUAY_IMAGE_EXPIRY=never
LABEL quay.expires-after=$${QUAY_IMAGE_EXPIRY}
endef
export QUAY_EXPIRY_TIME_LABEL

OPM_DOCKERFILE_TAG ?= latest
$(CATALOG_DOCKERFILE): opm
	-mkdir -p catalog/dns-operator-catalog
	cd catalog && $(OPM) generate dockerfile dns-operator-catalog -l quay.expires-after=$(QUAY_IMAGE_EXPIRY) -i "quay.io/operator-framework/opm:${OPM_DOCKERFILE_TAG}"
	# Inject --pprof-addr="" into both RUN and CMD serve invocations to disable running the pprof server
	sed -i.bak -E '/(opm".*"serve|^CMD \["serve)/s#\]$$#, "--pprof-addr="]#' $(CATALOG_DOCKERFILE) && rm -f $(CATALOG_DOCKERFILE).bak

.PHONY: catalog-dockerfile
catalog-dockerfile: $(CATALOG_DOCKERFILE) ## Generate catalog dockerfile.

$(CATALOG_FILE): opm yq
	@echo "************************************************************"
	@echo Build dns operator catalog
	@echo
	@echo BUNDLE_IMG					= $(BUNDLE_IMG)
	@echo CHANNELS						= $(CHANNELS)
	@echo "************************************************************"
	@echo
	@echo Please check this matches your expectations and override variables if needed.
	@echo
	./utils/generate-catalog.sh $(OPM) $(YQ) $(BUNDLE_IMG) $@ $(CHANNELS)

.PHONY: catalog
catalog: opm ## Generate catalog content and validate.
	# Initializing the Catalog
	-rm -rf catalog/dns-operator-catalog
	-rm -rf catalog/dns-operator-catalog.Dockerfile
	$(MAKE) $(CATALOG_DOCKERFILE)
	$(MAKE) $(CATALOG_FILE) BUNDLE_IMG=$(BUNDLE_IMG)
	cd catalog && $(OPM) validate dns-operator-catalog

# Build a catalog image by adding bundle images to an empty catalog using the operator package manager tool, 'opm'.
# Ref https://olm.operatorframework.io/docs/tasks/creating-a-catalog/#catalog-creation-with-raw-file-based-catalogs
.PHONY: catalog-build
catalog-build: ## Build a catalog image.
	# Build the Catalog
	$(CONTAINER_TOOL) build catalog -f catalog/dns-operator-catalog.Dockerfile -t $(CATALOG_IMG)

# Push the catalog image.
.PHONY: catalog-push
catalog-push: ## Push a catalog image.
	$(MAKE) docker-push IMG=$(CATALOG_IMG)

.PHONY: deploy-catalog
deploy-catalog: kustomize yq ## Deploy operator to the K8s cluster specified in ~/.kube/config using OLM catalog image.
	V="$(CATALOG_IMG)" $(YQ) eval '.spec.image = strenv(V)' -i config/deploy/olm/catalogsource.yaml
	$(KUSTOMIZE) build config/deploy/olm | $(KUBECTL) apply -f -

.PHONY: undeploy-catalog
undeploy-catalog: kustomize ## Undeploy controller from the K8s cluster specified in ~/.kube/config using OLM catalog image.
	$(KUSTOMIZE) build config/deploy/olm | $(KUBECTL) delete -f -
