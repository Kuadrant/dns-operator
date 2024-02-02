
##@ GitHub Actions

## Targets to help test GitHub Actions locally using act https://github.com/nektos/act

.PHONY: act-test-unit-tests
act-test-unit-tests: act ## Run unit tests job.
	$(ACT) -j unit_test_suite

.PHONY: act-test-integration-tests
act-test-integration-tests: act ## Run integration tests job.
	$(ACT) -j integration_test_suite --privileged

.PHONY: act-test-verify-manifests
act-test-verify-manifests: act ## Run verify manifests job.
	$(ACT) -j verify-manifests

.PHONY: act-test-verify-bundle
act-test-verify-bundle: act ## Run verify bundle job.
	$(ACT) -j verify-bundle

.PHONY: act-test-verify-code
act-test-verify-code: act ## Run verify code job.
	$(ACT) -j verify-code

.PHONY: act-test-lint
act-test-lint: act ## Run lint job.
	$(ACT) -j lint

.PHONY: act-test-verify-imports
act-test-verify-imports: act ## Run verify-imports job.
	$(ACT) -j verify-imports
