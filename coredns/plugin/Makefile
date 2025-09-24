COREDNS_IMAGE_TAG_BASE ?= quay.io/kuadrant/coredns-kuadrant
COREDNS_DEFAULT_IMG ?= $(COREDNS_IMAGE_TAG_BASE):latest
COREDNS_IMG ?= $(COREDNS_DEFAULT_IMG)

CONTAINER_TOOL ?= docker

VERSION ?= 0.1.0
GIT_SHA=$(shell git rev-parse HEAD || echo "unknown")

.PHONY: all
all: build

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

.PHONY: clean
clean: ## Clean local files.
	go clean
	rm -f coredns

.PHONY: build
build: ## Build coredns binary.
	GOOS=linux CGO_ENABLED=0 go build -ldflags "-X main.pluginVersion=${VERSION} -X main.gitSHA=${GIT_SHA}" cmd/coredns.go

.PHONY: test-unit
test-unit: ## Run unit tests.
	go test ./... -v

.PHONY: run
run: DNS_PORT=1053
run: ## Run coredns from your host.
	go run --race  -ldflags "-X main.pluginVersion=${VERSION} -X main.gitSHA=${GIT_SHA}" ./cmd/coredns.go -dns.port ${DNS_PORT}

.PHONY: docker-build
docker-build: ## Build docker image.
	$(CONTAINER_TOOL) build . -t ${COREDNS_IMG} --build-arg VERSION=v$(VERSION) --build-arg GIT_SHA=$(GIT_SHA)

.PHONY: docker-push
docker-push: ## Push docker image.
	$(CONTAINER_TOOL) push ${COREDNS_IMG}

.PHONY: docker-run
docker-run: DNS_PORT=1053
docker-run: docker-build ## Build docker image and run coredns in a container.
	$(CONTAINER_TOOL) run --rm -it -p ${DNS_PORT}:53/udp ${COREDNS_IMG}

.PHONY: generate-demo-geo-db
generate-demo-geo-db: ## Generate demo geo db embedded in coredns image.
	cd geoip && go run db-generator.go
