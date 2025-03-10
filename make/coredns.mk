COREDNS_PLUGIN_DIR=./coredns/plugin

##@ CoreDNS Plugin

# Wraps the CoreDNS plugin make targets in ${COREDNS_PLUGIN_DIR} to simplify running the tasks at the root of this repo.

.PHONY: coredns-clean
coredns-clean: ## Clean local files.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) clean

.PHONY: coredns-build
coredns-build: ## Build coredns binary.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) build

.PHONY: coredns-test-unit
coredns-test-unit: ## Run unit tests.
	cd ${COREDNS_PLUGIN_DIR}  && $(MAKE) test-unit

.PHONY: coredns-run
coredns-run: DNS_PORT=1053
coredns-run: ## Run coredns from your host.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) run

.PHONY: coredns-docker-build
coredns-docker-build: ## Build docker image.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) docker-build

.PHONY: coredns-docker-push
coredns-docker-push: ## Push docker image.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) docker-push

.PHONY: coredns-docker-run
coredns-docker-run: DNS_PORT=1053
coredns-docker-run: ## Build docker image and run coredns in a container.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) docker-run

.PHONY: coredns-generate-demo-geo-db
coredns-generate-demo-geo-db: ## Generate demo geo db embedded in coredns image.
	cd ${COREDNS_PLUGIN_DIR} && $(MAKE) generate-demo-geo-db
