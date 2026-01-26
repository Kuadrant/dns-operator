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

**Note:** When using DNS Groups functionality with CoreDNS, you can customize where the active groups TXT record is resolved from:
```sh
make local-setup EXTERNAL_GROUPS_HOST=<your-external-host>
```
Default: `kuadrant-active-groups.hcpapps.net`

1. Run your controller (this will run in the foreground, so switch to a new terminal if you want to leave it running):

```sh
make run
```

To run with a specific group identifier:
```sh
make run GROUP=<your-group-name>
```

### Running controller on the cluster

1. Create local environment(creates kind cluster)
```sh
make local-setup DEPLOY=true
```

#### CoreDNS with DNS Groups

After running the local-setup, some configuration is required to enable the active-groups TXT record
when running locally with CoreDNS and using DNS Groups.

1. Modify the corefile
You will need to modify the Corefile in coredns, this is located in a configmap `kuadrant-coredns`
 by default in the `kuadrant-coredns` namespace. 
 
 Add the following config (change the parts in caps to your usecase):
```sh
    rewrite name regex kuadrant-active-groups\.(.*)ZONE_DOMAIN_NAME EXTERNAL_TXT_RECORD_HOST
    forward EXTERNAL_TXT_RECORD_HOST /etc/resolv.conf
```

Before the `ready` statement.

Then restart the coredns operator.
```sh
kubectl -n kuadrant-coredns rollout restart deployment kuadrant-coredns 
```

1. Verify controller deployment
```sh
kubectl logs -f deployments/dns-operator-controller-manager -n dns-operator-system
```

1. Create External record:
In your dns provider, create the record referred to earlier in your corefile. This record requires the 
following format (changing the values in caps as required):
```sh
version=1;groups=GROUP1&&GROUP2
```

1. Verify coredns groups resolution

Once the external record has been created, find the local IP of the coredns instance:
```sh
NS=$(kubectl get secrets -n dnstest dns-provider-credentials-coredns -o yaml | yq ".data.NAMESERVERS" | base64 -d | cut -f1 -d":")
```
Then dig the local TXT record from that IP:
```sh
dig @$NS kuadrant-active-groups.k.example.com TXT +short
"version=1;groups=GROUP1&&GROUP2"
```

### Running controller on existing cluster

You'll need a Kubernetes cluster to run against. You can use [KIND](https://sigs.k8s.io/kind) to get a local cluster for testing, or run against a remote cluster.
**Note:** Your controller will automatically use the current context in your kubeconfig file (i.e. whatever cluster `kubectl cluster-info` shows).

1. Apply Operator manifests
```sh
kustomize build config/default | kubectl apply -f -
```

1. Verify controller deployment
```sh
kubectl logs -f deployments/dns-operator-controller-manager -n dns-operator-system
```

## CoreDNS Provider

The DNS Operator includes a CoreDNS plugin that enables serving DNS zone data from Kubernetes DNSRecord resources. This is particularly useful for local development and testing.

### CoreDNS Plugin Configuration

For detailed CoreDNS configuration and integration documentation, see:
- [CoreDNS Integration Guide](./docs/coredns/README.md)

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

## Controller flags and environmental variables
The controller can be started with any of the following flags or environmental variables. Upon the start of the controller operator give precedence to envars (i.e. `PROVIDER` envar will override `--provider` flag). If neither is set the default value will be used. Envars are parsed into their types from a string where applicable. If parsing fails - envar is ignored

| Flag Name                 | Flag Type     | Envar Name                | Description                                                                                                           | Default                               |
|---------------------------|---------------|---------------------------|-----------------------------------------------------------------------------------------------------------------------|---------------------------------------|
| metrics-bind-address      | string        | METRICS_BIND_ADDRESS      | The address the metric endpoint binds to.                                                                             | ":8080"                               |
| health-probe-bind-address | string        | HEALTH_PROBE_BIND_ADDRESS | The address the probe endpoint binds to.                                                                              | ":8081"                               |
| leader-elect              | bool          | LEADER_ELECT              | Enable leader election for controller manager. Enabling this will ensure there is only one active controller manager. | "false"                               |
| min-requeue-time          | time.Duration | MIN_REQUEUE_TIME          | The minimal timeout between calls to the DNS Provider. Controls if we commit to the full reconcile loop               | "5s"                                  |
| max-requeue-time          | time.Duration | MAX_REQUEUE_TIME          | The maximum times it takes between reconciliations of DNS Record. Controls how ofter record is reconciled.            | "15m"                                 |
| valid-for                 | time.Duration | VALID_FOR                 | Duration when the record is considered to hold valid information. Controls if we commit to the full reconcile loop    | "14m"                                 |
| provider                  | string        | PROVIDER                  | DNS Provider(s) to enable.                                                                                            | "aws,google,azure,coredns,endpoints"  |
| enable-probes             | bool          | ENABLE_PROBES             | Enable DNSHealthProbes controller.                                                                                    | "true"                                |
| insecure-health-checks    | bool          | INSECURE_HEALTH_CHECKS    | DNS Health Probes will ignore insecure certificates when true                                                         | "true"                                |
| cluster-secret-namespace  | string        | CLUSTER_SECRET_NAMESPACE  | The namespace in which cluster secrets are located                                                                    | "dns-operator-system"                 |
| cluster-secret-label      | string        | CLUSTER_SECRET_LABEL      | The label that identifies a Secret resource as a cluster secret.                                                      | "kuadrant.io/multicluster-kubeconfig" |
| watch-namespaces          | string        | WATCH_NAMESPACES          | Comma separated list of default namespaces.                                                                           | \<empty string\>                      |
| delegation-role           | string        | DELEGATION_ROLE           | The delegation role for this controller. Must be one of 'primary'(default), or 'secondary'                            | "primary"                             |
| group                     | string        | GROUP                     | The DNS failover group identifier for this controller instance. Used for active-passive failover across clusters.     | \<empty string\>                      |

## Logging
Logs are following the general guidelines: 
- `logger.Info()` describe a high-level state of the resource such as creation, deletion and which reconciliation path was taken.  
- `logger.Error()` describe only those errors that are not returned in the result of the reconciliation. If error is occurred there should be only one error message. 
- `logger.V(1).Info()` debug level logs to give information about every change or event caused by the resource as well as every update of the resource.

There are two flags to control logging output 
- `--log-mode=[development|<any-other-value>]` will enable debug level logs for the output. 
    The debug mode is the most verbose.
- `--log-level` controls the level of displayed logs. Defaults to the most verbose in the `development` mode. 
    In any other modes it can take numerical values form `-1` (Debug level) to `4` (Nothing). 
    It is possible to specify other values, but hey will have no effect (e.g. `4` will do the same as `128`) 

You can find more [here](https://pkg.go.dev/github.com/go-logr/zapr#section-readme).


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
- `specEndpoints` - endpoints defined in the spec
- `statusEndpoints` - endpoints that were processed previously
- `currentGroup` - the group this DNS operator instance belongs to
- `activeGroups` - the currently active groups read from DNS

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
