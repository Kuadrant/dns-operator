# DNS Operator

The DNS Operator is a kubernetes based controller responsible for reconciling DNS Record and Managed Zone custom resources. It interfaces with cloud DNS providers such as AWS and Google to bring the DNS zone into the state declared in these CRDs.
One of the key use cases the DNS operator solves, is allowing complex DNS routing strategies such as Geo and Weighted to be expressed allowing you to leverage DNS as the first layer of traffic management. In order to make these strategies valuable, it also works across multiple clusters allowing you to use a shared domain name balance traffic based on your requirements.

## Getting Started

### Pre Setup

#### Add DNS provider configuration

**NOTE:** You can optionally skip this step but at least one ManagedZone will need to be configured and have valid credentials linked to use the DNS Operator.

##### AWS Provider (Route53)
```bash
make local-setup-aws-mz-clean local-setup-aws-mz-generate AWS_ZONE_ROOT_DOMAIN=<MY AWS Zone Root Domain> AWS_DNS_PUBLIC_ZONE_ID=<My AWS DNS Public Zone ID> AWS_ACCESS_KEY_ID=<My AWS ACCESS KEY> AWS_SECRET_ACCESS_KEY=<My AWS Secret Access Key>
```
More details about the AWS provider can be found [here](./docs/provider.md#aws-route-53-provider)

##### GCP Provider

```bash
make local-setup-gcp-mz-clean local-setup-gcp-mz-generate GCP_ZONE_NAME=<My GCP ZONE Name> GCP_ZONE_DNS_NAME=<My Zone DNS Name> GCP_GOOGLE='<My GCP Credentials.json>' GCP_PROJECT_ID=<My GCP PROJECT ID>
```
More details about the GCP provider can be found [here](./docs/provider.md#google-cloud-dns-provider)

### Running controller locally (default)

1. Create local environment(creates kind cluster)
```sh
make local-setup
```

1. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

### Running controller on the cluster

1. Create local environment(creates kind cluster)
```sh
make local-setup DEPLOY=true
```

1. Verify controller deployment
```sh
kubectl logs -f deployments/dns-operator-controller-manager -n dns-operator-system
```

### Running controller on existing cluster

You’ll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

1. Apply Operator manifests
```sh
kustomize build config/default | kubectl apply -f -
```

1. Verify controller deployment
```sh
kubectl logs -f deployments/dns-operator-controller-manager -n dns-operator-system
```

## Development

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## License

Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
