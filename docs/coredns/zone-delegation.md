# Zone Delegation for CoreDNS

This guide explains how to set up parent zone delegation to enable CoreDNS to serve as an authoritative DNS server for a subdomain. This pattern is commonly used in development environments and scenarios where you want to delegate a subdomain (e.g., `k.example.com`) to CoreDNS while maintaining the parent zone (`example.com`) in a traditional authoritative DNS server.

## Overview

**Zone delegation** is a DNS pattern where a parent zone delegates authority for a subdomain to a different set of nameservers. In this setup:

- **Parent Zone** (`example.com`): Managed by an authoritative DNS server (BIND9, PowerDNS, cloud provider, etc.)
- **Delegated Zone** (`k.example.com`): Managed by CoreDNS with the Kuadrant plugin

**This guide uses BIND9 as the example authoritative DNS server**, but the same delegation pattern applies to any DNS server that supports:
- Dynamic DNS updates (nsupdate/RFC 2136)
- Zone transfers (AXFR)
- NS record delegation

Alternative servers include: PowerDNS, Knot DNS, NSD, Unbound, or cloud provider DNS services (Route53, Cloud DNS, Azure DNS).

## Table of Contents

1. [Overview](#overview)
2. [Prerequisites](#prerequisites)
3. [BIND9 Deployment Overview](#bind9-deployment-overview)
4. [Step 1: Verify BIND9 Installation](#step-1-verify-bind9-installation)
5. [Step 2: Identify CoreDNS Instances](#step-2-identify-coredns-instances)
6. [Step 3: Delegate Zone to CoreDNS](#step-3-delegate-zone-to-coredns)
7. [Step 4: Test Delegation with DNSRecord](#step-4-test-delegation-with-dnsrecord)
8. [Step 5: Configure In-Cluster DNS Forwarding (Optional)](#step-5-configure-in-cluster-dns-forwarding-optional)
9. [Step 6: Configure DNS Groups Active-Groups Resolution (Optional)](#step-6-configure-dns-groups-active-groups-resolution-optional)
10. [Additional Commands](#additional-commands)
11. [Troubleshooting](#troubleshooting)
12. [Related Documentation](#related-documentation)

## Prerequisites

> **Important**: All commands in this guide should be run from the **dns-operator repository root directory** unless otherwise specified. File paths like `config/bind9/ddns.key` and `coredns/examples/` are relative to this root.

Before starting, ensure you have:

1. **Local Kind cluster with CoreDNS and BIND9 deployed**:
   ```bash
   make local-setup
   ```
   This installs:
   - Kind cluster with MetalLB (for LoadBalancer IP assignment)
   - CoreDNS with Kuadrant plugin in `kuadrant-coredns` namespace
   - BIND9 authoritative DNS server in `kuadrant-bind9` namespace

   > **Important**: MetalLB is required for Kind clusters to assign external IPs to LoadBalancer services. Without it, BIND9 and CoreDNS services would remain in "Pending" state and not receive IPs for DNS queries. The IP range assigned depends on your Kind cluster's Docker network configuration (typically `172.18.x.x` range).

2. **Required tools**:
   - `kubectl` - Kubernetes CLI
   - `dig` - DNS query tool (BIND utilities)
   - `nsupdate` - Dynamic DNS update tool (BIND utilities)
   - `jq` - JSON processor

3. **Understanding of**:
   - DNS delegation concepts (NS records, glue records)
   - Kubernetes Services and LoadBalancer types
   - Basic DNS query tools (dig, nsupdate)

### DNS Query Flags Used in This Guide

Throughout this guide, we use `dig` commands with various flags. Here's what they mean:

- **`+short`** - Display only the answer section (minimal output)
- **`+norec`** - Non-recursive query (query the server directly without recursion, used for authoritative answers)
- **`-t AXFR`** - Request a zone transfer (list all records in a zone)
- **`-k <keyfile>`** - Use TSIG key authentication (required for authenticated operations)
- **`@<server>`** - Query a specific DNS server instead of the default resolver

**Example patterns:**
```bash
# Query for minimal output
dig @${EDGE_NS} api.k.example.com +short

# Query authoritative server directly (no recursion)
dig @${EDGE_NS} soa example.com +norec

# Transfer entire zone with authentication
dig @${EDGE_NS} -k config/bind9/ddns.key -t AXFR example.com
```

## BIND9 Deployment Overview

The `make local-setup` command deploys BIND9 with the following configuration:

**Service**: `kuadrant-bind9` in namespace `kuadrant-bind9`
- **Type**: LoadBalancer (provides external IP for DNS queries)
- **Ports**: 53 (TCP/UDP) → 1053 (container port)
- **Purpose**: Acts as authoritative DNS server for `example.com` zone

**Initial Zone**: `example.com`
- Pre-configured with SOA and NS records
- Allows dynamic updates via TSIG key authentication
- Supports zone transfers (AXFR) for verification

**TSIG Key**: `config/bind9/ddns.key`
- Provides authenticated dynamic DNS updates
- Required for nsupdate commands
- Algorithm: HMAC-SHA256

## Step 1: Verify BIND9 Installation

Check that BIND9 is running:

```bash
kubectl get deployments -l app.kubernetes.io/name=bind9 -A
```

**Expected output**:
```
NAMESPACE        NAME   READY   UP-TO-DATE   AVAILABLE   AGE
kuadrant-bind9   edge   1/1     1            1           22s
```

> **Note**: The label selector `app.kubernetes.io/name=bind9` works even though the deployment.yaml shows `app: edge` because Kustomize applies common labels during deployment (see `config/bind9/kustomization.yaml`). The actual deployment gets both labels applied.

Retrieve the BIND9 LoadBalancer IP (external DNS server address):

```bash
EDGE_NS="$(kubectl get service/kuadrant-bind9 -n kuadrant-bind9 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
echo "BIND9 DNS Server: ${EDGE_NS}"
```

Verify the `example.com` zone exists by querying the SOA record:

```bash
dig @${EDGE_NS} soa example.com +norec
```

**Expected output**:
```
;; ANSWER SECTION:
example.com.            30      IN      SOA     example.com. root.example.com. 16 30 30 30 30

;; AUTHORITY SECTION:
example.com.            30      IN      NS      ns.example.com.
```

The `+norec` flag prevents recursion, ensuring we query BIND9 directly as an authoritative server.

## Step 2: Identify CoreDNS Instances

List all CoreDNS instances running in the cluster:

```bash
kubectl get service -A \
  -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics
```

**Expected output**:
```
NAMESPACE          NAME               TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                     AGE
kuadrant-coredns   kuadrant-coredns   LoadBalancer   10.96.242.254   172.18.0.16   53:31680/UDP,53:31680/TCP   31m
```

**Note**: The `app.kubernetes.io/component!=metrics` label filter excludes metrics services, showing only DNS-serving instances.

Verify CoreDNS is configured to serve the `k.example.com` zone by checking logs:

```bash
kubectl logs -n kuadrant-coredns deployment/kuadrant-coredns | \
  grep "Starting informer"
```

**Expected log output**:
```
[INFO] plugin/kuadrant: Starting informer 0 for zone k.example.com.
```

Get the CoreDNS LoadBalancer IP (used for delegation):

```bash
CORE_NS="$(kubectl get service -A \
  -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics \
  -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')"
echo "CoreDNS Server: ${CORE_NS}"
```

**Note**: In multi-cluster scenarios, you would retrieve multiple IPs and create multiple NS records.

## Step 3: Delegate Zone to CoreDNS

### Understanding the Delegation

To delegate `k.example.com` to CoreDNS, we need to add two records to the parent zone (`example.com`):

1. **NS Record**: `k.example.com 300 IN NS ns1.k.example.com`
   - Declares that `ns1.k.example.com` is authoritative for `k.example.com`

2. **A Record (Glue Record)**: `ns1.k.example.com 300 IN A <CORE_NS>`
   - Provides the IP address of the CoreDNS server
   - Required because the nameserver is within the delegated zone (RFC 1034)

**Without the glue record**, recursive resolvers would have a circular dependency:
- To resolve `k.example.com`, query `ns1.k.example.com`
- To resolve `ns1.k.example.com`, query `k.example.com` → **circular lookup**

### Generate nsupdate File

Create a dynamic DNS update file:

```bash
EDGE_NS="$(kubectl get service/kuadrant-bind9 -n kuadrant-bind9 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
CORE_NS="$(kubectl get service -A \
  -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics \
  -o jsonpath='{.items[0].status.loadBalancer.ingress[0].ip}')"

cat <<EOF >nsupdate-k-example-com
server ${EDGE_NS}
debug yes
zone example.com.
update add k.example.com 300 IN NS ns1.k.example.com
update add ns1.k.example.com 300 IN A ${CORE_NS}
send
EOF
```

**Verify the generated file**:

```bash
cat nsupdate-k-example-com
```

**Example output**:
```
server 172.18.0.17
debug yes
zone example.com.
update add k.example.com 300 IN NS ns1.k.example.com
update add ns1.k.example.com 300 IN A 172.18.0.16
send
```

### Apply the Update

Use `nsupdate` with TSIG key authentication to apply the changes:

```bash
nsupdate -k config/bind9/ddns.key -v nsupdate-k-example-com
```

**Expected output**:
```
Sending update to 172.18.0.17#53
Outgoing update query:
;; UPDATE SECTION:
k.example.com.          300     IN      NS      ns1.k.example.com.
ns1.k.example.com.      300     IN      A       172.18.0.16

Reply from update query:
;; ->>HEADER<<- opcode: UPDATE, status: NOERROR
```

### Verify Zone Delegation

**1. Verify via zone transfer (AXFR)**:

```bash
dig @${EDGE_NS} -k config/bind9/ddns.key -t AXFR example.com
```

**Expected output** (showing the new delegation records):
```
example.com.            30      IN      SOA     example.com. root.example.com. 17 30 30 30 30
example.com.            30      IN      NS      ns.example.com.
k.example.com.          300     IN      NS      ns1.k.example.com.
ns1.k.example.com.      300     IN      A       172.18.0.16
ns.example.com.         30      IN      A       127.0.0.1
example.com.            30      IN      SOA     example.com. root.example.com. 17 30 30 30 30
```

The presence of the `k.example.com NS` and `ns1.k.example.com A` records confirms successful delegation.

**2. Verify SOA record of delegated zone**:

```bash
dig @${EDGE_NS} soa k.example.com
```

**Expected output**:
```
;; ANSWER SECTION:
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
```

The SOA record should show `ns1.k.example.com` as the primary nameserver, confirming CoreDNS is responding authoritatively.

## Step 4: Test Delegation with DNSRecord

Create a test DNSRecord to verify end-to-end delegation:

```bash
# Run from the repository root directory
kubectl apply -f coredns/examples/dnsrecord-api-k-example-com_geo_weight.yaml
```

**Create the provider secret:**

The DNSRecord includes `providerRef: dns-provider-credentials-coredns`. For DNS Operator to reconcile this DNSRecord, the provider secret must exist in the namespace.

**Why the secret is required**:
- DNS Operator uses the secret's `ZONES` field for zone matching (matching `rootHost` to an available zone)
- After zone matching succeeds, DNS Operator applies the `kuadrant.io/coredns-zone-name` label
- CoreDNS plugin watches for this label and serves the DNSRecord
- Without the secret, DNS Operator reconciliation will fail even if you manually apply the label

Create the provider secret:

```bash
kubectl create secret generic dns-provider-credentials-coredns -n dnstest \
  --type=kuadrant.io/coredns \
  --from-literal=ZONES="k.example.com"
```

DNS Operator will automatically apply the zone label within a few seconds after the secret is created.

> **Note**: For more details on provider secrets, zone matching, and automatic vs manual labeling scenarios, see [Provider Secret Configuration](configuration.md#provider-secret-configuration).

Query the delegated DNS record via BIND9:

```bash
dig @${EDGE_NS} api.k.example.com +short
```

**Expected output**:
```
klb.api.k.example.com.
geo-us.klb.api.k.example.com.
cluster1.klb.api.k.example.com.
127.0.0.1
```

This output shows:
1. BIND9 received the query for `api.k.example.com`
2. BIND9 delegated to CoreDNS (`ns1.k.example.com`)
3. CoreDNS returned the CNAME chain and IP address
4. The delegation is working correctly

## Step 5: Configure In-Cluster DNS Forwarding (Optional)

This step enables pods running **inside the cluster** to resolve DNS records via the BIND9 edge server. This is useful for:
- Testing DNS Groups active-passive failover
- Simulating external DNS resolution from inside the cluster
- Resolving external TXT records (e.g., `kuadrant-active-groups.example.com`)

### Update Cluster CoreDNS Forwarder

Get the BIND9 **ClusterIP** (in-cluster service address):

```bash
CLUSTER_EDGE_NS="$(kubectl get service/kuadrant-bind9 -n kuadrant-bind9 \
  -o jsonpath='{.spec.clusterIP}')"
echo "BIND9 ClusterIP: ${CLUSTER_EDGE_NS}"
```

**Note**: We use `clusterIP` (not LoadBalancer `ingress` IP) because pods communicate with Services via internal Kubernetes networking.

Retrieve the current kube-system CoreDNS Corefile:

```bash
kubectl get configmap/coredns -n kube-system \
  -o jsonpath='{.data.Corefile}' > kube.Corefile
```

Prepend `example.com` zone forwarding to the Corefile:

```bash
ZONE=example.com
cat <<EOF | cat - kube.Corefile > /tmp/kube.Corefile.new && mv /tmp/kube.Corefile.new kube.Corefile
${ZONE}:53 {
    forward . ${CLUSTER_EDGE_NS}
}
EOF
```

**Review the updated Corefile**:

```bash
cat kube.Corefile
```

**Expected output** (showing the new zone block at the top):
```
example.com:53 {
    forward . 10.96.123.45
}

.:53 {
    errors
    health
    ...
}
```

Apply the updated Corefile:

```bash
kubectl create configmap coredns -n kube-system \
  --from-file=Corefile=kube.Corefile --dry-run=client -o yaml | kubectl apply -f -
```

### Verify In-Cluster DNS Resolution

Test that pods can resolve records via the edge server:

```bash
kubectl run -n default dig --attach --rm --restart=Never \
  --image=docker.io/curlimages/curl:latest \
  -- sh -c "apk add --no-cache bind-tools && dig k.example.com NS +short"
```

**Expected output**:
```
ns1.k.example.com.
```

**Alternative verification using a long-running pod**:

```bash
kubectl run -n default test-dns --image=docker.io/curlimages/curl:latest \
  --command -- sleep 3600
kubectl exec -n default test-dns -- sh -c \
  "apk add --no-cache bind-tools && dig k.example.com NS +short"
kubectl delete pod -n default test-dns
```

**Note**: We replaced the `toolbelt/dig` image with `curlimages/curl` (a well-maintained, official image) and install `bind-tools` for DNS utilities.

## Step 6: Configure DNS Groups Active-Groups Resolution (Optional)

> **For complete DNS Groups documentation**, see the [DNS Groups Configuration](configuration.md#dns-groups-configuration) section, which covers:
> - What DNS Groups are and how active-passive failover works
> - Configuring DNS Operator with group identifiers
> - Creating and managing active groups TXT records
>
> **For failover procedures**, see [Exercising DNS Failover via Groups](../exercising_dns_failover_via_groups.md)

**Prerequisites for this step:**
- Understanding of DNS Groups concept (see link above)
- DNS Operator configured with `GROUP` environment variable or `--group` flag
- Step 5 completed (in-cluster DNS forwarding configured)

**What's different for BIND9 delegation**: With parent zone delegation, the active-groups TXT record lives in the parent zone (`example.com`) rather than an external DNS provider. DNS Operator queries `kuadrant-active-groups.k.example.com`, so we need kuadrant-coredns to rewrite this to `k.kuadrant-active-groups.example.com` and forward to BIND9.

### Add Active-Groups TXT Record

**For BIND9 (local development)**, use nsupdate:

```bash
EDGE_NS="$(kubectl get service/kuadrant-bind9 -n kuadrant-bind9 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

cat <<EOF >nsupdate-active-groups
server ${EDGE_NS}
zone example.com.
update add k.kuadrant-active-groups.example.com 300 TXT "version=1;groups=primary-us-east"
send
EOF

nsupdate -k config/bind9/ddns.key -v nsupdate-active-groups
```

**For production (cloud DNS providers)**, use the DNS CLI instead:

```bash
# Build the CLI (run once)
make build-cli

# Add active group using DNS CLI
kubectl-kuadrant_dns add-active-group primary-us-east \
  --providerRef <namespace>/<secret-name> \
  --domain <your-domain>
```

Where `<secret-name>` is your AWS/GCP/Azure provider secret. The CLI automatically creates the TXT record in the correct format and handles multiple zones.

### Add Rewrite Rule to Kuadrant CoreDNS

Add a rewrite rule to the kuadrant-coredns Corefile using the same pattern from Step 5:

```bash
CLUSTER_EDGE_NS="$(kubectl get service/kuadrant-bind9 -n kuadrant-bind9 \
  -o jsonpath='{.spec.clusterIP}')"

kubectl get configmap/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.data.Corefile}' > kuadrant.Corefile

cat <<EOF | cat - kuadrant.Corefile > /tmp/kuadrant.Corefile.new && mv /tmp/kuadrant.Corefile.new kuadrant.Corefile
kuadrant-active-groups.k.example.com {
    rewrite name exact kuadrant-active-groups.k.example.com k.kuadrant-active-groups.example.com
    forward . ${CLUSTER_EDGE_NS}
}
EOF

kubectl create configmap kuadrant-coredns -n kuadrant-coredns \
  --from-file=Corefile=kuadrant.Corefile --dry-run=client -o yaml | kubectl apply -f -

kubectl rollout restart -n kuadrant-coredns deployment/kuadrant-coredns
```

### Verify

Test that DNS Groups active-groups resolution works:

```bash
kubectl run -n default dig-test --attach --rm --restart=Never \
  --image=docker.io/curlimages/curl:latest \
  -- sh -c "apk add --no-cache bind-tools && dig kuadrant-active-groups.k.example.com TXT +short"
```

**Expected**: `"version=1;groups=primary-us-east"`

## Additional Commands

### Generate New DDNS Key

If you need to regenerate the TSIG key:

```bash
ddns-confgen -k example.com-key -z example.com.
```

This outputs configuration for both BIND9 named.conf and the key file.

### Monitor All DNS Logs

Tail logs from both CoreDNS and BIND9:

```bash
kubectl stern -l 'app.kubernetes.io/name in (coredns, bind9)' -A
```

**Note**: Requires [stern](https://github.com/stern/stern) to be installed.

### Verify BIND9 Configuration Files

The BIND9 deployment includes:

**Zone Configuration** (`config/bind9/zone.yaml`):
- Defines `example.com` zone
- Configures TSIG key for authentication
- Allows dynamic updates and zone transfers

**DDNS Key** (`config/bind9/ddns.key`):
- TSIG key for authenticated updates
- Algorithm: HMAC-SHA256
- Used with `-k` flag in nsupdate and dig commands

**Deployment** (`config/bind9/deployment.yaml`):
- Runs BIND9 in namespace `kuadrant-bind9`
- Listens on container port 1053
- Mounts zone configuration and DDNS key

**Service** (`config/bind9/service.yaml`):
- Type: LoadBalancer
- Exposes port 53 (DNS standard) → 1053 (container)
- Provides external IP for DNS queries

## Troubleshooting

### nsupdate Fails with "update failed: NOTAUTH"

**Cause**: TSIG key authentication failed.

**Solution**:
- Verify key file path: `config/bind9/ddns.key`
- Ensure key name in file matches zone configuration: `example.com-key`
- Check that you're running from repository root

### dig Shows SERVFAIL for Delegated Zone

**Cause**: Delegation records not properly configured.

**Solution**:
1. Verify NS record exists:
   ```bash
   dig @${EDGE_NS} k.example.com NS +short
   ```
   Should return: `ns1.k.example.com.`

2. Verify glue record exists:
   ```bash
   dig @${EDGE_NS} ns1.k.example.com A +short
   ```
   Should return CoreDNS LoadBalancer IP

3. Check zone transfer to see all records:
   ```bash
   dig @${EDGE_NS} -k config/bind9/ddns.key -t AXFR example.com
   ```

### CoreDNS Not Serving DNSRecord

**Cause**: Missing zone label on DNSRecord.

**Solution**:
Add the label to tell CoreDNS to serve this record:
```bash
kubectl label dnsrecords.kuadrant.io/<name> -n <namespace> \
  kuadrant.io/coredns-zone-name=k.example.com
```

### Pods Can't Resolve example.com Records

**Cause**: kube-system CoreDNS not configured to forward to BIND9.

**Solution**:
Verify forwarding configuration exists in kube-system CoreDNS:
```bash
kubectl get configmap/coredns -n kube-system -o yaml
```

Should contain:
```
example.com:53 {
    forward . <CLUSTER_EDGE_NS>
}
```

### Active-Groups TXT Record Returns NXDOMAIN

**Cause**: Rewrite rule not applied or incorrect.

**Solution**:
1. Check kuadrant-coredns Corefile:
   ```bash
   kubectl get configmap/kuadrant-coredns -n kuadrant-coredns \
     -o jsonpath='{.data.Corefile}'
   ```

2. Verify rewrite syntax:
   ```
   rewrite name exact kuadrant-active-groups.k.example.com k.kuadrant-active-groups.example.com
   ```

3. Restart CoreDNS after configuration changes:
   ```bash
   kubectl rollout restart -n kuadrant-coredns deployment/kuadrant-coredns
   ```

## Related Documentation

- [CoreDNS Local Development Guide](local-development.md) - Getting started with CoreDNS integration
- [CoreDNS Configuration Reference](configuration.md) - Comprehensive configuration options
- [DNS Groups Configuration](configuration.md#dns-groups-configuration) - Active-passive failover setup
- [Provider Secret Configuration](configuration.md#provider-secret-configuration) - CoreDNS provider secrets
