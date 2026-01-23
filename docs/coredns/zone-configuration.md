# CoreDNS Zone Configuration

## Overview

When using the Kuadrant CoreDNS plugin, you need to configure the zone's authoritative nameserver records to ensure proper DNS delegation and redundancy. For each zone configured with the `kuadrant` directive in the Corefile, the plugin initializes the zone with a single hardcoded NS record (`ns1.<zone-name>`, e.g., `ns1.k.example.com`), but this is insufficient for production use.

A properly configured zone requires:
- **Multiple NS records** for redundancy (IANA requirement: minimum 2 nameservers)
- **Corresponding A records** mapping nameserver hostnames to their IP addresses
- **Consistency** with parent zone delegation configuration

Without proper zone configuration, DNS queries for nameserver records will fail, and the zone will have a single point of failure.

## Configuration via DNSRecord

The recommended approach is to use DNSRecord resources to configure zone apex NS and A records. This leverages the NS record type support in the DNS operator and provides maximum flexibility for updates.

### Single Cluster Setup

> **⚠️ WARNING: Not for Production Use**
>
> A single cluster deployment creates only ONE CoreDNS instance, which means you cannot achieve true redundancy even with multiple NS records pointing to the same IP address. This configuration is suitable for:
> - Development and testing environments
> - Local Kind clusters
> - Learning and experimentation
>
> **For production environments, use the multi-cluster setup described below.**

For development/testing with a single CoreDNS instance:

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

**Limitations of Single Cluster:**
- If the single CoreDNS instance fails, the entire zone becomes unavailable
- Both nameservers resolve to the same IP address
- Does not meet production reliability requirements
- Not compliant with best practices for authoritative DNS

### Multi-Cluster Setup (Production)

For production environments, deploy CoreDNS across multiple clusters with delegation enabled. Each primary cluster runs its own CoreDNS instance, and the NS record merging logic combines nameserver records from all clusters into the authoritative zone.

#### Architecture

- **Multiple Primary Clusters**: Each runs a CoreDNS instance with the Kuadrant plugin
- **Delegation Mode**: Each cluster creates delegated DNSRecord resources
- **Automatic Merging**: The operator merges NS records from all clusters
- **True Redundancy**: Each nameserver has a unique IP from a different cluster

#### Configuration

**Cluster 1 (Primary):**
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

**Cluster 2 (Primary):**
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

**Optional Cluster 3+ (Primary):**

For additional redundancy, add more clusters following the same pattern:

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
  - dnsName: k.example.com
    recordTTL: 60
    recordType: NS
    targets:
    - ns3.k.example.com
  - dnsName: ns3.k.example.com
    recordTTL: 60
    recordType: A
    targets:
    - 172.18.0.19  # Cluster 3 CoreDNS LoadBalancer IP
```

#### Result

The DNS operator merges these delegated records into a single authoritative zone containing:
```
k.example.com.          60  IN  NS  ns1.k.example.com.
k.example.com.          60  IN  NS  ns2.k.example.com.
k.example.com.          60  IN  NS  ns3.k.example.com.
ns1.k.example.com.      60  IN  A   172.18.0.17
ns2.k.example.com.      60  IN  A   172.18.0.18
ns3.k.example.com.      60  IN  A   172.18.0.19
```

This provides true redundancy: if any single CoreDNS instance fails, the other nameservers continue serving the zone.

## Determining CoreDNS Instance IPs

### Kind Clusters

For local Kind clusters, retrieve the LoadBalancer IP:

**Single cluster:**
```bash
kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}'
```

**Multi-cluster setup:**
```bash
# Cluster 1
NS1=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-1 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Cluster 1 CoreDNS IP: $NS1"

# Cluster 2
NS2=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-2 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Cluster 2 CoreDNS IP: $NS2"

# Cluster 3 (if present)
NS3=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-3 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
echo "Cluster 3 CoreDNS IP: $NS3"
```

### Production Deployments

For production Kubernetes environments:

**Get LoadBalancer IP or hostname:**
```bash
# Most cloud providers (GCP, Azure, DigitalOcean) assign an IP
kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}'

# AWS ELB/NLB typically provides a hostname instead of IP
kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].hostname}'
```

> **Note on AWS LoadBalancers**: AWS ELB/NLB services typically expose a DNS hostname (e.g., `abc123.elb.us-east-1.amazonaws.com`) rather than a static IP address. If using AWS, you'll need to resolve this hostname to get the actual IP addresses for your DNSRecord A records:
> ```bash
> LB_HOSTNAME=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns -o jsonpath='{.status.loadBalancer.ingress[0].hostname}')
> dig +short $LB_HOSTNAME
> ```
> Use the resolved IP addresses in your zone configuration DNSRecord. Note that AWS ELB IPs can change, so consider using AWS NLB with static Elastic IPs for production authoritative nameservers.

**Important Considerations:**
- **LoadBalancer IPs**: The CoreDNS service LoadBalancer IP is typically assigned automatically by your cloud provider and should remain stable across pod restarts
- **Public accessibility**: Ensure the LoadBalancer service is publicly accessible (not internal-only) so external DNS clients can reach the nameservers
- **IP documentation**: Record the LoadBalancer IPs and their corresponding cluster associations for operational reference
- **IP changes**: If the LoadBalancer service is deleted and recreated, the IP may change, requiring updates to both the zone config DNSRecord and parent zone delegation

## Parent Zone Configuration

After configuring the CoreDNS zone, you must update the parent zone to delegate authority for your subdomain. The parent zone needs NS records and corresponding glue records (A records).

### Example Parent Zone Configuration

If your CoreDNS manages `k.example.com` and the parent zone is `example.com`:

**For BIND9 (example.com zone file):**
```zone
; Delegation for k.example.com to CoreDNS instances
k.example.com.          300  IN  NS  ns1.k.example.com.
k.example.com.          300  IN  NS  ns2.k.example.com.

; Glue records (required because nameservers are within delegated zone)
ns1.k.example.com.      300  IN  A   172.18.0.17
ns2.k.example.com.      300  IN  A   172.18.0.18
```

**For dynamic updates (BIND9 with nsupdate):**
```bash
EDGE_NS=<parent-nameserver-ip>
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

**For cloud DNS providers (AWS Route53, Google Cloud DNS, Azure DNS):**

For supported cloud DNS providers, use the DNS Operator to manage parent zone delegation records via DNSRecord resources. This provides a consistent, declarative approach across all DNS infrastructure.

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

The DNSRecord structure is identical for AWS Route53, Google Cloud DNS, and Azure DNS. Only the provider credentials secret differs (type `kuadrant.io/aws`, `kuadrant.io/gcp`, or `kuadrant.io/azure`).

**For other DNS providers:**

For DNS providers not directly supported by the DNS Operator, use the provider's available management tools to create the delegation records:
- **Web Console/GUI**: Most DNS providers offer web-based management interfaces
- **CLI Tools**: Use provider-specific command-line tools (e.g., `aws route53`, `gcloud dns`, `az network dns`)
- **Terraform/IaC**: Use infrastructure-as-code tools to manage DNS records
- **API**: Use the provider's REST API for programmatic management

Regardless of the method, ensure you create:
1. NS records pointing `k.example.com` to `ns1.k.example.com` and `ns2.k.example.com`
2. A records (glue records) mapping nameserver hostnames to their IP addresses

### Glue Records

Glue records are **required** when nameserver hostnames are within the delegated zone (e.g., `ns1.k.example.com` for zone `k.example.com`). Without glue records, a circular dependency occurs:
1. Resolver tries to find `k.example.com` nameservers
2. Parent says "ask `ns1.k.example.com`"
3. To find `ns1.k.example.com`, resolver needs to query `k.example.com`
4. Circular dependency - resolution fails

The A records in the parent zone break this cycle by providing the IP addresses directly.

## Verification

### Verify Zone Transfer Shows All NS Records

```bash
# Get CoreDNS IP
NS1=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Request full zone transfer
dig @${NS1} -t AXFR k.example.com
```

**Expected output should include:**
```
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60
k.example.com.          60      IN      NS      ns1.k.example.com.
k.example.com.          60      IN      NS      ns2.k.example.com.
ns1.k.example.com.      60      IN      A       172.18.0.17
ns2.k.example.com.      60      IN      A       172.18.0.18
```

### Verify Nameserver A Records Resolve

```bash
# Verify each nameserver resolves
dig @${NS1} ns1.k.example.com +short
# Expected: 172.18.0.17

dig @${NS1} ns2.k.example.com +short
# Expected: 172.18.0.18
```

### Verify NS Records from CoreDNS

```bash
# Query for NS records
dig @${NS1} k.example.com NS +short
```

**Expected output:**
```
ns1.k.example.com.
ns2.k.example.com.
```

### Verify Delegation from Parent Zone

```bash
# Query parent zone's nameserver for delegation
dig @<parent-nameserver-ip> k.example.com NS +short
```

**Expected output:**
```
ns1.k.example.com.
ns2.k.example.com.
```

### Verify Glue Records from Parent Zone

```bash
# Query parent zone for glue records
dig @<parent-nameserver-ip> ns1.k.example.com A +short
dig @<parent-nameserver-ip> ns2.k.example.com A +short
```

### End-to-End Resolution Test

```bash
# Test resolution through public DNS (uses parent delegation)
dig k.example.com NS
dig ns1.k.example.com A
dig ns2.k.example.com A

# Test actual record resolution
dig simple.k.example.com A
```

### Multi-Cluster Verification

For multi-cluster deployments, verify all CoreDNS instances have the same zone data:

```bash
# Get all CoreDNS IPs
NS1=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-1 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')
NS2=$(kubectl get service/kuadrant-coredns -n kuadrant-coredns \
  --context kind-kuadrant-dns-local-2 \
  -o jsonpath='{.status.loadBalancer.ingress[0].ip}')

# Verify both have identical NS records
echo "=== Cluster 1 NS Records ==="
dig @${NS1} k.example.com NS +short | sort

echo "=== Cluster 2 NS Records ==="
dig @${NS2} k.example.com NS +short | sort

# They should be identical
```

## Updating Nameserver IPs

If CoreDNS instance IPs change (e.g., due to cluster recreation or service reconfiguration), follow these steps:

### 1. Update DNSRecord Resource

Edit the DNSRecord to reflect new IP addresses:

```bash
kubectl edit dnsrecord dnsrecord-k-example-com-zone-config -n kuadrant-coredns
```

Update the targets in the A record endpoints:

```yaml
  - dnsName: ns1.k.example.com
    recordTTL: 60
    recordType: A
    targets:
    - 192.168.1.100  # New IP
```

### 2. Update Parent Zone Delegation

Update the glue records in the parent zone to match the new IPs. The exact method depends on your parent zone provider (see [Parent Zone Configuration](#parent-zone-configuration) section).

### 3. Wait for TTL Expiration

DNS changes are subject to TTL (Time To Live) caching:
- **Zone config TTL**: Default 60 seconds (as shown in examples)
- **Parent zone TTL**: Typically 300 seconds (5 minutes) or higher
- **Recursive resolver caching**: Varies by resolver

Allow sufficient time for all caches to expire before expecting consistent resolution. A good rule of thumb is to wait:
```
Wait Time = MAX(zone_config_ttl, parent_delegation_ttl) + 5 minutes
```

### 4. Verify Updates

After the TTL period:

```bash
# Verify new IP is in CoreDNS zone
dig @<new-coredns-ip> ns1.k.example.com +short
# Expected: 192.168.1.100

# Verify parent zone has new glue records
dig @<parent-nameserver-ip> ns1.k.example.com +short
# Expected: 192.168.1.100

# Test end-to-end resolution
dig ns1.k.example.com +short
# Expected: 192.168.1.100
```

### 5. Monitor for Issues

Watch CoreDNS logs and DNS query metrics during the transition:

```bash
kubectl logs -f deployment/kuadrant-coredns -n kuadrant-coredns
```

### Best Practices for IP Changes

- **Plan ahead**: Use static/reserved IPs when possible to avoid changes
- **Staged rollout**: Update one cluster at a time in multi-cluster deployments
- **Lower TTLs beforehand**: If planning IP changes, temporarily reduce TTLs 24 hours in advance
- **Document changes**: Keep records of IP assignments and change history
- **Test thoroughly**: Verify resolution from multiple geographic locations

## Security Considerations

**Critical: Zone configuration DNSRecords have full control over the zone's nameserver records.** Anyone who can create or modify DNSRecords targeting the zone apex with NS records can effectively control the entire zone's DNS resolution.

### Access Control

Implement strict RBAC controls to protect zone configuration:

**1. Use a dedicated namespace for zone configuration:**
```yaml
apiVersion: v1
kind: Namespace
metadata:
  name: dns-config  # Separate from application namespaces
```

**2. Restrict DNSRecord creation in the zone config namespace:**
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

**3. Prevent regular users from accessing the zone config namespace:**
- Do NOT grant cluster-wide DNSRecord creation permissions
- Application teams should only have DNSRecord permissions in their own namespaces
- Use namespace-scoped roles, not cluster roles, for DNS management

**4. Enable audit logging:**

Monitor all changes to zone configuration DNSRecords:
```bash
# View audit logs for DNSRecord changes in zone config namespace
kubectl get events -n kuadrant-coredns --field-selector involvedObject.kind=DNSRecord
```

Consider enabling Kubernetes audit logging to track all API operations on DNSRecords.

### Attack Scenarios

**Zone Hijacking:**
- An attacker with DNSRecord creation permissions could replace NS records with malicious nameservers
- All DNS queries for the zone would be redirected to attacker-controlled servers
- **Mitigation**: Strict RBAC limiting who can create/modify zone config DNSRecords

**Denial of Service:**
- Invalid or non-responsive NS records could break DNS resolution for the entire zone
- Removing all NS records would make the zone unresolvable
- **Mitigation**: Use namespace isolation and RBAC; consider implementing admission webhooks to validate NS record changes

**DNS Poisoning:**
- Malicious NS records could direct queries to servers that return false DNS data
- Users could be redirected to phishing sites or malicious services
- **Mitigation**: Protect zone config DNSRecords with RBAC; enable audit logging to detect unauthorized changes

### Best Practices

1. **Separate namespaces**: Keep zone configuration DNSRecords in a dedicated, restricted namespace separate from application DNSRecords
2. **Principle of least privilege**: Only grant DNSRecord permissions to users who absolutely need them
3. **Multi-cluster delegation**: In delegating setups, each cluster should only manage its own nameserver (e.g., cluster1 manages `ns1.k.example.com`, cluster2 manages `ns2.k.example.com`)
4. **Audit and monitor**: Enable audit logging and monitor for unexpected changes to zone configuration DNSRecords
5. **Version control**: Store zone configuration DNSRecord YAML in Git with review processes before applying changes
6. **Admission control**: Consider implementing ValidatingWebhookConfiguration to enforce policies on NS record creation/modification
7. **Regular reviews**: Periodically audit who has access to create/modify zone configuration DNSRecords

### Multi-Tenant Environments

In multi-tenant clusters, zone configuration becomes even more critical:

- Create dedicated namespaces per tenant for their application DNSRecords
- Zone configuration DNSRecords should be in a cluster-admin-only namespace
- Use ResourceQuotas and LimitRanges to prevent resource exhaustion
- Consider using separate clusters for different trust boundaries

## Troubleshooting

### Common Issues

**Issue: NS queries return only ns1.k.example.com (single NS record)**

*Cause*: Zone configuration DNSRecord not created or not applied

*Solution*: Create and apply the zone configuration DNSRecord as shown above

---

**Issue: NXDOMAIN when querying nameserver A records**

*Cause*: A records for nameservers not configured in zone config

*Solution*: Ensure DNSRecord includes A record endpoints for all nameservers

---

**Issue: Parent zone delegation not working**

*Cause*: Missing or incorrect glue records in parent zone

*Solution*: Verify parent zone has both NS and A records (see [Parent Zone Configuration](#parent-zone-configuration))

---

**Issue: Multi-cluster shows different NS records on different CoreDNS instances**

*Cause*: Delegation not properly configured or multi-cluster sync issues

*Solution*:
- Verify all clusters have `delegate: true` in their DNSRecord specs
- Check multi-cluster connection secrets exist
- Review DNS operator logs for synchronization errors

---

**Issue: Changes not appearing in zone**

*Cause*: DNS operator not reconciling DNSRecord or CoreDNS not reloading

*Solution*:
```bash
# Check DNSRecord status
kubectl get dnsrecord dnsrecord-k-example-com-zone-config -n kuadrant-coredns -o yaml

# Check DNS operator logs
kubectl logs -f deployment/dns-operator-controller-manager -n dns-operator-system

# Check CoreDNS logs
kubectl logs -f deployment/kuadrant-coredns -n kuadrant-coredns
```

## Related Documentation

- [CoreDNS Integration Overview](README.md)
- [Configure Edge Server](configure-edge-server.md)
- [DNS Record Delegation](../dns_record_delegation.md)
- [Multi-cluster Setup Guide](README.md#setup-multi-cluster-local-environment-kind)
