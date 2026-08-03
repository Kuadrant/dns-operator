# Release

This repository uses a two-phase release workflow as defined in the [Two-Phase Release Workflow RFC](https://github.com/Kuadrant/architecture/pull/178).

## Overview

Every release is split into two GitHub Actions workflows with a human review gate between them:

1. **Pre-release** (`pre-release.yaml`) — Makes version-related code changes and opens a pull request to the release branch
2. **Release** (`release.yaml`) — Runs smoke tests, tags, builds all artifacts, and creates the GitHub Release as its final step

A `release.yaml` file at the repository root is the machine-readable source of truth for the component version.
A **version gate** CI check validates this file on pull requests to release branches.

## New Minor Release (e.g. 0.18.0)

1. Trigger the [Pre-release workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/pre-release.yaml) with:
   - **version**: `0.18.0`
   - **source-branch**: `main` (default)

2. The workflow will:
   - Create the release branch `release-0.18` from main (if it doesn't exist)
   - Update `release.yaml` with version `0.18.0`
   - Run `make prepare-release VERSION=0.18.0 CHANNELS=stable`
   - Open a PR from `pre-release-v0.18.0` to `release-0.18`

3. Review the PR:
   - CI runs tests and the version gate check
   - Verify version numbers, bundle manifests, and helm chart
   - Approve and merge

4. Trigger the [Release workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/release.yaml) with:
   - **release-branch**: `release-0.18`

5. The workflow will (in order):
   - Read the version from `release.yaml`
   - Run smoke tests (verify-manifests, verify-bundle, verify-helm-build, unit tests)
   - Create and push tag `v0.18.0`
   - Build and push container images (operator, bundle, catalog, CoreDNS)
   - Package and sign the Helm chart
   - Build CLI binaries for all platforms
   - Create the GitHub Release with all artifacts attached

6. Verify the release:
   - Check that all images are available on quay.io
   - Verify OLM deployment (see [Verify OLM Deployment](#verify-olm-deployment))

## New Patch Release (e.g. 0.18.1)

1. Trigger the [Pre-release workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/pre-release.yaml) with:
   - **version**: `0.18.1`
   - **source-branch**: `release-0.18` (or a branch with cherry-picked fixes)

2. Review and merge the resulting PR to `release-0.18`

3. Trigger the [Release workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/release.yaml) with:
   - **release-branch**: `release-0.18`

4. Verify the release as above

## Release Artifacts

The release workflow produces the following artifacts:

| Artifact | Registry/Location |
|---|---|
| Operator image | `quay.io/kuadrant/dns-operator:vX.Y.Z` |
| Bundle image | `quay.io/kuadrant/dns-operator-bundle:vX.Y.Z` |
| Catalog image | `quay.io/kuadrant/dns-operator-catalog:vX.Y.Z` |
| CoreDNS image | `quay.io/kuadrant/coredns-kuadrant:vX.Y.Z` |
| Helm chart | GitHub Release asset (GPG signed) |
| CLI binaries | GitHub Release assets (linux/darwin, amd64/arm64/s390x/ppc64le) |

All container images are built for platforms: `linux/amd64`, `linux/arm64`, `linux/s390x`, `linux/ppc64le`.

## release.yaml

The `release.yaml` file at the repository root declares the component version:

```yaml
dns-operator:
  version: "0.0.0"    # 0.0.0 on main, concrete version on release branches
```

On the `main` branch, the version is always `0.0.0` (sentinel for "under active development").
On release branches, the pre-release workflow updates it to the target version.

## Generated Files

During the pre-release process (`make prepare-release`), the following files are generated or modified:

### Modified files
- `release.yaml`
- `bundle.Dockerfile`
- `bundle/manifests/dns-operator.clusterserviceversion.yaml`
- `bundle/metadata/annotations.yaml`
- `charts/dns-operator/Chart.yaml`
- `charts/dns-operator/templates/manifests.yaml`
- `config/deploy/olm/catalogsource.yaml`
- `config/deploy/olm/subscription.yaml`
- `config/manager/kustomization.yaml`
- `config/manifests/bases/dns-operator.clusterserviceversion.yaml`

### Generated files
- `make/release.mk`

The `make/release.mk` contains release-specific variable overrides:
```sh
#Release default values
IMG=quay.io/kuadrant/dns-operator:v0.18.0
BUNDLE_IMG=quay.io/kuadrant/dns-operator-bundle:v0.18.0
CATALOG_IMG=quay.io/kuadrant/dns-operator-catalog:v0.18.0
COREDNS_IMG=quay.io/kuadrant/coredns-kuadrant:v0.18.0
CHANNELS=stable
BUNDLE_CHANNELS=--channels=stable
VERSION=0.18.0
```

Note: The `VERSION` number is **not** prefixed with `v` (e.g. `0.18.0`), but image tags **are** prefixed with `v` (e.g. `v0.18.0`).

## Required GitHub Secrets

The release workflows require the following secrets to be configured in the repository settings:

| Secret | Used by | Description |
|---|---|---|
| `GITHUB_TOKEN` | Pre-release, Release, Version Gate | Built-in GitHub token. Used for creating branches, PRs, tags, and GitHub Releases. No manual configuration needed. |
| `IMG_REGISTRY_USERNAME` | Release | Quay.io username for pushing container images (operator, bundle, catalog, CoreDNS). |
| `IMG_REGISTRY_TOKEN` | Release | Quay.io API token or password for pushing container images. |
| `HELM_CHARTS_SIGNING_KEY` | Release | Base64-encoded GPG private key used to sign the Helm chart package. |

## Verify OLM Deployment

1. Deploy the OLM catalog image:
```sh
make local-setup install-olm deploy-catalog
```

2. Wait for deployment:
```sh
kubectl -n dns-operator-system wait --timeout=60s --for=condition=Available deployments --all
```

3. Check the logs:
```sh
kubectl -n dns-operator-system logs -f deployment/dns-operator-controller-manager
```

4. Check the version:
```sh
kubectl -n dns-operator-system get deployment dns-operator-controller-manager --show-labels
```

## Community Operator Index Catalogs

- [Operatorhub Community Operators](https://github.com/k8s-operatorhub/community-operators/tree/main/operators/dns-operator)
- [Openshift Community Operators](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/dns-operator)

>Note: These are no longer updated as part of a release, links are here for historical reference only.
