# kuadrant

## Name

*kuadrant* - enables serving zone data from kuadrant DNSRecord resources.

## Description

The *kuadrant* plugin enables CoreDNS to serve DNS records from Kubernetes DNSRecord custom resources, providing an alternative to cloud-based DNS services by allowing you to host DNS records in your own CoreDNS instances running in Kubernetes.

The plugin sets up watchers and listeners on DNSRecord resources in the Kubernetes cluster. As it discovers them, it processes and adds the endpoints to the appropriate DNS zone with GEO and weighted routing capabilities. The plugin uses logic from the [CoreDNS file plugin](https://github.com/coredns/coredns/tree/master/plugin/file) to create a functioning DNS server.

**Weighted Routing:** The plugin builds a list of all available records that could be provided as the answer to a given query from within the identified zone. It then applies a weighting algorithm to decide on a single response depending on the individual record weighting, using a random number between 0 and the sum of all weights. This provides probabilistic load distribution across endpoints.

**Geographic Routing:** GEO data is sourced from a geographical database such as MaxMind and made available with the [CoreDNS `geoip` plugin](https://coredns.io/plugins/geoip/), which must execute before the Kuadrant plugin. With this enabled, the plugin uses GEO data to decide which record to return based on the client's geographic location.

**Combined Routing:** When multiple endpoints exist within a single geographic region, the plugin first applies the GEO filter and then uses the weighting algorithm on the result, enabling both geographic distribution and load balancing within regions.

For a complete overview of CoreDNS integration with DNS Operator, see the [CoreDNS Integration Guide](../../docs/coredns/coredns-integration.md).

## Syntax

```
kuadrant [ZONES...]
```

With only the plugin specified, the *kuadrant* plugin will default to the zone specified in
the server's block. It will handle all queries in that zone and connect to Kubernetes in-cluster. If **ZONES** is used
it specifies all the zones the plugin should be authoritative for.

```
kuadrant [ZONES...] {
    kubeconfig KUBECONFIG [CONTEXT]
    rname EMAIL
}
```

* `kubeconfig` **KUBECONFIG [CONTEXT]** authenticates the connection to a remote Kubernetes cluster using a kubeconfig file.
  **[CONTEXT]** is optional, if not set, then the current context specified in kubeconfig will be used.
* `rname` **EMAIL** sets the email address (RNAME) in the SOA record for the zone. The email format (e.g., `admin@example.com`)
  will be converted to DNS mailbox format (e.g., `admin.example.com.`). According to [RFC 1035](https://www.rfc-editor.org/rfc/rfc1035.html) and [RFC 2142](https://www.rfc-editor.org/rfc/rfc2142.html), any dots in the
  local part (before @) will be escaped with backslash (e.g., `dns.admin@example.com` becomes `dns\.admin.example.com.`).
  If not specified, defaults to `hostmaster.{zone}`.

For enabling zone transfers look at the *transfer* plugin.

## Examples

### Example 1: Basic DNS Record Serving

Load the `example.org` zone from DNSRecord resources on the cluster with the label `kuadrant.io/coredns-zone-name: example.org`.

**Corefile:**
```corefile
example.org {
    kuadrant
}
```

**DNSRecord:**
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: example.org
  labels:
    kuadrant.io/coredns-zone-name: example.org
spec:
  rootHost: api.example.org
  endpoints:
    - dnsName: api.example.org
      recordTTL: 60
      recordType: A
      targets:
        - 1.1.1.1
```

### Example 2: Geographic Routing with GeoIP

Load the `example.org` zone from DNSRecord resources on the cluster with the label `kuadrant.io/coredns-zone-name: example.org` and apply geoip lookup to route clients to region-specific endpoints.

**Corefile:**
```corefile
example.org {
   geoip GeoLite2-City-demo.mmdb {
      edns-subnet
   }
   metadata
   kuadrant
}
```

**DNSRecord:**
```yaml
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: example.org
  labels:
    kuadrant.io/coredns-zone-name: example.org
spec:
  rootHost: api.example.org
  endpoints:
    - dnsName: api.example.org
      recordType: A
      providerSpecific:
        - name: geo-code
          value: GEO-EU
      recordTTL: 300
      setIdentifier: GEO-EU
      targets:
        - 1.1.1.1
    - dnsName: api.example.org
      recordType: A
      providerSpecific:
        - name: geo-code
          value: GEO-US
      recordTTL: 300
      setIdentifier: GEO-US
      targets:
        - 2.2.2.2
```

## Development

Quick start for testing plugin changes locally:

**Prerequisites:**
- Running Kubernetes cluster with kubectl configured
- A `Corefile` in the `coredns/plugin/` directory (see below)

When you run `make run` from the `coredns/plugin/` directory, CoreDNS will look for a `Corefile` in the current working directory. Create a `Corefile` in `coredns/plugin/` with the following content (see `coredns/examples/Corefile` for reference):

```corefile
k.example.com {
   debug
   errors
   log
   geoip geoip/GeoLite2-City-demo.mmdb {
      edns-subnet
   }
   metadata
   kuadrant {
      kubeconfig <path-to-your-home>/.kube/config
   }
}
```

**Note:** The `geoip` line is optional and only needed for testing geographic routing. For basic DNS record testing, you can omit it.

Then run CoreDNS locally:

```shell
# Run from coredns/plugin directory
make run

# In another terminal, apply a test DNSRecord and label it
kubectl apply -f ../examples/dnsrecord-api-k-example-com_geo_weight.yaml
kubectl label dnsrecord/api-k-example-com kuadrant.io/coredns-zone-name=k.example.com

# Verify DNS resolution
dig @127.0.0.1 api.k.example.com -p 1053 +short
```

For comprehensive local development and testing instructions, see the [CoreDNS Local Development Guide](../../docs/coredns/local-development.md).

## Troubleshooting

**DNSRecords not appearing in DNS queries:**

Verify the DNSRecord has the required label:
```shell
kubectl get dnsrecord -o jsonpath='{.items[*].metadata.labels}' | grep kuadrant.io/coredns-zone-name
```

**Plugin not loading or errors during startup:**

Check the terminal output where you ran `make run` for error messages. The plugin logs errors and warnings directly to stdout/stderr.

For more troubleshooting guidance:
- Local environment issues: [Local Development Guide Troubleshooting](../../docs/coredns/local-development.md#troubleshooting)
- General CoreDNS integration issues: [Integration Guide Troubleshooting](../../docs/coredns/coredns-integration.md#troubleshooting)

## See Also

- [CoreDNS Integration Guide](../../docs/coredns/coredns-integration.md) - Complete overview and production deployment
- [CoreDNS Local Development Guide](../../docs/coredns/local-development.md) - Local testing with Kind clusters
- [CoreDNS Configuration Reference](../../docs/coredns/configuration.md) - Detailed configuration options
- [CoreDNS Official Documentation](https://coredns.io/manual/toc/) - General CoreDNS plugins and configuration
- [CoreDNS GeoIP Plugin](https://coredns.io/plugins/geoip/) - Geographic routing plugin
