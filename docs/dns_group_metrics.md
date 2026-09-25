# DNS Group Metrics

The dns-operator exposes Prometheus metrics for monitoring DNS group assignments and failover operations. These metrics are available when the dns-operator is running and groups are configured.

## Available Metrics

### dns_record_group_info

| Field | Value |
|---|---|
| Type | Gauge |
| Description | Reports the group assignment for each DNSRecord. Always emits a value of 1; the group is identified by the `group` label. |
| Labels | `dns_record_name`, `dns_record_namespace`, `group` |

### dns_record_group_active

| Field | Value |
|---|---|
| Type | Gauge |
| Description | Reports whether each DNSRecord is in an active group (1) or inactive group (0). Based on the `Active` status condition of the DNSRecord. Also emitted for ungrouped records (with an empty `group` label) — use `{group!=""}` to filter these out. |
| Labels | `dns_record_name`, `dns_record_namespace`, `group` |

### dns_record_inactive_group_cleanup_total

| Field | Value |
|---|---|
| Type | Counter |
| Description | Counts cleanup operations that remove DNS records or TXT registry entries belonging to inactive groups. Incremented each time an active controller cleans up records from an inactive group. |
| Labels | `dns_record_name`, `dns_record_namespace`, `group` |

## Example Queries

### List all DNSRecords and their group assignments

```promql
dns_record_group_info
```

### Show only inactive DNSRecords

```promql
dns_record_group_active{group!=""} == 0
```

### Show only active DNSRecords

```promql
dns_record_group_active{group!=""} == 1
```

### Count DNSRecords per group

```promql
count by (group) (dns_record_group_info)
```

### Count active records per group

```promql
count by (group) (dns_record_group_active{group!=""} == 1)
```

### Count inactive records per group

```promql
count by (group) (dns_record_group_active{group!=""} == 0)
```

### Rate of inactive group cleanup operations (per minute)

```promql
rate(dns_record_inactive_group_cleanup_total[5m]) * 60
```

### Alert: inactive group cleanup is occurring

This can indicate that a failover has been triggered and active controllers are cleaning up records from the now-inactive group.

```promql
increase(dns_record_inactive_group_cleanup_total[10m]) > 0
```

### Alert: unexpected inactive group cleanup activity

Sustained cleanup activity may indicate that a group was deactivated unintentionally or that an operator is stuck cycling between active and inactive states:

```yaml
apiVersion: monitoring.coreos.com/v1
kind: PrometheusRule
metadata:
  name: dns-group-alerts
spec:
  groups:
    - name: dns-groups
      rules:
        - alert: DNSInactiveGroupCleanupSustained
          expr: rate(dns_record_inactive_group_cleanup_total[5m]) > 0
          for: 30m
          labels:
            severity: warning
          annotations:
            summary: "Sustained inactive group cleanup for {{ $labels.dns_record_name }} in group {{ $labels.group }}"
            description: "Continuous cleanup operations for an inactive group over 30 minutes may indicate a misconfigured group or an operator cycling between states."
```

> **Note:** Records in a standby (inactive) group are expected — `dns_record_group_active == 0` alone is not an error condition. Alert on cleanup activity or unexpected group transitions rather than on inactive state directly.

## Grafana Dashboard Tips

- Use a **stat panel** with `count by (group) (dns_record_group_info)` to show the number of records per group.
- Use a **table panel** with `dns_record_group_info * on(dns_record_name, dns_record_namespace) group_left dns_record_group_active` to show each record's group and active state together.
- Use a **time series panel** with `rate(dns_record_inactive_group_cleanup_total[5m])` to visualize cleanup activity over time, which correlates with failover events.
