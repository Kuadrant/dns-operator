# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

DNS Operator is a Kubernetes controller for managing DNS records across multiple cloud providers (AWS Route53, Google Cloud DNS, Azure DNS) and clusters. It supports advanced routing strategies (Geo, Weighted) and health-aware DNS record publishing with multi-cluster coordination.

## Common Commands

### Development

```bash
# Run controller locally (foreground)
make run

# Run with specific delegation role
make run-primary    # Primary cluster mode (default)
make run-secondary  # Secondary cluster mode

# Build the binary
make build

# Format, vet, and lint code
make fmt
make vet
make lint

# Fix imports
make imports
```

### Testing

```bash
# Run all tests
make test

# Run unit tests only
make test-unit

# Run integration tests only
make test-integration

# Run E2E tests (requires cluster with DNS provider configured)
make test-e2e TEST_DNS_ZONE_DOMAIN_NAME=<domain> \
  TEST_DNS_PROVIDER_SECRET_NAME=<secret> \
  TEST_DNS_NAMESPACES=<namespace>

# Run E2E tests for multi-cluster scenarios
make test-e2e-multi

# Run E2E tests for single record scenarios
make test-e2e-single
```

### Local Development with Kind

```bash
# Create local kind cluster (default: 1 cluster)
make local-setup

# Create multiple clusters for multi-cluster testing
make local-setup CLUSTER_COUNT=2

# Deploy operator to existing kind cluster
make local-deploy

# Clean up all local clusters
make local-cleanup
```

### Kubernetes Operations

```bash
# Install CRDs
make install

# Deploy controller to cluster
make deploy

# View controller logs
kubectl logs -f deployments/dns-operator-controller-manager -n dns-operator-system

# Undeploy controller
make undeploy
```

### Code Generation

```bash
# Generate manifests (CRDs, RBAC, webhooks) after API changes
make manifests

# Generate DeepCopy methods after API changes
make generate
```

## Architecture Overview

### Core Components

**DNSRecord Controller** (`internal/controller/dnsrecord_controller.go`)
- Main reconciliation loop for DNSRecord CRDs
- Reconciliation steps:
  1. Validate DNSRecord
  2. Assign Owner ID (for multi-cluster ownership tracking)
  3. Find/Assign DNS Zone (matches rootHost to provider zones)
  4. Create DNS Provider (AWS/GCP/Azure/CoreDNS)
  5. Publish/Delete Records (via external-dns integration)
  6. Manage Health Checks
  7. Update Status Conditions
- Uses exponential backoff for reconciliation (min 5s, max 15m)
- Implements TXT registry for ownership tracking

**DNSHealthCheckProbe Controller** (`internal/controller/dnshealthcheckprobe_reconciler.go`)
- Performs health checks on DNS record endpoints
- Updates probe status based on success/failure thresholds
- Supports HTTP/HTTPS with configurable paths, ports, headers
- Can optionally ignore TLS certificate validation

**Remote DNSRecord Controller** (`internal/controller/remote_dnsrecord_controller.go`)
- Synchronizes DNSRecord status across multiple clusters
- Enables multi-cluster delegation feature

### API Types (`api/v1alpha1/`)

**DNSRecord** (`dnsrecord_types.go`)
- Primary CRD for managing DNS records
- Key fields:
  - `rootHost`: The domain name for the DNS record
  - `endpoints`: List of DNS endpoints (A, CNAME, etc.)
  - `providerRef`: Reference to DNS provider secret (mutually exclusive with `delegate`)
  - `delegate`: Enable multi-cluster delegation mode
  - `healthCheck`: Configuration for health checking
- Status tracks:
  - Zone information
  - DNS provider reference
  - Owner ID
  - Endpoint health
  - Ready/NotReady conditions

**DNSHealthCheckProbe** (`dnshealthcheckprobe_types.go`)
- CRD for health check configuration
- Supports:
  - Configurable protocol, port, path
  - Custom headers
  - Failure/success thresholds
  - Interval between checks

### Provider System (`internal/provider/`)

The operator uses an abstract provider interface supporting:
- AWS Route53
- Google Cloud DNS
- Azure DNS
- CoreDNS (for local/testing)
- In-memory (for testing)

Provider selection controlled by:
- `--provider` flag (comma-separated list)
- `PROVIDER` environment variable

Provider credentials stored in secrets with type:
- `kuadrant.io/aws`
- `kuadrant.io/gcp`
- `kuadrant.io/azure`

Default provider secret must have label: `kuadrant.io/default-provider=true`

### Multi-Cluster Delegation

**Key Concepts:**
- **Primary Cluster**: Reconciles delegated DNS records into authoritative DNS records. Requires default provider secret and multi-cluster connection secrets.
- **Secondary Cluster**: Validates and maintains status of delegated DNS records but doesn't interact with DNS provider. Can still reconcile non-delegated records.
- **Authoritative DNS Record**: Managed by dns-operator, consists of all delegated DNS records for a root host.

**Configuration:**
- Primary: `--delegation-role=primary` (default) or `DELEGATION_ROLE=primary`
- Secondary: `--delegation-role=secondary` or `DELEGATION_ROLE=secondary`
- Multi-cluster secrets labeled: `kuadrant.io/multicluster-kubeconfig=true`
- Use `kubectl-kuadrant_dns add-cluster-secret` to create connection secrets (obtain by `make build-cli`)

**Important Constraints:**
- `delegate` field is immutable (requires DNSRecord deletion/recreation to change)
- `delegate=true` and `providerRef` are mutually exclusive
- Multiple primary clusters must share same cluster connection secrets

### External DNS Integration (`internal/external-dns/`)

Code adapted from kubernetes-sigs/external-dns for:
- Zone discovery
- Record management
- TXT registry for ownership

This directory is expected to be merged back to external-dns upstream and removed.

### Health Checking System

Health checks influence DNS record publishing:
- Unhealthy endpoints can be removed from DNS
- Configurable probe intervals and thresholds
- SSL/TLS validation optional (`insecure-health-checks` flag)
- Probes can be disabled with `--enable-probes=false`

## Configuration Flags

Key controller flags (can also be set via environment variables):

| Flag | Env Var | Default | Description |
|------|---------|---------|-------------|
| `--metrics-bind-address` | `METRICS_BIND_ADDRESS` | `:8080` | Metrics endpoint |
| `--health-probe-bind-address` | `HEALTH_PROBE_BIND_ADDRESS` | `:8081` | Health probe endpoint |
| `--leader-elect` | `LEADER_ELECT` | `false` | Enable leader election |
| `--min-requeue-time` | `MIN_REQUEUE_TIME` | `5s` | Min timeout between provider calls |
| `--max-requeue-time` | `MAX_REQUEUE_TIME` | `15m` | Max time between reconciliations |
| `--valid-for` | `VALID_FOR` | `14m` | Duration record info is valid |
| `--provider` | `PROVIDER` | `aws,google,azure,coredns,endpoints` | DNS providers to enable |
| `--enable-probes` | `ENABLE_PROBES` | `true` | Enable health probes |
| `--insecure-health-checks` | `INSECURE_HEALTH_CHECKS` | `true` | Ignore cert validation |
| `--delegation-role` | `DELEGATION_ROLE` | `primary` | Delegation role |
| `--watch-namespaces` | `WATCH_NAMESPACES` | (empty) | Namespaces to watch |

## Code Layout

- `cmd/main.go` - Application entry point
- `api/v1alpha1/` - CRD type definitions
- `internal/controller/` - Controller reconciliation logic
  - `dnsrecord_controller.go` - Main DNSRecord controller
  - `dnshealthcheckprobe_reconciler.go` - Health probe controller
  - `remote_dnsrecord_controller.go` - Multi-cluster sync
  - `dnsrecord_healthchecks.go` - Health check logic
- `internal/provider/` - DNS provider abstractions
- `internal/external-dns/` - Forked external-dns code (temporary)
- `internal/common/` - Shared utilities
- `test/e2e/` - End-to-end tests
- `config/` - Kustomize deployment configurations
- `docs/` - Documentation

## Logging

Follow these guidelines when working with logs:

- `logger.Info()` - High-level resource state (creation, deletion, reconciliation path)
- `logger.Error()` - Errors not returned in reconciliation result (one error message only)
- `logger.V(1).Info()` - Debug logs (every change/event/update)

Use `--log-mode=development` flag to enable debug level logs.

Common log metadata:
- `DNSRecord` - Name/namespace of DNSRecord being reconciled
- `reconcileID` - Unique reconciliation ID
- `ownerID` - Owner of the DNS Record
- `zoneEndpoints` - Endpoints in provider
- `specEndpoints` - Endpoints in spec
- `statusEndpoints` - Previously processed endpoints

## Important Development Notes

**API Changes:**
- After modifying CRD types in `api/v1alpha1/`, run `make manifests generate` to regenerate code and manifests

**Testing Strategy:**
- Unit tests: `internal/controller/*_test.go` - Test controller logic in isolation
- Integration tests: Uses envtest for testing against Kubernetes API
- E2E tests: `test/e2e/` - Requires real DNS provider credentials and domain

**Provider Secrets:**
- AWS: Requires `AWS_ACCESS_KEY_ID`, `AWS_SECRET_ACCESS_KEY`, `AWS_REGION`
- GCP: Requires `GOOGLE` (JSON credentials), `PROJECT_ID`
- Azure: Requires `azure.json` with tenantId, subscriptionId, resourceGroup, aadClientId, aadClientSecret

**Zone Matching:**
- Operator lists available zones from provider
- Matches rootHost to zone domain
- If multiple zones match, operator won't update any (ambiguity protection)

**Ownership Tracking:**
- Uses TXT records in DNS provider for ownership tracking
- TXT prefix/suffix in logs indicate ownership record names
- Prevents conflicts when multiple operators manage same zone

**Reconciliation Flow:**
- Records marked valid for 14m by default
- Min requeue time: 5s (prevents API throttling)
- Max requeue time: 15m (ensures eventual consistency)
- Exponential backoff between min and max
