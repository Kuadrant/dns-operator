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
make local-setup-gcp-mz-clean local-setup-gcp-mz-generate GCP_ZONE_NAME=<My GCP ZONE Name> GCP_ZONE_DNS_NAME=<My Zone DNS Name> GCP_GOOGLE_CREDENTIALS='<My GCP Credentials.json>' GCP_PROJECT_ID=<My GCP PROJECT ID>
```
More details about the GCP provider can be found [here](./docs/provider.md#google-cloud-dns-provider)

##### AZURE Provider

```bash
make local-setup-azure-mz-clean local-setup-azure-mz-generate KUADRANT_AZURE_CREDENTIALS='<My Azure Credentials.json>' KUADRANT_AZURE_DNS_ZONE_ID=<My Azure Zone ID> KUADRANT_AZURE_ZONE_ROOT_DOMAIN='<My Azure Domain Name>'
```

Info on generating service principal credentials [here](https://github.com/kubernetes-sigs/external-dns/blob/master/docs/tutorials/azure.md)

Getting the zone ID can be achieved using the below command:
```bash
az network dns zone show --name <my domain name> --resource-group <my resource group> --query "{id:id,domain:name}"
```

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

### E2E Test Suite

The e2e test suite can be executed against any cluster running the DNS Operator with configuration added for any supported provider.

```
make test-e2e TEST_DNS_MANAGED_ZONE_NAME=<My managed zone name> TEST_DNS_NAMESPACES=<My test namespace(s)>
```

| Environment Variable       | Description                                                                                          |
|----------------------------|------------------------------------------------------------------------------------------------------|
| TEST_DNS_MANAGED_ZONE_NAME | Name of the managed zone to use. If using local-setup Managed zones, one of [dev-mz-aws; dev-mz-gcp] | 
| TEST_DNS_NAMESPACES        | The namespace(s) where the managed zone with the name (TEST_DNS_MANAGED_ZONE_NAME) can be found      | 

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Logging
Logs are following the general guidelines: 
- `logger.Info()` describe a high-level state of the resource such as creation, deletion and which reconciliation path was taken.  
- `logger.Error()` describe only those errors that are not returned in the result of the reconciliation. If error is occurred there should be only one error message. 
- `logger.V(1).Info()` debug level logs to give information about every change or event caused by the resource as well as every update of the resource.

The `--zap-devel` argument will enable debug level logs for the output. Otherwise, all `V()` logs are ignored.

### Common metadata 
Not exhaustive list of metadata for DNSRecord controller:
- `level` - logging level. Values are: `info`,`debug` or `error`
- `ts` - timestamp
- `logger` - logger name
- `msg`
- `controller` and `controllerKind` - controller name, and it's kind respectively to output the log
- `DNSRecord` - name and namespace of the DNS Record CR that is being reconciled
- `reconcileID`
- `ownerID` - ID the of owner of the DNS Record 
- `txtPrefix`/`txtSuffix` - prefix and suffix of the TXT record in provider. 
- `zoneEndpoints` - endpoints that exist in the provider
- `specEdnoinds` - endpoints defined in the spec
- `statusEndpoints` - endpoints that were processed previously

> Note that not all the metadata values are present at each of the logs statements. 

### Examples
To query logs locally you can use `jq`. For example:
Retrieve logs by 
```shell
kubectl get deployments -l app.kubernetes.io/part-of=dns-operator -A

NAMESPACE             NAME                              READY 
dns-operator-system   dns-operator-controller-manager   1/1   
```
And query them. For example:
```shell
kubectl logs -l control-plane=dns-operator-controller-manager -n dns-operator-system --tail -1 | sed '/^{/!d' | jq 'select(.controller=="dnsrecord" and .level=="info")'
```
or 
```shell
kubectl logs -l control-plane=dns-operator-controller-manager -n dns-operator-system --tail -1 | sed '/^{/!d' | jq 'select(.controller=="dnsrecord" and .DNSRecord.name=="test" and .reconcileID=="2be16b6d-b90f-430e-9996-8b5ec4855d53")' | jq '.level, .msg, .zoneEndpoints, .specEndpoints, .statusEndpoints '

```
You could use selector in the `jq` with `and`/`not`/`or` to restrict.


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
