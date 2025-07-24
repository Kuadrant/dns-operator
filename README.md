# DNS Operator
[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FKuadrant%2Fdns-operator.svg?type=shield)](https://app.fossa.com/projects/git%2Bgithub.com%2FKuadrant%2Fdns-operator?ref=badge_shield)


The DNS Operator is a kubernetes based controller responsible for reconciling DNS Record custom resources. It interfaces with cloud DNS providers such as AWS, Google and Azure to bring the DNS zone into the state declared in these CRDs.
One of the key use cases the DNS operator solves, is allowing complex DNS routing strategies such as Geo and Weighted to be expressed. This allows you to leverage DNS as the first layer of traffic management. These strategies increase in value as you works across multiple clusters. DNS operator can be deployed to multiple cluster and coordinate on a given zone allowing you to use a shared domain name to balance traffic based on your requirements.

## Getting Started

### Pre Setup

#### Add DNS provider configuration

**NOTE:** You can optionally skip this step but at least one DNS Provider Secret will need to be configured with valid credentials to use the DNS Operator.

##### AWS Provider (Route53)
```bash
make local-setup-aws-clean local-setup-aws-generate AWS_ACCESS_KEY_ID=<My AWS ACCESS KEY> AWS_SECRET_ACCESS_KEY=<My AWS Secret Access Key>
```
More details about the AWS provider can be found [here](./docs/provider.md#aws-route-53-provider)

##### GCP Provider

```bash
make local-setup-gcp-clean local-setup-gcp-generate GCP_GOOGLE_CREDENTIALS='<My GCP Credentials.json>' GCP_PROJECT_ID=<My GCP PROJECT ID>
```
More details about the GCP provider can be found [here](./docs/provider.md#google-cloud-dns-provider)

##### AZURE Provider

```bash
make local-setup-azure-clean local-setup-azure-generate KUADRANT_AZURE_CREDENTIALS='<My Azure Credentials.json>'
```

Info on generating service principal credentials [here](https://github.com/kubernetes-sigs/external-dns/blob/master/docs/tutorials/azure.md)

Get your resource group ID like so:
```
az group show --resource-group <resource group name> | jq ".id" -r
```

Also give traffic manager contributor role:
```
az role assignment create --role "Traffic Manager Contributor" --assignee $EXTERNALDNS_SP_APP_ID --scope <RESOURCE_GROUP_ID>
```

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
make test-e2e TEST_DNS_ZONE_DOMAIN_NAME=<My domain name> TEST_DNS_PROVIDER_SECRET_NAME=<My provider secret name> TEST_DNS_NAMESPACES=<My test namespace(s)>
```

| Environment Variable          | Description                                                                                                                                                                         |
|-------------------------------|-------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------|
| TEST_DNS_PROVIDER_SECRET_NAME | Name of the provider secret to use. If using local-setup provider secrets zones, one of [dns-provider-credentials-aws; dns-provider-credentials-gcp;dns-provider-credentials-azure] | 
| TEST_DNS_ZONE_DOMAIN_NAME     | The Domain name to use in the test. Must be a zone accessible with the (TEST_DNS_PROVIDER_SECRET_NAME) credentials with the same domain name                                        | 
| TEST_DNS_NAMESPACES           | The namespace(s) where the provider secret(s) can be found                                                                                                                          | 

### Modifying the API definitions
If you are editing the API definitions, generate the manifests such as CRs or CRDs using:

```sh
make manifests
```

**NOTE:** Run `make --help` for more information on all potential `make` targets

More information can be found via the [Kubebuilder Documentation](https://book.kubebuilder.io/introduction.html)

## Controller flags 
The controller can be started with any of the following flags. Upon the start of the controller operator will locate the `dns-operator` configMap in the `kuadrant-system` namespace and will use data from it to override any flags set. The keys in the configMap must match exactly the name of the flag; the values must be parsable from a string into the type of the flag. If there is an error parsing - the value from the config map will be ignored.

| Flag Name                 | Flag Type     | Description                                                                                                           | Default                             |
|---------------------------|---------------|-----------------------------------------------------------------------------------------------------------------------|-------------------------------------|
| metrics-bind-address      | string        | The address the metric endpoint binds to.                                                                             | ":8080"                             |
| health-probe-bind-address | string        | The address the probe endpoint binds to.                                                                              | ":8081"                             |
| leader-elect              | bool          | Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager. | "false"                             |
| min-requeue-time          | time.Duration | The minimal timeout between calls to the DNS Provider. Controls if we commit to the full reconcile loop               | "5s"                                |
| max-requeue-time          | time.Duration | The maximum times it takes between reconciliations of DNS Record. Controls how ofter record is reconciled.            | "15m"                               |
| valid-for                 | time.Duration | Duration when the record is considered to hold valid information. Controls if we commit to the full reconcile loop    | "14m"                               |
| provider                  | string        | DNS Provider(s) to enable.                                                                                            | "aws,google,inmemory,azure,coredns" |
| enable-probes             | bool          | Enable DNSHealthProbes controller.                                                                                    | "true"                              |
| insecure-health-checks    | bool          | Allow DNSHealthProbes to use insecure certificates                                                                    | "true"                              |

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


[![FOSSA Status](https://app.fossa.com/api/projects/git%2Bgithub.com%2FKuadrant%2Fdns-operator.svg?type=large)](https://app.fossa.com/projects/git%2Bgithub.com%2FKuadrant%2Fdns-operator?ref=badge_large)
