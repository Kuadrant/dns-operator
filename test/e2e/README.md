# E2E Test Suite

The e2e test suite is used to test common scenarios in each supported dns provider. The suite contains tests for single instance tests (single_record) where only a single running controller are expected as well as multi instance tests (multi_record) that are intended to test scenarios where multiple instances of the dns operator are running, each reconciling DNSRecord resources contributing to the same shared dns zone. 
The suite allows runtime configuration to alter the number of instances that are under test allowing stress testing scenarios to be executed using more extreme numbers of instances and records.

## Test Labels

Tests are organized with [labels](https://onsi.github.io/ginkgo/#spec-labels) allowing subsets of specs to be easily run using ginkgo command line [options](https://onsi.github.io/ginkgo/#combining-filters).

| label           | description                                                                               |
|-----------------|-------------------------------------------------------------------------------------------|
| multi_record    | Test cases covering multiple DNSRecords updating a set of records in a zone (Distributed) |
| single_record   | Test cases covering a single DNSRecord updating a set of records in a zone                | 
| simple          | Test cases for DNSRecords using the simple endpoint structure                             | 
| loadbalanced    | Test cases for DNSRecords using the loadbalanced endpoint structure                       | 
| provider_errors | Tests cases that put DNSRecords into known error states                                   | 
| health_checks   | Tests cases covering DNSRecords with health checks                                        | 
| happy           | Happy path test cases, minimum set of tests that check basic functionality                | 

## Local Setup

### Cluster scoped on single cluster

Deploy the operator on a single kind cluster with one operator instance watching all namespaces:
```shell
make local-setup DEPLOY=true
```

### Namespace scoped on single cluster

Deploy the operator on a single kind cluster with two operator instances in two namespaces watching their own namespace only:
```shell
make local-setup DEPLOY=true DEPLOYMENT_SCOPE=namespace DEPLOYMENT_COUNT=2
```

The above will create two dns operator deployments on the kind cluster, each configured to watch its own namespace, with the development provider secrets (Assuming you have configured them locally) created in each deployment namespace.

DNS Operator Deployments:
```shell
kubectl get deployments -l app.kubernetes.io/part-of=dns-operator -A
NAMESPACE        NAME                                READY   UP-TO-DATE   AVAILABLE   AGE
dns-operator-1   dns-operator-controller-manager-1   1/1     1            1           21s
dns-operator-2   dns-operator-controller-manager-2   1/1     1            1           21s
```

DNS Provider Secrets:
```shell
kubectl get secrets -l app.kubernetes.io/part-of=dns-operator -A
NAMESPACE        NAME                                TYPE                            DATA   AGE
dns-operator-1   dns-provider-credentials-aws        kuadrant.io/aws                 5      21s
dns-operator-1   dns-provider-credentials-azure      kuadrant.io/azure               3      21s
dns-operator-1   dns-provider-credentials-gcp        kuadrant.io/gcp                 4      21s
dns-operator-1   dns-provider-credentials-inmemory   kuadrant.io/inmemory            0      21s
dns-operator-2   dns-provider-credentials-aws        kuadrant.io/aws                 5      21s
dns-operator-2   dns-provider-credentials-azure      kuadrant.io/azure               3      21s
dns-operator-2   dns-provider-credentials-gcp        kuadrant.io/gcp                 4      21s
dns-operator-2   dns-provider-credentials-inmemory   kuadrant.io/inmemory            0      21s
dnstest          dns-provider-credentials-aws        kuadrant.io/aws                 5      68s
dnstest          dns-provider-credentials-azure      kuadrant.io/azure               3      68s
dnstest          dns-provider-credentials-gcp        kuadrant.io/gcp                 4      68s
dnstest          dns-provider-credentials-inmemory   kuadrant.io/inmemory            0      68s
```

### Cluster scoped on multiple clusters

Deploy the operator on two kind clusters each with one operator instance watching all namespaces:
```shell
make local-setup DEPLOY=true CLUSTER_COUNT=2
```

### Namespace scoped on multiple clusters

Deploy the operator on two local kind clusters with two operator instances in two namespaces watching their own namespace only:
```shell
make local-setup DEPLOY=true DEPLOYMENT_SCOPE=namespace DEPLOYMENT_COUNT=2 CLUSTER_COUNT=2
```

## Run the test suite

### Cluster scoped on single cluster
```shell
make test-e2e TEST_DNS_ZONE_DOMAIN_NAME=mn.hcpapps.net TEST_DNS_PROVIDER_SECRET_NAME=dns-provider-credentials-aws TEST_DNS_NAMESPACES=dnstest
```

### Namespace scoped on single cluster
```shell
make test-e2e TEST_DNS_ZONE_DOMAIN_NAME=mn.hcpapps.net TEST_DNS_PROVIDER_SECRET_NAME=dns-provider-credentials-aws TEST_DNS_NAMESPACES=dns-operator DEPLOYMENT_COUNT=2
```

### Cluster scoped on multiple clusters
```shell
make test-e2e TEST_DNS_ZONE_DOMAIN_NAME=mn.hcpapps.net TEST_DNS_PROVIDER_SECRET_NAME=dns-provider-credentials-aws TEST_DNS_NAMESPACES=dnstest TEST_DNS_CLUSTER_CONTEXTS=kind-kuadrant-dns-local CLUSTER_COUNT=2
```

### Namespace scoped on multiple clusters
```shell
make test-e2e TEST_DNS_ZONE_DOMAIN_NAME=mn.hcpapps.net TEST_DNS_PROVIDER_SECRET_NAME=dns-provider-credentials-aws TEST_DNS_NAMESPACES=dns-operator DEPLOYMENT_COUNT=2 TEST_DNS_CLUSTER_CONTEXTS=kind-kuadrant-dns-local CLUSTER_COUNT=2
```

## Tailing operator pod logs

It's not possible to tail logs across namespaces with `kubectl logs -f`, but third party plugins such as [stern](https://github.com/stern/stern) can be used instead.

```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces
```

If development mode is disabled and json logs are being used you can pipe the logs into jq:
```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces -i 'logger' -o raw | jq .
```

Use jq to select the filter down to the logs you care about:
```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces -i 'logger' -o raw | jq 'select(.logger=="dnsrecord_controller")'
```
