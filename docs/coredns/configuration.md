# CoreDNS Configuration Reference

This document provides comprehensive technical information for configuring CoreDNS integration with DNS Operator. Each section follows the pattern: **What** (parameter/concept) → **How** (configuration example) → **Result** (expected outcome).

## Table of Contents

1. [Corefile Configuration](#corefile-configuration)
2. [Zone Configuration](#zone-configuration)
3. [Provider Secret Configuration](#provider-secret-configuration)
4. [DNS Groups Configuration](#dns-groups-configuration)
5. [GeoIP Database Configuration](#geoip-database-configuration)
6. [Advanced Routing Strategies](#advanced-routing-strategies)
7. [Monitoring and Observability](#monitoring-and-observability)
8. [Logging Configuration](#logging-configuration)

---

## Corefile Configuration

### What

CoreDNS is configured via a Corefile that defines which zones it serves and how it processes DNS queries. The Kuadrant CoreDNS plugin extends CoreDNS to watch Kubernetes DNSRecord resources and serve them as DNS responses.

### How

**Basic Corefile Structure**:

```corefile
k.example.com {
    debug
    errors
    log
    health {
        lameduck 5s
    }
    ready
    geoip GeoLite2-City-demo.mmdb {
        edns-subnet
    }
    metadata
    transfer {
        to *
    }
    kuadrant
    prometheus 0.0.0.0:9153
}
```

**Key Concepts**:

- **Zone Coordination**: Each zone in the Corefile must match a zone listed in your CoreDNS provider secret's `ZONES` field (see [Provider Secret Configuration](#provider-secret-configuration) for details)
- **Required Plugins**: For geo-routing to work, ensure `geoip` and `metadata` plugins are included in your Corefile. Plugin execution order is determined at build time (see `coredns/plugin/cmd/plugin/plugin.go`), not by Corefile order, so you can list plugins in any order.
- **Watch Mechanism**: The Kuadrant plugin watches for DNSRecords labeled with `kuadrant.io/coredns-zone-name: <zone>`

**Core Plugin**:

**kuadrant** - Kuadrant DNS integration (required)
```corefile
kuadrant
```

**Plugins for Geo-Routing** (required for geo/weighted routing):

**metadata** - Enables metadata extraction for routing decisions
```corefile
metadata
```

**geoip** - Provides geographic IP lookup
```corefile
geoip /path/to/GeoLite2-City.mmdb {
    edns-subnet
}
```

**Recommended Plugins**:

**health** - Kubernetes liveness probe endpoint
```corefile
health {
    lameduck 5s
}
```

**ready** - Kubernetes readiness probe endpoint
```corefile
ready
```

**transfer** - Zone transfer support (useful for debugging and secondary servers)
```corefile
transfer {
    to *
}
```

**SOA RNAME Configuration**:

The `rname` directive customizes the email address (RNAME) in the SOA (Start of Authority) record for the zone.

**Syntax**:
```corefile
kuadrant {
    rname EMAIL
}
```

**Default Behavior**: If not specified, defaults to `hostmaster.{zone}` (e.g., `hostmaster.k.example.com.`)

**Email Format Conversion**: The email format (e.g., `admin@example.com`) is automatically converted to DNS mailbox format (e.g., `admin.example.com.`). According to [RFC 1035](https://www.rfc-editor.org/rfc/rfc1035.html) and [RFC 2142](https://www.rfc-editor.org/rfc/rfc2142.html), any dots in the local part (before @) are escaped with backslash (e.g., `dns.admin@example.com` becomes `dns\.admin.example.com.`).

**Example**:
```corefile
k.example.com {
    kuadrant {
        rname admin@example.com
    }
}
```

### Result

**Verification**:
```bash
# Query SOA record to verify RNAME
dig @${NS} k.example.com SOA +short
```

Expected output:
```
ns1.k.example.com. admin.example.com. 12345 7200 1800 86400 60
```

The second field (`admin.example.com.`) is the converted RNAME.

### Additional Resources

- **[Kuadrant CoreDNS Plugin Documentation](../../coredns/plugin/README.md)** - Complete plugin syntax, examples, and development guide
- **[CoreDNS Official Documentation](https://coredns.io/manual/toc/)** - General CoreDNS configuration and plugin reference

---

## Zone Configuration

### What

When using the Kuadrant CoreDNS plugin, you need to configure the zone's authoritative nameserver records. The plugin initializes each zone with a single hardcoded NS record (`ns1.<zone-name>`).

A zone configuration typically includes:
- **Multiple NS records** (IANA requirement: minimum 2 nameservers)
- **Corresponding A records** mapping nameserver hostnames to their IP addresses
- **Consistency** with parent zone delegation configuration
- **Parent zone delegation** with NS records and glue A records

### How

Use DNSRecord resources to configure zone apex NS and A records.

#### Deployment Patterns

##### Single Cluster Setup

**Configuration**:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: dnsrecord-k-example-com-zone-config
  namespace: kuadrant-coredns
spec:
  rootHost: k.example.com
  providerRef:
    name: dns-provider-credentials-coredns
  endpoints:
  # NS records for the zone
  # Note: Both NS records will point to the same CoreDNS instance IP
  # This does NOT provide true redundancy
  - dnsName: k.example.com
    recordTTL: 60
    recordType: NS
    targets:
    - ns1.k.example.com
    - ns2.k.example.com
  # A record for ns1 (single CoreDNS instance)
  - dnsName: ns1.k.example.com
    recordTTL: 60
    recordType: A
    targets:
    - 172.18.0.17  # IP of the single CoreDNS instance
  # A record for ns2 (same IP as ns1 - no actual redundancy)
  - dnsName: ns2.k.example.com
    recordTTL: 60
    recordType: A
    targets:
    - 172.18.0.17  # Same IP - this is not redundant!
```

**Determining CoreDNS IP** (Kind clusters):
```bash
NS1="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
echo "CoreDNS IP: ${NS1}"
```

**Characteristics**:
- Single CoreDNS instance serves the zone
- Multiple NS records can be configured, but all point to the same IP address
- If the CoreDNS instance fails, the zone becomes unavailable

##### Multi-Cluster Setup

Deploy CoreDNS across multiple clusters with delegation enabled. Each primary cluster runs its own CoreDNS instance, and the NS record merging logic combines nameserver records from all clusters into the authoritative zone.

**Architecture**:
- **Multiple Primary Clusters**: Each runs a CoreDNS instance with the Kuadrant plugin
- **Delegation Mode**: Each cluster creates delegated DNSRecord resources
- **Automatic Merging**: The operator merges NS records from all clusters
- **True Redundancy**: Each nameserver has a unique IP from a different cluster

**Configuration**:

**Cluster 1 (Primary)**:
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: dnsrecord-k-example-com-zone-config
  namespace: kuadrant-coredns
spec:
  rootHost: k.example.com
  delegate: true
  endpoints:
  # This cluster contributes ns1.k.example.com
  - dnsName: k.example.com
    recordTTL: 60
    recordType: NS
    targets:
    - ns1.k.example.com
  # A record for ns1 pointing to Cluster 1's CoreDNS
  - dnsName: ns1.k.example.com
    recordTTL: 60
    recordType: A
    targets:
    - 172.18.0.17  # Cluster 1 CoreDNS LoadBalancer IP
```

**Cluster 2 (Primary)**:
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: dnsrecord-k-example-com-zone-config
  namespace: kuadrant-coredns
spec:
  rootHost: k.example.com
  delegate: true
  endpoints:
  # This cluster contributes ns2.k.example.com
  - dnsName: k.example.com
    recordTTL: 60
    recordType: NS
    targets:
    - ns2.k.example.com
  # A record for ns2 pointing to Cluster 2's CoreDNS
  - dnsName: ns2.k.example.com
    recordTTL: 60
    recordType: A
    targets:
    - 172.18.0.18  # Cluster 2 CoreDNS LoadBalancer IP
```

**Optional Cluster 3+ (Primary)**:

For additional redundancy, add more clusters following the same pattern with `ns3.k.example.com`, `ns4.k.example.com`, etc.

**Determining CoreDNS IPs** (Multi-cluster Kind):
```bash
# Cluster 1
NS1="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-1 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
echo "Cluster 1 CoreDNS IP: ${NS1}"

# Cluster 2
NS2="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-2 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"
echo "Cluster 2 CoreDNS IP: ${NS2}"
```

**Cloud Providers**:
```bash
# Most cloud providers (GCP, Azure, DigitalOcean) assign an IP
kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

# AWS ELB/NLB typically provides a hostname instead of IP
kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

> **Note on AWS LoadBalancers**: AWS ELB/NLB services typically expose a DNS hostname (e.g., `abc123.elb.us-east-1.amazonaws.com`) rather than a static IP address. You can use the ELB hostname directly as your NS record target without needing glue A records, since the hostname is in a different DNS zone and resolvers can query it independently.

### Result

Verify that CoreDNS is serving the configured NS and A records for the zone.

#### Verify NS and A Records

```bash
# Get CoreDNS IP
NS1="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

# Verify NS records
dig @${NS1} k.example.com NS +short

# Verify nameserver A records
dig @${NS1} ns1.k.example.com +short
dig @${NS1} ns2.k.example.com +short
```

**Expected output**:
```
# NS records
ns1.k.example.com.
ns2.k.example.com.

# A records
172.18.0.17
172.18.0.18
```

#### Optional: Zone Transfer

If you have the `transfer` plugin enabled, you can view all zone records at once:

```bash
# Get CoreDNS IP
NS1="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

# Request full zone transfer
dig @${NS1} -t AXFR k.example.com
```

**Expected output**:
```
k.example.com.          60  IN  SOA  ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60  IN  NS   ns1.k.example.com.
k.example.com.          60  IN  NS   ns2.k.example.com.
ns1.k.example.com.      60  IN  A    172.18.0.17
ns2.k.example.com.      60  IN  A    172.18.0.18
k.example.com.          60  IN  SOA  ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
```

#### Parent Zone Delegation

After configuring the CoreDNS zone, you must update the parent zone to delegate authority for your subdomain. The parent zone needs NS records and corresponding glue records (A records).

> **For a comprehensive step-by-step guide to parent zone delegation**, including local BIND9 setup, dynamic DNS updates, and in-cluster DNS forwarding, see the [Zone Delegation Guide](zone-delegation.md).

**What**: Glue records are **required** when nameserver hostnames are within the delegated zone (e.g., `ns1.k.example.com` for zone `k.example.com`). For a detailed explanation, see [RFC 9471 - DNS Glue Requirements](https://www.rfc-editor.org/rfc/rfc9471).

**How**: If your CoreDNS manages `k.example.com` and the parent zone is `example.com`:

**For BIND9** (example.com zone file):
```zone
; Delegation for k.example.com to CoreDNS instances
k.example.com.          300  IN  NS  ns1.k.example.com.
k.example.com.          300  IN  NS  ns2.k.example.com.

; Glue records (required because nameservers are within delegated zone)
ns1.k.example.com.      300  IN  A   172.18.0.17
ns2.k.example.com.      300  IN  A   172.18.0.18
```

**For dynamic updates** (BIND9 with nsupdate):
```bash
EDGE_NS="<parent-nameserver>"
cat <<EOF >nsupdate-delegation
server ${EDGE_NS}
debug yes
zone example.com.
update add k.example.com 300 IN NS ns1.k.example.com.
update add k.example.com 300 IN NS ns2.k.example.com.
update add ns1.k.example.com 300 IN A 172.18.0.17
update add ns2.k.example.com 300 IN A 172.18.0.18
send
EOF

nsupdate -k /path/to/ddns.key -v nsupdate-delegation
```

**For cloud DNS providers** (AWS Route53, Google Cloud DNS, Azure DNS):

Use DNS Operator to manage parent zone delegation records:

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: parent-zone-delegation-k-example-com
  namespace: kuadrant-coredns
spec:
  rootHost: example.com
  providerRef:
    name: dns-provider-credentials  # Secret with provider credentials
  endpoints:
  # NS records for subdomain delegation
  - dnsName: k.example.com
    recordTTL: 300
    recordType: NS
    targets:
    - ns1.k.example.com
    - ns2.k.example.com
  # Glue records for nameservers
  - dnsName: ns1.k.example.com
    recordTTL: 300
    recordType: A
    targets:
    - 172.18.0.17
  - dnsName: ns2.k.example.com
    recordTTL: 300
    recordType: A
    targets:
    - 172.18.0.18
```

**TTL Best Practices**:
- **Within parent zone**: NS and A (glue) records should use the same TTL (e.g., 300 seconds or longer)
- **Within child zone**: NS and A records should use the same TTL (e.g., 60 seconds)
- **Between zones**: Parent and child zones MAY use different TTL values ([RFC 7477](https://www.rfc-editor.org/rfc/rfc7477.html))
  - Parent delegation NS records typically use longer TTLs (1 day is common, minimum 1 hour recommended)
  - Child authoritative NS records can use shorter TTLs for faster updates
- Using consistent TTLs within each zone ensures related records expire together

**Result**: Verify delegation from parent zone:

```bash
# Query parent zone's nameserver for delegation
dig @<parent-nameserver> k.example.com NS +short
```

Expected output:
```
ns1.k.example.com.
ns2.k.example.com.
```

#### Security Considerations

**Critical: Zone configuration DNSRecords have full control over the zone's nameserver records.** Anyone who can create or modify DNSRecords targeting the zone apex with NS records can effectively control the entire zone's DNS resolution.

##### Access Control Best Practices

1. **Separate namespaces**: Keep zone configuration DNSRecords in a dedicated, restricted namespace
2. **Strict RBAC**: Only grant DNSRecord creation permissions to trusted administrators
3. **Namespace isolation**: Application teams should only have DNSRecord permissions in their own namespaces
4. **Audit logging**: Enable Kubernetes audit logging to track all DNSRecord changes
5. **Version control**: Store zone configuration DNSRecord YAML in Git with review processes

**Example RBAC Configuration**:

```yaml
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: dns-zone-config-admin
  namespace: kuadrant-coredns
rules:
- apiGroups: ["kuadrant.io"]
  resources: ["dnsrecords"]
  verbs: ["create", "update", "patch", "delete"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: dns-zone-config-admin-binding
  namespace: kuadrant-coredns
subjects:
- kind: User
  name: dns-admin@example.com  # Only trusted administrators
  apiGroup: rbac.authorization.k8s.io
roleRef:
  kind: Role
  name: dns-zone-config-admin
  apiGroup: rbac.authorization.k8s.io
```

##### Attack Scenarios

- **Zone Hijacking**: Attacker replaces NS records with malicious nameservers, redirecting all zone queries
- **Denial of Service**: Invalid NS records break DNS resolution for the entire zone
- **DNS Poisoning**: Malicious NS records direct queries to servers returning false data

**Mitigation**: Strict RBAC, namespace isolation, audit logging, admission webhooks for validation.

---

## Provider Secret Configuration

### What

CoreDNS provider secrets enable DNS Operator to match DNSRecords to zones and coordinate with CoreDNS.

**Important**: At least one provider secret is always required for CoreDNS integration. Setting up the zone itself requires non-delegating DNSRecords, which need a provider secret (see "When Provider Secrets Are Needed" below).

#### When Provider Secrets Are Needed

Provider secrets are **only required for non-delegating DNSRecords**:

- **Non-delegating DNSRecords** (without `delegate: true`): Provider secret **required** (via `providerRef` or default provider label)
  - DNS Operator uses the secret for zone matching and endpoint validation
  - DNS Operator adds the `kuadrant.io/coredns-zone-name` label to the DNSRecord
  - CoreDNS plugin watches and serves the labeled DNSRecord

- **Delegating DNSRecords** (`delegate: true`): Provider secret **not needed**
  - Managed by primary clusters that handle delegation
  - Primary clusters add the label and CoreDNS serves the merged authoritative record
  - Note: `delegate: true` and `providerRef` are mutually exclusive fields

### How

#### Secret Structure

**Secret Type**: `kuadrant.io/coredns`

**Required Fields**:
```yaml
data:
  ZONES: "k.example.com,k2.example.com"  # Required: Comma-separated zones managed by CoreDNS
```

**Optional Fields**:
```yaml
data:
  NAMESERVERS: "10.22.100.23:53,10.22.100.24:53"  # Optional: For DNS groups active-groups TXT queries
```

> **Note**: The `NAMESERVERS` field is used for DNS groups failover feature to query active groups TXT records. It is **not** related to CoreDNS integration itself.

#### Creating a Provider Secret

```bash
kubectl create secret generic dns-provider-credentials-coredns \
  --namespace=<target-namespace> \
  --type=kuadrant.io/coredns \
  --from-literal=ZONES="k.example.com"
```

#### Setting as Default Provider

```bash
kubectl label secret dns-provider-credentials-coredns \
  -n <namespace> \
  kuadrant.io/default-provider=true
```

This allows non-delegating DNSRecords to use this secret automatically without specifying `providerRef`.

### Result

#### How DNS Operator Uses Provider Secrets

When a non-delegating DNSRecord references a CoreDNS provider secret, DNS Operator performs:

1. **Zone Matching**: Uses the `ZONES` field to find the appropriate zone for the DNSRecord's `rootHost`
2. **Endpoint Validation**: Validates the DNSRecord endpoints conform to CoreDNS requirements
3. **Label Application**: Adds `kuadrant.io/coredns-zone-name: <zone>` label so CoreDNS plugin can discover the record
4. **No Record Pushing**: Unlike cloud providers, DNS Operator does NOT push records to CoreDNS via API - the label triggers the watch

#### Zone Coordination

Zones must be explicitly listed in both:
- **The Corefile**: Tells CoreDNS which zones to serve
- **The provider secret `ZONES` field**: Tells DNS Operator which zones are available for matching

**Verification**:

```bash
# Verify provider secret exists and has correct zones
kubectl get secret dns-provider-credentials-coredns -n kuadrant-coredns \
  -o jsonpath='{.data.ZONES}' | base64 --decode
```

Expected output:
```
k.example.com
```

---

## DNS Groups Configuration

### What

DNS Groups provide active-passive failover across multiple clusters. Each DNS operator instance can belong to a group, and only the active groups process and publish DNS records. This enables controlled failover scenarios where you can switch traffic between cluster groups by updating an external TXT record.

**Key Concepts**:
- **Group Identifier**: Set via `--group` flag or `GROUP` environment variable
- **Active Groups TXT Record**: External DNS TXT record containing the list of active groups
- **Failover Mechanism**: Only DNS operators in active groups reconcile and publish records

### How

#### Step 1: Configure DNS Operator with Group

When running the DNS operator, specify the group identifier:

```bash
# Using make (local development)
make run GROUP=primary-us-east

# Using environment variable
export GROUP=primary-us-east
make run
```

For deployed controllers, set the `GROUP` environment variable in the deployment:

```yaml
apiVersion: apps/v1
kind: Deployment
metadata:
  name: dns-operator-controller-manager
  namespace: dns-operator-system
spec:
  template:
    spec:
      containers:
      - name: manager
        env:
        - name: GROUP
          value: "primary-us-east"
```

#### Step 2: Configure CoreDNS to Resolve Active Groups TXT Record

When using CoreDNS locally with DNS Groups, you need to configure CoreDNS to forward the active-groups TXT record query to an external resolver.

> **Note**: If using parent zone delegation with BIND9, see the [Zone Delegation Guide - Step 6](zone-delegation.md#step-6-configure-dns-groups-active-groups-resolution-optional) for the BIND9-specific variant of this configuration.

**Option A: Using a CNAME DNSRecord (Recommended)**

Create a DNSRecord with a CNAME that points to the external active-groups TXT record host. This approach is more Kubernetes-native and doesn't require Corefile modifications.

```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: kuadrant-active-groups-cname
  namespace: kuadrant-coredns
  labels:
    kuadrant.io/coredns-zone-name: k.example.com
spec:
  rootHost: kuadrant-active-groups.k.example.com
  providerRef:
    name: dns-provider-credentials-coredns
  endpoints:
  - dnsName: kuadrant-active-groups.k.example.com
    recordType: CNAME
    recordTTL: 60
    targets:
    - kuadrant-active-groups.hcpapps.net
```

Replace:
- `k.example.com` with your actual zone domain
- `kuadrant-active-groups.hcpapps.net` with your external TXT record host

**Key Points**:
- The `kuadrant.io/coredns-zone-name` label ensures CoreDNS picks up this record
- The CNAME allows CoreDNS to resolve the active-groups query without Corefile changes
- No CoreDNS restart required

**Option B: Using Corefile Rewrite and Forward Directives**

Alternatively, you can modify the Corefile to rewrite and forward the active-groups query. **Note: This approach requires modifying the Corefile and restarting CoreDNS.**

The Corefile is located in the `kuadrant-coredns` ConfigMap in the `kuadrant-coredns` namespace.

Add the following configuration **before** the `ready` statement:

```corefile
k.example.com {
    # Forward active groups TXT record query to external resolver
    rewrite name regex kuadrant-active-groups\.(.*)k.example.com kuadrant-active-groups.hcpapps.net
    forward kuadrant-active-groups.hcpapps.net /etc/resolv.conf

    # ... other plugins ...
    ready
    kuadrant
}
```

Replace:
- `k.example.com` with your actual zone domain
- `kuadrant-active-groups.hcpapps.net` with your external TXT record host

**After modifying the Corefile, restart CoreDNS**:
```bash
kubectl -n kuadrant-coredns rollout restart deployment kuadrant-coredns
```

**Customizing External Groups Host** (local setup):

When setting up the local environment, you can customize the external groups host:

```bash
make local-setup EXTERNAL_GROUPS_HOST=<your-external-host>
```

Default: `kuadrant-active-groups.hcpapps.net`

#### Step 3: Create External Active Groups TXT Record

**Recommended: Use the DNS CLI** (requires `make build-cli`):

```bash
# Build the CLI (run once)
make build-cli

# Add active group to external DNS provider
kubectl-kuadrant_dns add-active-group primary-us-east \
  --providerRef <namespace>/<secret-name> \
  --domain <external-domain>

# Add another group to the same domain
kubectl-kuadrant_dns add-active-group primary-eu-west \
  --providerRef <namespace>/<secret-name> \
  --domain <external-domain>
```

Where:
- `<secret-name>` is your provider secret (AWS, GCP, or Azure)
- `<external-domain>` is the domain hosting the TXT record (e.g., `hcpapps.net`)

The CLI automatically:
- Creates the TXT record in the correct format (`version=1;groups=...`)
- Adds groups to existing records (doesn't overwrite)
- Handles multiple zones if they exist
- Prompts for confirmation before making changes

**Verify the groups**:

```bash
kubectl-kuadrant_dns get-active-groups \
  --providerRef <namespace>/<secret-name> \
  --domain <external-domain>
```

<details>
<summary>Alternative: Using cloud provider CLIs directly</summary>

**TXT Record Format**:
```
version=1;groups=GROUP1&&GROUP2
```

**Example for AWS Route53**:

```bash
aws route53 change-resource-record-sets --hosted-zone-id <zone-id> --change-batch '{
  "Changes": [{
    "Action": "UPSERT",
    "ResourceRecordSet": {
      "Name": "kuadrant-active-groups.hcpapps.net",
      "Type": "TXT",
      "TTL": 60,
      "ResourceRecords": [{"Value": "\"version=1;groups=primary-us-east&&primary-eu-west\""}]
    }
  }]
}'
```

**Format Details**:
- `version=1` - Protocol version
- `groups=` - List of active groups separated by `&&`
- Groups listed here will actively reconcile DNS records
- Groups not listed will be passive (not reconciling)

</details>

### Result

#### Verify Groups Resolution

Once configured, verify CoreDNS can resolve the active groups TXT record:

```bash
# Get CoreDNS nameserver IP
NS="$(kubectl get secrets -n dnstest dns-provider-credentials-coredns \
  -o jsonpath='{.data.NAMESERVERS}' | base64 -d | cut -f1 -d':')"

# Query the active groups TXT record
dig @${NS} kuadrant-active-groups.k.example.com TXT +short
```

**Expected output**:
```
"version=1;groups=primary-us-east&&primary-eu-west"
```

#### Verify Controller Group Assignment

Check the DNS operator logs to see the current group and active groups:

```bash
kubectl logs -n dns-operator-system deployment/dns-operator-controller-manager | \
  grep -E "currentGroup|activeGroups"
```

**Expected log output**:
```json
{
  "currentGroup": "primary-us-east",
  "activeGroups": ["primary-us-east", "primary-eu-west"],
  "msg": "Processing DNSRecord"
}
```

#### Additional Resources

**For procedural guides:**
- [Exercising DNS Failover via Groups](../exercising_dns_failover_via_groups.md) - Step-by-step failover exercise and use cases
- [Migrating Existing Clusters to Use Groups](../migrating_existing_clusters_to_use_groups.md) - Migration from non-groups setup

---

## GeoIP Database Configuration

### What

GeoIP databases enable geographic routing by mapping client IP addresses to geographic locations. The Kuadrant CoreDNS plugin uses this data to return region-specific DNS responses.

**Key Concepts**:
- **Database Format**: `.mmdb` (MaxMind database format)
- **Update Frequency**: Monthly updates recommended for current GeoIP data
- **Demo Database**: Embedded in the image with localhost/Kind cluster subnets
- **MaxMind Database**: Comprehensive global IP-to-location mapping
- **EDNS Client Subnet**: The CoreDNS Service uses `externalTrafficPolicy: Cluster` (the default), which means kube-proxy replaces the original client IP with an internal cluster IP. Without correction, CoreDNS would geo-locate based on the cluster node IP rather than the client's actual location. The `geoip` plugin's `edns-subnet` directive solves this by reading the [EDNS Client Subnet (ECS)](https://datatracker.ietf.org/doc/html/rfc7871) option that recursive resolvers include in DNS queries. Most major resolvers (Google Public DNS, Cloudflare, OpenDNS, ISP resolvers) send ECS, so GeoIP routing works correctly for the vast majority of traffic. Queries from resolvers that omit ECS fall back to geo-locating based on the cluster node IP.

### How

#### Using the Embedded Demo Database

The Kuadrant CoreDNS image includes a demo database (`GeoLite2-City-demo.mmdb`) with:
- Localhost and Kind cluster subnets mapped to IE and US locales
- No additional setup required

**Corefile Configuration**:
```corefile
k.example.com {
    geoip GeoLite2-City-demo.mmdb {
        edns-subnet
    }
    metadata
    kuadrant
}
```

**Demo Database Mappings**: See the [Local Development Guide GEO section](local-development.md#geo) for complete subnet-to-locale mappings.

#### Using MaxMind GeoIP Database

For global IP-to-location mapping:
- MaxMind GeoLite2 (free tier) or commercial database
- Free tier: https://dev.maxmind.com/geoip/geolite2-free-geolocation-data
- Requires MaxMind account and license key

**Step 1: Obtain Database**:
- Sign up for MaxMind account
- Download GeoLite2-City database (`.mmdb` format)

**Step 2: Create ConfigMap**:
```bash
kubectl create configmap geoip-db \
  --from-file=GeoLite2-City.mmdb \
  -n kuadrant-coredns
```

**Step 3: Mount in CoreDNS Deployment**:

Update deployment to mount the database:
```yaml
volumeMounts:
- name: geoip-db
  mountPath: /etc/geoip
volumes:
- name: geoip-db
  configMap:
    name: geoip-db
```

**Step 4: Update Corefile**:
```corefile
k.example.com {
    geoip /etc/geoip/GeoLite2-City.mmdb {
        edns-subnet
    }
    metadata
    kuadrant
}
```

**Database Updates**:

MaxMind releases monthly database updates. Set up automated updates:

```bash
# Download new database
wget https://download.maxmind.com/app/geoip_download?...

# Update ConfigMap
kubectl create configmap geoip-db \
  --from-file=GeoLite2-City.mmdb \
  -n kuadrant-coredns \
  --dry-run=client -o yaml | kubectl apply -f -

# Restart CoreDNS
kubectl rollout restart deployment/kuadrant-coredns -n kuadrant-coredns
```

### Result

#### Testing Geographic Routing

**When running CoreDNS locally** (from terminal with `make run`):

```bash
# Query from IE locale
dig @127.0.0.1 api.k.example.com -p 1053 +subnet=127.0.100.0/24

# Query from US locale
dig @127.0.0.1 api.k.example.com -p 1053 +subnet=127.0.200.0/24
```

**When running CoreDNS in Kind cluster**:

```bash
# Get CoreDNS IP
NS1="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

# Simulate query from Ireland
dig @${NS1} api.k.example.com +subnet=127.0.100.0/24

# Simulate query from United States
dig @${NS1} api.k.example.com +subnet=127.0.200.0/24
```

**Expected Behavior**: Queries with different geographic contexts return different IP addresses based on the geo-code configured in DNSRecord endpoints.

---

## Advanced Routing Strategies

### What

The Kuadrant CoreDNS plugin supports advanced routing strategies to distribute traffic across endpoints based on geographic location and weighted distribution.

**Routing Types**:
- **Geographic (Geo) Routing**: Routes queries to geographically appropriate endpoints
- **Weighted Routing**: Distributes traffic across endpoints based on assigned weights
- **Combined Routing**: First applies GEO filter, then weighted selection within the matched region

### How

#### Geographic Routing

**Configuration**: Set `geo-code` in DNSRecord `providerSpecific` fields.

**Example DNSRecord**:
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: geo-example
  labels:
    kuadrant.io/coredns-zone-name: k.example.com
spec:
  rootHost: api.k.example.com
  endpoints:
    - dnsName: api.k.example.com
      recordType: A
      providerSpecific:
        - name: geo-code
          value: GEO-EU
      recordTTL: 300
      setIdentifier: GEO-EU
      targets:
        - 1.1.1.1
    - dnsName: api.k.example.com
      recordType: A
      providerSpecific:
        - name: geo-code
          value: GEO-US
      recordTTL: 300
      setIdentifier: GEO-US
      targets:
        - 2.2.2.2
```

**Required Corefile Configuration**:
```corefile
k.example.com {
    geoip GeoLite2-City-demo.mmdb {
        edns-subnet
    }
    metadata
    kuadrant
}
```

#### Weighted Routing

**How It Works**:
- Distributes traffic across endpoints based on assigned weights
- Uses weighted random selection algorithm
- Useful for canary deployments and gradual rollouts

**Algorithm**: The plugin builds a list of available records and applies a weighting algorithm. Selection is based on a random number between 0 and the sum of all weights.

**Configuration**: Set `weight` in DNSRecord `providerSpecific` fields.

**Example DNSRecord**:
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: weighted-example
  labels:
    kuadrant.io/coredns-zone-name: k.example.com
spec:
  rootHost: api.k.example.com
  endpoints:
    - dnsName: api.k.example.com
      recordType: A
      recordTTL: 60
      providerSpecific:
        - name: weight
          value: "70"
      setIdentifier: cluster-1
      targets:
        - 1.1.1.1
    - dnsName: api.k.example.com
      recordType: A
      recordTTL: 60
      providerSpecific:
        - name: weight
          value: "30"
      setIdentifier: cluster-2
      targets:
        - 2.2.2.2
```

**Behavior**: With the above configuration, approximately 70% of queries return `1.1.1.1` and 30% return `2.2.2.2`.

#### Combined Routing

**How It Works**:
1. First applies GEO filter to select location-appropriate endpoints
2. Then applies weighting algorithm if multiple endpoints exist in that GEO

**Use Case**: Multiple clusters in the same geographic region with different capacity.

### Result

#### Testing Geo Routing

```bash
# Get CoreDNS IP
NS="$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')"

# Simulate query from IE (using demo database)
dig @${NS} api.k.example.com +subnet=127.0.100.0/24 +short

# Simulate query from US
dig @${NS} api.k.example.com +subnet=127.0.200.0/24 +short
```

**Expected output**:
```
# From IE subnet
1.1.1.1

# From US subnet
2.2.2.2
```

Different geographic contexts return different IP addresses based on geo-code configuration.

### Additional Resources

For comprehensive technical details and examples, see:
- **[Kuadrant CoreDNS Plugin Documentation](../../coredns/plugin/README.md)** - Complete routing algorithm details and examples
- **[CoreDNS Official Documentation](https://coredns.io/manual/toc/)** - General CoreDNS plugin options

---

## Monitoring and Observability

### What

CoreDNS metrics enable monitoring of DNS query performance, error rates, and operational health. The Prometheus plugin exposes metrics for collection by Prometheus/Grafana.

**Key Metrics**:
- Query rate (queries per second)
- Query latency (response time distribution)
- Error rate (NXDOMAIN, SERVFAIL, etc.)
- Cache hit ratio
- Plugin performance (per-plugin execution time)

### How

#### Enable Prometheus Metrics

Add the `prometheus` directive to the Corefile:

```corefile
k.example.com {
    # ... other plugins ...
    prometheus 0.0.0.0:9153
}
```

**Configuration Details**:
- Exposes metrics on port 9153
- Must be added to each zone block
- Metrics available at `/metrics` endpoint

#### ServiceMonitor Integration

**Default Configuration**: When deploying CoreDNS using the DNS Operator's kustomize configuration ([`config/coredns/kustomization.yaml`](../../config/coredns/kustomization.yaml)), Prometheus monitoring is **already enabled by default**:

```yaml
prometheus:
  service:
    enabled: true
  monitor:
    enabled: true
    namespace: kuadrant-coredns
```

This automatically creates:
- ServiceMonitor resource for Prometheus Operator scraping
- Metrics service on port 9153

**Customizing Configuration**:

If you need to modify the default settings (e.g., change namespace, add labels, or disable monitoring), edit [`config/coredns/kustomization.yaml`](../../config/coredns/kustomization.yaml) in the `valuesInline` section.

#### Grafana Dashboards

**Development Setup**:
```bash
make install-observability
```

Access dashboards:
```bash
kubectl -n monitoring port-forward service/grafana 3000:3000
```

Open http://127.0.0.1:3000

**Default Credentials**: admin/admin

### Result

**Verify Metrics Endpoint**:

```bash
# Get CoreDNS pod
COREDNS_POD="$(kubectl get pods -n kuadrant-coredns \
  -l app.kubernetes.io/name=coredns \
  -o jsonpath='{.items[0].metadata.name}')"

# Port-forward to metrics endpoint
kubectl port-forward -n kuadrant-coredns ${COREDNS_POD} 9153:9153

# Query metrics
curl http://localhost:9153/metrics
```

**Expected output** (sample):
```
# HELP coredns_dns_request_duration_seconds Histogram of the time (in seconds) each request took.
# TYPE coredns_dns_request_duration_seconds histogram
coredns_dns_request_duration_seconds_bucket{server="dns://:53",zone="k.example.com.",le="0.00025"} 45
coredns_dns_request_duration_seconds_bucket{server="dns://:53",zone="k.example.com.",le="0.0005"} 87
...

# HELP coredns_dns_requests_total Counter of DNS requests made per zone, protocol and family.
# TYPE coredns_dns_requests_total counter
coredns_dns_requests_total{family="1",proto="udp",server="dns://:53",zone="k.example.com."} 342
```

**Grafana Dashboards** will visualize:
- Query rate over time
- Latency percentiles (p50, p95, p99)
- Error rate trends
- Cache effectiveness

---

## Logging Configuration

### What

CoreDNS logging provides visibility into DNS query processing, errors, and plugin execution. Logs are output to stdout/stderr for container log aggregation.

**Log Directives**:
- `debug` - Most verbose, includes all plugin execution details
- `errors` - Errors and failures only
- `log` - Query logging, logs each DNS query received

### How

Enable logging directives in the Corefile:

```corefile
k.example.com {
    debug      # Enable debug logging
    errors     # Log errors
    log        # Enable query logging
    # ... other plugins ...
}
```

### Result

#### Viewing Logs

```bash
# Stream CoreDNS logs
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-coredns

# Get recent logs
kubectl logs --tail=100 deployments/kuadrant-coredns -n kuadrant-coredns
```

**Expected output** (sample with query logging):
```
[INFO] plugin/reload: Running configuration SHA512 = ...
[INFO] 127.0.0.1:54321 - 12345 "A IN api.k.example.com. udp 35 false 512" NOERROR qr,aa,rd 98 0.001234567s
[INFO] 127.0.0.1:54322 - 12346 "NS IN k.example.com. udp 30 false 512" NOERROR qr,aa,rd 156 0.000987654s
```

**Log Integration**: Logs are structured for aggregation by cluster logging solutions (FluentD, Loki, etc.).

---

## Additional Configuration Topics

### Parent Zone Delegation Setup

For step-by-step instructions on setting up parent zone delegation with CoreDNS:
- **Local Development**: BIND9 setup for testing delegation scenarios
- **Dynamic DNS Updates**: Using nsupdate for parent zone configuration
- **In-Cluster DNS Forwarding**: Enabling pod-based DNS resolution
- **DNS Groups with BIND9**: Active-groups TXT record resolution variant

See **[Zone Delegation Guide](zone-delegation.md)** for comprehensive instructions.

### Custom Corefile Modifications

For general CoreDNS plugin options not specific to Kuadrant integration:
- **Caching**: Configure DNS response caching
- **Forwarding**: Forward queries to upstream DNS servers
- **Rewrite Rules**: Modify queries or responses
- **Secondary Zones**: Configure zone transfers

See **[CoreDNS Official Documentation](https://coredns.io/manual/toc/)** for details.

### Multi-Cluster Configuration

For multi-cluster deployments using delegation:
- **Primary Clusters**: Require default provider secret and multicluster kubeconfig secrets
- **Secondary Clusters**: Can optionally configure provider secrets for non-delegating DNSRecords

See **[DNS Record Delegation](../dns_record_delegation.md)** for complete multi-cluster configuration details.
