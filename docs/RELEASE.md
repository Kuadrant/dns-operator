# Release

## New Major.Minor version

1. Create a new minor release branch from the HEAD of main:
```sh
git checkout -b release-0.2
```
2. Run prepare release:
```sh
make prepare-release IMG_TAG=release-0.2 VERSION=0.2.0-dev CHANNELS=alpha
```
3. Verify local changes, commit and push:
```sh
git add .
git commit -m "prepare-release: release-0.2"
git push upstream release-0.2
```
4. Verify that the build [image workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/build-images.yaml) is triggered and completes for the new branch

5. Do any final testing and bug fixing against the release branch, see [Verify OLM Deployment](#verify-olm-deployment)

6. Run prepare release for final version
```sh
make prepare-release VERSION=0.2.0 CHANNELS=stable
```
7. Verify local changes, commit, push and tag:
```sh
git add .
git commit -m "prepare-release: v0.2.0"
git tag v0.2.0
git push upstream release-0.2
git push upstream v0.2.0
```
8. Verify that the build [release tag workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/build-images-for-tag-release.yaml) is triggered and completes for the new tag

9. Verify the new version can be installed from the catalog image, see [Verify OLM Deployment](#verify-olm-deployment)

## New Patch version

1. Checkout minor release branch:
```sh
git checkout release-0.2
```
2. Run prepare release:
```sh
make prepare-release VERSION=0.2.1 CHANNELS=stable
```
3. Verify local changes, commit and push:
```sh
git add .
git commit -m "prepare-release: v0.2.1"
git tag v0.2.1
git push upstream release-0.2
git push upstream v0.2.1
```
4. Verify that the build [release tag workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/build-images-for-tag-release.yaml) is triggered and completes for the new tag

5. Verify the new version can be installed from the catalog image, see [Verify OLM Deployment](#verify-olm-deployment)

## Generated Files

During the release process a number of files will be generated, or modified.

### Modified files
- bundle.Dockerfile
- bundle/manifests/dns-operator.clusterserviceversion.yaml
- bundle/metadata/annotations.yaml
- charts/dns-operator/Chart.yaml
- charts/dns-operator/templates/manifests.yaml
- config/deploy/olm/catalogsource.yaml
- config/deploy/olm/subscription.yaml
- config/manager/kustomization.yaml
- config/manifests/bases/dns-operator.clusterserviceversion.yaml

### Generated files
- make/release.mk (modified during patch releases)

The `make/release.mk` contains the variables for the modifications of the modified files listed above.
Below is a sample of this file.
```sh
#Release default values
IMG=quay.io/kuadrant/dns-operator:v0.16.0
BUNDLE_IMG=quay.io/kuadrant/dns-operator-bundle:v0.16.0
CATALOG_IMG=quay.io/kuadrant/dns-operator-catalog:v0.16.0
CHANNELS=stable
BUNDLE_CHANNELS=--channels=stable
VERSION=0.16.0
```
Points to note.
The `VERSION` number is **not** prefixed with a `v`, 0.16.0.
Image tags for released version **are** prefixed with a `v`, v0.16.0.
Without the `v` prefix the [release tag workflow](https://github.com/Kuadrant/dns-operator/actions/workflows/build-images-for-tag-release.yaml) will fail.


## Create GitHub Release

Once the images have been published to quay.io, a GitHub Release is also required.
There are a number of workflows that depend on the release being created.
- [Release Helm Chart](https://github.com/Kuadrant/dns-operator/actions/workflows/release-helm-chart.yaml)
- [Upload CLI binary](https://github.com/Kuadrant/dns-operator/actions/workflows/upload-cli.yml)

## Verify OLM Deployment

1. Deploy the OLM catalog image:
```sh
make local-setup install-olm deploy-catalog
```

2. Wait for deployment:
```sh
kubectl -n dns-operator-system wait --timeout=60s --for=condition=Available deployments --all
deployment.apps/dns-operator-controller-manager condition met
```

3. Check the logs:
```sh
kubectl -n dns-operator-system logs -f deployment/dns-operator-controller-manager
```

4. Check the version:
```sh
$ kubectl -n dns-operator-system get deployment dns-operator-controller-manager --show-labels
NAME                              READY   UP-TO-DATE   AVAILABLE   AGE     LABELS
dns-operator-controller-manager   1/1     1            1           5m42s   app.kubernetes.io/component=manager,app.kubernetes.io/created-by=dns-operator,
app.kubernetes.io/instance=controller-manager,app.kubernetes.io/managed-by=kustomize,app.kubernetes.io/name=deployment,app.kubernetes.io/part-of=dns-operator,
control-plane=dns-operator-controller-manager,olm.deployment-spec-hash=1jPe8AuMpSKHh51nnDs4j25ZgoUrKhF45EP0Wa,olm.managed=true,olm.owner.kind=ClusterServiceVersion,
olm.owner.namespace=dns-operator-system,olm.owner=dns-operator.v0.2.0-dev,operators.coreos.com/dns-operator.dns-operator-system=
```

## Community Operator Index Catalogs

- [Operatorhub Community Operators](https://github.com/k8s-operatorhub/community-operators/tree/main/operators/dns-operator)
- [Openshift Community Operators](https://github.com/redhat-openshift-ecosystem/community-operators-prod/tree/main/operators/dns-operator)

>Note: These are no longer updated as part of a release, links are here for historical reference only.
