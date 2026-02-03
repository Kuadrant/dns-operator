# BIND9 Configuration

This directory contains Kubernetes deployment manifests for BIND9, used as an example authoritative DNS server for testing zone delegation scenarios with CoreDNS.

## What is Included

- **`deployment.yaml`** - BIND9 pod deployment
- **`service.yaml`** - LoadBalancer service exposing BIND9 on port 53
- **`zone.yaml`** - ConfigMap with `example.com` zone configuration and TSIG key
- **`ddns.key`** - TSIG key file for authenticated dynamic DNS updates

## Quick Start

BIND9 is automatically installed as part of the local development setup:

```bash
make local-setup
```

Or install manually:

```bash
make install-bind9
```

This deploys BIND9 in the `kuadrant-bind9` namespace with:
- Service name: `kuadrant-bind9`
- LoadBalancer type for external access
- Pre-configured `example.com` zone
- TSIG key authentication for dynamic updates

## Comprehensive Guide

For complete setup instructions, zone delegation configuration, DNS Groups integration, and troubleshooting, see:

**[Zone Delegation Guide](../../docs/coredns/zone-delegation.md)**

This guide covers:
- Parent zone delegation pattern and concepts
- Step-by-step BIND9 and CoreDNS integration
- Dynamic DNS updates with nsupdate
- In-cluster DNS forwarding configuration
- DNS Groups active-passive failover setup
- Verification and troubleshooting

## TSIG Key

The `ddns.key` file contains a TSIG key for authenticated DNS updates:

```
key "example.com-key" {
    algorithm hmac-sha256;
    secret "...";
};
```

Use with `-k` flag in `nsupdate` and `dig` commands:
```bash
nsupdate -k config/bind9/ddns.key -v <update-file>
dig @<server> -k config/bind9/ddns.key -t AXFR example.com
```

To generate a new key:
```bash
ddns-confgen -k example.com-key -z example.com.
```

## Configuration Files

**Zone Configuration** (`zone.yaml`):
- Defines `example.com` zone
- Allows dynamic updates via TSIG key
- Permits zone transfers (AXFR)
- TTL: 30 seconds (for rapid testing)

**Service** (`service.yaml`):
- Type: LoadBalancer
- Ports: 53 (DNS) → 1053 (container)
- External access for DNS queries and updates

**Deployment** (`deployment.yaml`):
- Image: internetsystemsconsortium/bind9:9.18
- Container port: 1053
- Mounts zone configuration and DDNS key
- Namespace: kuadrant-bind9
