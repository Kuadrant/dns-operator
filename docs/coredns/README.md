# CoreDNS Integration

## Setup local environment (kind)

Create a kind cluster
```shell
(cd ../.. && make local-setup)
```

Configure observability stack (Optional)
```shell
(cd ../.. && make install-observability)
```

Forward port for grafana (Optional)
```shell
kubectl -n monitoring port-forward service/grafana 3000:3000
```
Access dashboards http://127.0.0.1:3000

> **_NOTE:_** default user/password is admin/admin

### Default CoreDNS

Local setup will deploy a single instance of CoreDNS with the kuadrant plugin enabled, configured to watch all  namespaces for DNSRecord resources and zones configured for demo/test purposes.

The Corefile configmap data can be viewed with:
```shell
kubectl get configmap/kuadrant-coredns -n kuadrant-coredns -o yaml | yq .data
```

Tail CoreDNS logs
```shell
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-coredns
```

#### Enable Monitoring:

Monitoring is not enabled by default, if you configured the observability stack above, the CoreDNS instance can be  updated to enable it with:
```shell
../../bin/kustomize build --enable-helm ../../config/coredns/ | kubectl apply -f -
```

#### Redeploy CoreDNS:

Changes can be made to the Corefile or any deployment by modifying and redeploying the appropriate configuration. 
Depending on whether you enabled monitoring or not, different config will need to be applied.
```shell
../../bin/kustomize build --enable-helm ../../config/coredns[-unmonitored]/ | kubectl apply -f -
```

### Verify

Create DNSRecord:
```bash
kubectl apply -f - <<EOF
apiVersion: kuadrant.io/v1alpha1
kind: DNSRecord
metadata:
  name: api-k-example-com
  labels:
    kuadrant.io/coredns-zone-name: k.example.com
spec:
  rootHost: api.k.example.com
  endpoints:
    - dnsName: api.k.example.com
      recordTTL: 60
      recordType: A
      targets:
        - 1.1.1.1
  providerRef:
    name: dns-provider-credentials-coredns
EOF
````

Verify DNS Server responds:
```shell
DNS_SRV=`kubectl get service/kuadrant-coredns -n kuadrant-coredns -o yaml | yq '.status.loadBalancer.ingress[0].ip'`
echo $DNS_SRV
echo "Dig command: dig @$DNS_SRV api.k.example.com"
dig @$DNS_SRV api.k.example.com
```

### Setup Multi CoreDNS POC

Install CoreDNS Multi POC Setup
```shell
(cd ../.. && make install-coredns-multi)
```

Create coredns provider secrets:
```shell
KNS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics,app.kubernetes.io/part-of=coredns-multi -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip + ":53")] | join(",")'`
echo $KNS
kubectl create secret generic core-dns --namespace=kuadrant-coredns-1 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="$KNS" --from-literal=ZONES="k.example.com"
kubectl create secret generic core-dns --namespace=kuadrant-coredns-2 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="$KNS" --from-literal=ZONES="k.example.com"
```

Run dns-operator instance
```shell
(cd ../.. && make run)
```

Apply example dnsrecords:
```shell
(cd ../.. && kubectl apply -f coredns/examples/dnsrecord-c1.yaml)
(cd ../.. && kubectl apply -f coredns/examples/dnsrecord-c2.yaml)
```

Dig nameservers
```shell
NS1=`kubectl get service -n kuadrant-coredns-1 -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`
NS2=`kubectl get service -n kuadrant-coredns-2 -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`

echo $NS1
echo $NS2

dig @${NS1} k.example.com
dig @${NS2} k.example.com
```

Delete example dnsrecords:
```shell
(cd ../.. && kubectl delete -f coredns/examples/dnsrecord-c1.yaml)
(cd ../.. && kubectl delete -f coredns/examples/dnsrecord-c2.yaml)
```

### GEO
The geo functionality is provided by the [geoip](https://coredns.io/plugins/geoip/) plugin from CoreDNS. 
The kuadrant CoreDNS container image has a mock db embedded at it's root (GeoLite2-City-demo.mmdb), generated using `coredns/plugin/geoip/db-generator.go`, that can be used for testing purposes.
The mock database contains sets of "local" subnets that are typical for kind deployments on mac and linux that are pointing at IE and US locales:

| Subnet           | Continent          | Country            |
|------------------|--------------------|--------------------|
| 127.0.100.0/24 	 | Europe / EU        | Ireland / IE       |
| 27.0.200.0/24  	 | North America / NA | United States / US |
| 10.89.100.0/24 	 | Europe / EU        | Ireland / IE       |
| 10.89.200.0/24 	 | North America / NA | United States / US |

You can use `-b` option with dig to use any available to host machine IP addresses as a "source". E.G `dig @[nameserver] [hostname] -p [exposed-port] -b 127.0.100.1` will be associated with IE locale and `-b 127.0.200.1` with US

> **_NOTE:_** the demo DB contains only localhost addresses. I.E. will work only with CoreDNS instance running with `make coredns-run` (not in kind cluster) unless you specify desired subnet in dig with `+subnet=[subnet]`

To add more subnets, it is the best to generate a new DB file. Add your desired CIDR range to the constants and at the end of the file associate it with the desired record (IE or US). 

For a deployment using a real-world database you could refer to the [maxmind](https://dev.maxmind.com/geoip/) for their free db. Once obtained it must be mounted and referenced in the Corefile instead of the demo-db.

### Misc Commands

Get all CoreDNS Deployments:
```shell
kubectl get deployments -A -l app.kubernetes.io/name=coredns
```

Get all CoreDNS services:
```shell
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics
```

Get all CoreDNS service external IPs for multi deployment:
```shell
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics,app.kubernetes.io/part-of=coredns-multi -o json | jq -r '.items[].status.loadBalancer.ingress[].ip'
```

Get Name servers string:
```shell
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics,app.kubernetes.io/part-of=coredns-multi -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip + ":53")] | join(",")'
```

Tail all coredns logs:
```shell
kubectl stern -A -l app.kubernetes.io/part-of=coredns-multi
```
