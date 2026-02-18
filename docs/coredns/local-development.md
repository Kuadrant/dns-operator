# CoreDNS Local Development Guide

This guide provides step-by-step instructions for setting up and testing CoreDNS integration locally using Kind clusters. This is intended for local development and testing purposes only.

**For conceptual overview and production deployment**, see the [CoreDNS Integration Guide](coredns-integration.md).

**For quick-start examples and plugin syntax**, see the [CoreDNS Plugin README](../../coredns/plugin/README.md).

**For parent zone delegation setup (using BIND9)**, see the [Zone Delegation Guide](zone-delegation.md).

**Prerequisites:**
- `kubectl` - Kubernetes command-line tool
- `kind` - Tool for running local Kubernetes clusters
- `make` - Build automation tool
- `yq` - YAML processor for querying Kubernetes resources
- `dig` - DNS lookup utility for testing DNS resolution

## Table of Contents

1. [Single Cluster Setup](#setup-single-cluster-local-environment-kind)
2. [Multi-Cluster Setup](#setup-multi-cluster-local-environment-kind)
3. [GeoIP Testing](#geo)
4. [Troubleshooting](#troubleshooting)
5. [Cleanup](#cleanup)

## Setup single cluster local environment (kind)

Create a kind cluster with DNS Operator deployed:
```shell
# Run from dns-operator repository root
# DEPLOY=true installs DNS Operator and CoreDNS into the cluster
make local-setup DEPLOY=true
```

Configure observability stack (optional):
```shell
# Run from dns-operator repository root
make install-observability
```

Forward port for Grafana (optional):
```shell
kubectl -n monitoring port-forward service/grafana 3000:3000
```

Access dashboards at http://127.0.0.1:3000

> **Note:** Default credentials are `admin`/`admin`

### Understanding the Setup

Local setup deploys a single instance of CoreDNS with the kuadrant plugin enabled, configured to watch all namespaces for DNSRecord resources and zones configured for demo/test purposes.

View the Corefile ConfigMap data:
```shell
kubectl get configmap/kuadrant-coredns -n kuadrant-coredns -o yaml | yq .data
```

View CoreDNS logs:
```shell
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-coredns
```

### Optional Configuration

#### Enable Monitoring

Monitoring is not enabled by default. If you configured the observability stack above, update the CoreDNS instance to enable monitoring:
```shell
# Run from dns-operator repository root
bin/kustomize build --enable-helm config/coredns/ | kubectl apply -f -
```

#### Redeploy CoreDNS

To apply changes to the Corefile or deployment configuration:
```shell
# Run from dns-operator repository root
# Use config/coredns/ if monitoring is enabled, config/coredns-unmonitored/ if not
bin/kustomize build --enable-helm config/coredns/ | kubectl apply -f -
```

### Verify

Create test namespace and DNSRecord:
```shell
# Create dnstest namespace
kubectl create ns dnstest

# Run from dns-operator repository root
kubectl apply -n dnstest -f config/local-setup/dnsrecords/basic/coredns/simple/dnsrecord-simple-coredns.yaml
```

Verify zone (k.example.com) has updated records in the CoreDNS instance:
```shell
NS1=`kubectl get service/kuadrant-coredns -n kuadrant-coredns -o yaml | yq '.status.loadBalancer.ingress[0].ip'`
echo $NS1
dig @${NS1} -t AXFR k.example.com
```
Expected output (IP addresses, hashes, and timestamps will differ in your environment):
```
; <<>> DiG 9.18.28 <<>> @172.18.0.17 -t AXFR k.example.com
; (1 server found)
;; global options: +cmd
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
kuadrant-2kl5wt14-a-simple.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=2vl2ac4b,external-dns/version=1\""
simple.k.example.com.   60      IN      A       172.18.200.1
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
;; Query time: 0 msec
;; SERVER: 172.18.0.17#53(172.18.0.17) (TCP)
;; WHEN: Mon Sep 29 16:02:14 IST 2025
;; XFR size: 5 records (messages 1, bytes 441)
```

Verify DNS Server responds:
```shell
NS1=`kubectl get service/kuadrant-coredns -n kuadrant-coredns -o yaml | yq '.status.loadBalancer.ingress[0].ip'`
echo $NS1
dig @${NS1} simple.k.example.com +short
```
Expected output (IP address might differ in your environment):
```
172.18.200.1
```

## Setup multi cluster local environment (kind)

Create three kind clusters (2 primary and 1 secondary):
```shell
# Run from dns-operator repository root
# PRIMARY_CLUSTER_COUNT=2 creates 2 primary clusters (with CoreDNS deployed)
# CLUSTER_COUNT=3 creates 3 clusters total (CLUSTER_COUNT - PRIMARY_CLUSTER_COUNT = 1 secondary)
# Primary clusters can reconcile delegated DNSRecords, secondary clusters cannot
make multicluster-local-setup PRIMARY_CLUSTER_COUNT=2 CLUSTER_COUNT=3
```

### Verify Primary Cluster Setup

Check that CoreDNS, provider secrets, and cluster interconnection secrets are properly configured. 

CoreDNS is deployed and running:
```shell
kubectl get deployments,service -A -l app.kubernetes.io/name=coredns --context kind-kuadrant-dns-local-1
kubectl get deployments,service -A -l app.kubernetes.io/name=coredns --context kind-kuadrant-dns-local-2
```
Expected output:
```
NAMESPACE          NAME                               READY   UP-TO-DATE   AVAILABLE   AGE
kuadrant-coredns   deployment.apps/kuadrant-coredns   1/1     1            1           10m

NAMESPACE          NAME                       TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                     AGE
kuadrant-coredns   service/kuadrant-coredns   LoadBalancer   10.96.253.138   172.18.0.17   53:30494/UDP,53:30494/TCP   10m
```

Test zone transfer (AXFR) capability for k.example.com zone in both CoreDNS instances:
```shell
NS1=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-1 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`
NS2=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-2 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`

dig @${NS1} -t AXFR k.example.com
dig @${NS2} -t AXFR k.example.com
```
Expected output (empty zone before adding DNSRecords):
```
; <<>> DiG 9.18.28 <<>> @172.18.0.33 -t AXFR k.example.com
; (1 server found)
;; global options: +cmd
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
;; Query time: 1 msec
;; SERVER: 172.18.0.33#53(172.18.0.33) (TCP)
;; WHEN: Mon Sep 29 12:51:25 IST 2025
;; XFR size: 3 records (messages 1, bytes 278)
```

CoreDNS provider secret exists in the "dnstest" namespace with test zone (k.example.com) configured (namespace is created automatically on primary clusters):
```shell
kubectl get secret/dns-provider-credentials-coredns -n dnstest -o jsonpath='{.data.ZONES}' --context kind-kuadrant-dns-local-1 | base64 --decode
kubectl get secret/dns-provider-credentials-coredns -n dnstest -o jsonpath='{.data.ZONES}' --context kind-kuadrant-dns-local-2 | base64 --decode
```
Expected output:
```
k.example.com
```

Cluster interconnection secrets exist on kind-kuadrant-dns-local-1(primary 1) for kind-kuadrant-dns-local-2(primary 2) and kind-kuadrant-dns-local-3(secondary):
```shell
kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-kuadrant-dns-local-1
```
Expected output:
```
NAMESPACE             NAME                        TYPE     DATA   AGE   LABELS
dns-operator-system   kind-kuadrant-dns-local-2   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
dns-operator-system   kind-kuadrant-dns-local-3   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
```

Cluster interconnection secrets exist on kind-kuadrant-dns-local-2(primary 2) for kind-kuadrant-dns-local-1(primary 1) and kind-kuadrant-dns-local-3(secondary):
```shell
kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-kuadrant-dns-local-2
```
Expected output:
```
NAMESPACE             NAME                        TYPE     DATA   AGE   LABELS
dns-operator-system   kind-kuadrant-dns-local-1   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
dns-operator-system   kind-kuadrant-dns-local-3   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
```

### Verify Multi-Cluster DNS Records

Create "dnstest" namespace on kind-kuadrant-dns-local-3 (secondary cluster - not created automatically):
```shell
kubectl create ns dnstest --context kind-kuadrant-dns-local-3
```

Set CoreDNS provider as the default in the "dnstest" namespace on both primary clusters:
```shell
# The kuadrant.io/default-provider=true label allows DNSRecords and DNSPolicy
# to use this provider without explicitly specifying providerRef
kubectl label secret/dns-provider-credentials-coredns -n dnstest kuadrant.io/default-provider=true --context kind-kuadrant-dns-local-1
kubectl label secret/dns-provider-credentials-coredns -n dnstest kuadrant.io/default-provider=true --context kind-kuadrant-dns-local-2
```

Apply example DNSRecords with delegation enabled:
```shell
# Run from dns-operator repository root
kubectl apply -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster1.yaml --context kind-kuadrant-dns-local-1
kubectl apply -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster2.yaml --context kind-kuadrant-dns-local-2
kubectl apply -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster3.yaml --context kind-kuadrant-dns-local-3
```

Verify zone (k.example.com) has updated records in both primary cluster CoreDNS instances (after delegation reconciliation):
```shell
NS1=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-1 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`
NS2=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-2 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`

echo $NS1
echo $NS2

dig @${NS1} -t AXFR k.example.com
dig @${NS2} -t AXFR k.example.com
```
Expected output (IP addresses, hashes, and timestamps will differ in your environment):
```
; <<>> DiG 9.18.28 <<>> @172.18.0.33 -t AXFR k.example.com
; (1 server found)
;; global options: +cmd
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
kuadrant-1a20rnj9-cname-loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=2fgmvv67,external-dns/version=1\""
kuadrant-2o2qjax9-cname-loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=26yn4kgw,external-dns/version=1\""
kuadrant-31aztxux-cname-loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34l61c8o,external-dns/version=1\""
loadbalanced.k.example.com. 300 IN      CNAME   klb.loadbalanced.k.example.com.
klb.loadbalanced.k.example.com. 300 IN  CNAME   ie.klb.loadbalanced.k.example.com.
klb.loadbalanced.k.example.com. 300 IN  CNAME   ie.klb.loadbalanced.k.example.com.
klb.loadbalanced.k.example.com. 300 IN  CNAME   us.klb.loadbalanced.k.example.com.
cluster1-gw1-ns1.klb.loadbalanced.k.example.com. 60 IN A 172.18.200.1
cluster2-gw1-ns1.klb.loadbalanced.k.example.com. 60 IN A 172.18.200.2
cluster3-gw1-ns1.klb.loadbalanced.k.example.com. 60 IN A 172.18.200.3
ie.klb.loadbalanced.k.example.com. 60 IN CNAME  cluster2-gw1-ns1.klb.loadbalanced.k.example.com.
ie.klb.loadbalanced.k.example.com. 60 IN CNAME  cluster1-gw1-ns1.klb.loadbalanced.k.example.com.
kuadrant-1a20rnj9-a-cluster3-gw1-ns1.klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=2fgmvv67,external-dns/version=1\""
kuadrant-1a20rnj9-cname-us.klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=2fgmvv67,external-dns/version=1\""
kuadrant-2o2qjax9-a-cluster2-gw1-ns1.klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=26yn4kgw,external-dns/version=1\""
kuadrant-2o2qjax9-cname-ie.klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=26yn4kgw,external-dns/version=1\""
kuadrant-31aztxux-a-cluster1-gw1-ns1.klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34l61c8o,external-dns/version=1\""
kuadrant-31aztxux-cname-ie.klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34l61c8o,external-dns/version=1\""
us.klb.loadbalanced.k.example.com. 60 IN CNAME  cluster3-gw1-ns1.klb.loadbalanced.k.example.com.
kuadrant-1a20rnj9-cname-klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=2fgmvv67,external-dns/version=1\""
kuadrant-2o2qjax9-cname-klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=26yn4kgw,external-dns/version=1\""
kuadrant-2o2qjax9-cname-klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=26yn4kgw,external-dns/version=1\""
kuadrant-31aztxux-cname-klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34l61c8o,external-dns/version=1\""
kuadrant-31aztxux-cname-klb.loadbalanced.k.example.com. 0 IN TXT "\"heritage=external-dns,external-dns/owner=34l61c8o,external-dns/version=1\""
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
;; Query time: 1 msec
;; SERVER: 172.18.0.33#53(172.18.0.33) (TCP)
;; WHEN: Mon Sep 29 13:27:50 IST 2025
;; XFR size: 27 records (messages 1, bytes 3060)

```

Verify DNS Server(s) respond:
```shell
NS1=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-1 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`
NS2=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-2 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`

echo $NS1
echo $NS2

echo "Dig command: dig @$NS1 loadbalanced.k.example.com"
dig @$NS1 loadbalanced.k.example.com +short

echo "Dig command: dig @$NS2 loadbalanced.k.example.com"
# +subnet parameter simulates client location for GEO routing (127.0.100.0/24 = Ireland in demo DB)
dig @$NS2 loadbalanced.k.example.com +short +subnet=127.0.100.0/24
```
Expected output (from NS2 with Ireland subnet, routing to IE geo endpoint):
```
klb.loadbalanced.k.example.com.
ie.klb.loadbalanced.k.example.com.
cluster1-gw1-ns1.klb.loadbalanced.k.example.com.
172.18.200.1
```

Delete example DNSRecords:
```shell
# Run from dns-operator repository root
kubectl delete -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster1.yaml --context kind-kuadrant-dns-local-1
kubectl delete -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster2.yaml --context kind-kuadrant-dns-local-2
kubectl delete -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster3.yaml --context kind-kuadrant-dns-local-3
```

## GEO

This section describes how to test CoreDNS geographic routing capabilities using the embedded demo GeoIP database.

### GeoIP Database

The geo functionality is provided by the [geoip](https://coredns.io/plugins/geoip/) plugin from CoreDNS. The kuadrant CoreDNS container image has a demo database embedded at its root (`/GeoLite2-City-demo.mmdb`), generated using `coredns/plugin/geoip/db-generator.go`, for testing purposes.

**Note:** The database path differs between deployment contexts:
- **Container deployment** (Kind/Kubernetes): Use `/GeoLite2-City-demo.mmdb` (absolute path from container root)
- **Local development** (running from `coredns/plugin/` directory): Use `geoip/GeoLite2-City-demo.mmdb` (relative path to the plugin directory)

Ensure your Corefile uses the correct path for your deployment context.

The demo database contains sets of local subnets typical for Kind deployments that map to IE and US locales:

| Subnet           | Continent          | Country            |
|------------------|--------------------|--------------------|
| 127.0.100.0/24 	 | Europe / EU        | Ireland / IE       |
| 127.0.200.0/24  	 | North America / NA | United States / US |
| 10.89.100.0/24 	 | Europe / EU        | Ireland / IE       |
| 10.89.200.0/24 	 | North America / NA | United States / US |

### Testing Geographic Routing

**When running CoreDNS locally** (from terminal with `make run` in `coredns/plugin/` directory):
You can use the `+subnet` option with dig to specify a client subnet. For example:
- `dig @127.0.0.1 api.k.example.com -p 1053 +subnet=127.0.100.0/24` will be associated with IE locale
- `dig @127.0.0.1 api.k.example.com -p 1053 +subnet=127.0.200.0/24` will be associated with US locale

**When running CoreDNS in Kind cluster:**
The demo DB contains only localhost addresses which aren't routable in Kind. Instead, use the `+subnet` parameter with dig to simulate client location (replace `$NS1` with your CoreDNS service IP):
- `dig @$NS1 api.k.example.com +subnet=127.0.100.0/24` simulates a client from Ireland
- `dig @$NS1 api.k.example.com +subnet=127.0.200.0/24` simulates a client from United States

### Customizing the GeoIP Database

To add more subnets, generate a new database file by editing `coredns/plugin/geoip/db-generator.go`. Add your desired CIDR range to the constants and associate it with the desired record (IE or US).

For production deployments using a real-world database, refer to [MaxMind](https://dev.maxmind.com/geoip/) for their free database. Once obtained, it must be mounted and referenced in the Corefile instead of the demo database.

## Troubleshooting

### Kind Cluster Issues

**LoadBalancer IP not assigned:**

Check that MetalLB is installed and running (required for LoadBalancer services in Kind):
```shell
kubectl get pods -n metallb-system
```

If not present, MetalLB should be installed as part of `make local-setup`. Check the setup logs.

**CoreDNS pod not starting:**

Check pod status and logs:
```shell
kubectl get pods -n kuadrant-coredns
kubectl logs -n kuadrant-coredns deployment/kuadrant-coredns
```

Common issues:
- RBAC permissions missing (check ClusterRole and ClusterRoleBinding)
- Invalid Corefile configuration
- GeoIP database file not found

### DNSRecord Not Appearing in Zone

**Verify the DNSRecord has the zone label:**
```shell
kubectl get dnsrecords.kuadrant.io -n dnstest -o jsonpath='{.items[*].metadata.labels}' | grep kuadrant.io/coredns-zone-name
```

Should include `kuadrant.io/coredns-zone-name: k.example.com`.

If the label is missing, DNS Operator hasn't processed the record yet. Check:
```shell
# Check DNS Operator is running
kubectl get pods -n dns-operator-system

# Check DNS Operator logs
kubectl logs -n dns-operator-system deployment/dns-operator-controller-manager
```

**Zone transfer (AXFR) shows no records:**

Ensure the `transfer` plugin is enabled in the Corefile:
```corefile
k.example.com {
    transfer {
        to *
    }
    kuadrant
}
```

### Multi-Cluster Issues

**Cluster interconnection secrets not working:**

Verify secrets exist and have correct labels:
```shell
kubectl get secret -A -l kuadrant.io/multicluster-kubeconfig=true
```

For Kind clusters, ensure you used the correct multicluster setup command:
```shell
make multicluster-local-setup PRIMARY_CLUSTER_COUNT=2 CLUSTER_COUNT=3
```

**Authoritative DNSRecord not created on primary cluster:**

Check DNS Operator delegation role:
```shell
kubectl get configmap dns-operator-controller-env -n dns-operator-system -o jsonpath='{.data.DELEGATION_ROLE}'
```

Should be `primary` on primary clusters and `secondary` on secondary clusters.

**For general CoreDNS integration troubleshooting**, see the [Integration Guide Troubleshooting Section](coredns-integration.md#troubleshooting).

## Cleanup

### Single Cluster Cleanup

```shell
# Delete test DNSRecord
kubectl delete dnsrecords.kuadrant.io -n dnstest --all

# Delete test namespace
kubectl delete ns dnstest

# Delete the kind cluster
kind delete cluster --name kuadrant-dns-local
```

### Multi-Cluster Cleanup

```shell
# Delete DNSRecords from all clusters
kubectl delete dnsrecords.kuadrant.io -n dnstest --all --context kind-kuadrant-dns-local-1
kubectl delete dnsrecords.kuadrant.io -n dnstest --all --context kind-kuadrant-dns-local-2
kubectl delete dnsrecords.kuadrant.io -n dnstest --all --context kind-kuadrant-dns-local-3

# Delete test namespace from all clusters
kubectl delete ns dnstest --context kind-kuadrant-dns-local-1
kubectl delete ns dnstest --context kind-kuadrant-dns-local-2
kubectl delete ns dnstest --context kind-kuadrant-dns-local-3

# Delete all kind clusters
kind delete cluster --name kuadrant-dns-local-1
kind delete cluster --name kuadrant-dns-local-2
kind delete cluster --name kuadrant-dns-local-3
```

## Related Documentation

- **[Zone Delegation Guide](zone-delegation.md)** - Parent zone delegation setup using BIND9 as an example authoritative DNS server
- **[CoreDNS Configuration Reference](configuration.md)** - Comprehensive configuration options for CoreDNS integration
- **[CoreDNS Plugin README](../../coredns/plugin/README.md)** - Plugin syntax and quick-start examples
- **[CoreDNS Integration Guide](coredns-integration.md)** - Conceptual overview and production deployment
