
COREDNS_IMAGE_TAG_BASE ?= quay.io/kuadrant/coredns-kuadrant
COREDNS_DEFAULT_IMG ?= $(COREDNS_IMAGE_TAG_BASE):latest
COREDNS_IMG ?= $(COREDNS_DEFAULT_IMG)

COREDNS_PLUGIN_DIR=./coredns/plugin

##@ CoreDNS Plugin

.PHONY: coredns-clean
coredns-clean: ## Clean local files.
	cd ${COREDNS_PLUGIN_DIR} && go clean
	cd ${COREDNS_PLUGIN_DIR} && rm -f coredns

.PHONY: coredns-build
coredns-build: ## Build coredns binary.
	cd ${COREDNS_PLUGIN_DIR} && GOOS=linux CGO_ENABLED=0 go build cmd/coredns.go

.PHONY: coredns-run
coredns-run: DNS_PORT=1053
coredns-run: ## Run coredns from your host.
	cd ${COREDNS_PLUGIN_DIR} && go run --race ./cmd/coredns.go -dns.port ${DNS_PORT}

.PHONY: coredns-docker-build
coredns-docker-build: coredns-build ## Build docker image.
	cd ${COREDNS_PLUGIN_DIR} && $(CONTAINER_TOOL) build . -t ${COREDNS_IMG}

.PHONY: coredns-docker-push
coredns-docker-push: ## Push docker image.
	$(CONTAINER_TOOL) push ${COREDNS_IMG}

.PHONY: coredns-docker-run
coredns-docker-run: DNS_PORT=1053
coredns-docker-run: coredns-docker-build ## Build docker image and run coredns in a container.
	cd ${COREDNS_PLUGIN_DIR} && $(CONTAINER_TOOL) run --rm -it -p ${DNS_PORT}:53/udp ${COREDNS_IMG}
