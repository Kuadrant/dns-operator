
##@ Verify

## Targets to verify actions that generate/modify code have been executed and output committed

.PHONY: verify-all
verify-all: verify-code verify-bundle verify-helm-build verify-imports verify-manifests verify-generate verify-go-mod verify-vunerabilities

.PHONY: verify-code
verify-code: vet ## Verify code formatting
	@diff -u <(echo -n) <(gofmt -s -d `find . -type f -name '*.go' -not -path "./vendor/*"`)

.PHONY: verify-vunerabilities
verify-vunerabilities: govulncheck
	$(GOVULNCHECK) ./...

.PHONY: verify-manifests
verify-manifests: manifests ## Verify manifests update.
	git diff --exit-code ./config
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./config)" ]

.PHONY: verify-bundle
verify-bundle: bundle ## Verify bundle update.
	git diff --exit-code ./bundle ./config
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./bundle ./config)" ]

.PHONY: verify-helm-build
verify-helm-build: helm-build ## Verify helm build update.
	git diff --exit-code ./charts ./config
	[ -z "$$(git ls-files --other --exclude-standard --directory --no-empty-directory ./charts ./config)" ]

.PHONY: verify-imports
verify-imports: ## Verify go imports are sorted and grouped correctly.
	hack/verify-imports.sh

.PHONY: verify-generate
verify-generate: generate ## Verify generate update.
	git diff --exit-code ./api ./internal/controller

.PHONY: verify-go-mod
verify-go-mod: ## Verify go.mod matches source code
	go mod tidy
	git diff --exit-code ./go.mod
