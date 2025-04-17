# kuadrant

## Name

*kuadrant* - enables serving zone data from kuadrant DNSRecord resources.

## Description

The *kuadrant* plugin sets up watch and listers on kuadrant's DNSRecord resources in the k8s cluster and as it discovers
them processes them and adds the endpoints to the appropriate DNS zone with the correct GEO and Weighted data.
The plugin uses logic from the [CoreDNS file plugin](https://github.com/coredns/coredns/tree/master/plugin/file) to create a functioning DNS server.

**Weighting**

For weighted responses, the *kuadrant* plugin builds a list of all the available records that could be provided as the
answer to a given query from within the identified zone. It then applies a weighting algorithm to decide on a single
response depending on the individual record weighting. It is effectively decided each time based on a random number
between 0 and the sum of all the weights. So it is not a super predictable response but is a correctly weighted response.

**GEO**

GEO data is sourced from a geo database such as MaxMind. This is then made available via the existing
[CoreDNS geoip plugin](https://coredns.io/plugins/geoip/). This plugin must execute before the Kuadrant plugin in order
for GEO based responses to be provided. With this plugin enabled, Kuadrant can use the GEO data to decide which record
to return to the DNS query.

**Weighting within a GEO**

It can be the case that you have multiple endpoints within a single GEO and want to weight traffic across those
endpoints. In this case the Kuadrant plugin will first apply the GEO filter and then use the weighting filter on the
result if there is more than one endpoint within a given GEO.

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
}
```

* `kubeconfig` **KUBECONFIG [CONTEXT]** authenticates the connection to a remote k8s cluster using a kubeconfig file.
  **[CONTEXT]** is optional, if not set, then the current context specified in kubeconfig will be used.

For enabling zone transfers look at the *transfer* plugin.

## Examples

Load the `example.org` zone from DNSRecord resources on cluster with the label `kuadrant.io/coredns-zone-name: example.org`

```corefile
example.org {
    kuadrant
}
```

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

Load the `example.org` zone from DNSRecord resources on cluster with the label `kuadrant.io/coredns-zone-name: example.org` and 
apply geoip lookup.

```corefile
example.org {
   geoip GeoLite2-City-demo.mmdb
   metadata
   kuadrant
}
```

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
          value: GEO-EU
      recordTTL: 300
      setIdentifier: GEO-EU
      targets:
        - 2.2.2.2
```

## Development

Make targets to aid development can be viewed with:
```shell
make help
```

Create a Corefile for local development that references your local kubeconfig:
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
      kubeconfig <path to kubeconfig>/.kube/config
   }
}
. {
   forward . /etc/resolv.conf
}
```

Run Kuadrant build of CoreDNS locally:
```shell
make run
```

Create a DNSRecord resource and label it with the correct zone name:
```shell
kubectl apply -f ../examples/dnsrecord-api-k-example-com_geo_weight.yaml
kubectl label dnsrecord/api-k-example-com kuadrant.io/coredns-zone-name=k.example.com
```

Verify you can access the server:
```shell
dig @127.0.0.1 api.k.example.com -p 1053 +subnet=127.0.100.100 +short
klb.api.k.example.com.
geo-eu.klb.api.k.example.com.
cluster3.klb.api.k.example.com.
127.0.0.3
```
