# CoreDNS Integration

## Setup single cluster local environment (kind)

Create a kind cluster
```shell
(cd ../.. && make local-setup DEPLOY=true)
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

Local setup will deploy a single instance of CoreDNS with the kuadrant plugin enabled, configured to watch all namespaces for DNSRecord resources and zones configured for demo/test purposes.

The Corefile configmap data can be viewed with:
```shell
kubectl get configmap/kuadrant-coredns -n kuadrant-coredns -o yaml | yq .data
```

Tail CoreDNS logs
```shell
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-coredns
```

#### Customizing SOA RNAME Email

The default SOA (Start of Authority) RNAME field uses `hostmaster.{zone}` format. You can customize this to use a specific email address by modifying the Corefile configuration.

To set a custom RNAME email, edit the Corefile in the `kuadrant-coredns` configmap:
```corefile
k.example.com {
   debug
   errors
   log
   kuadrant {
      rname admin@example.com
   }
}
```

After updating the configmap, restart CoreDNS:
```shell
kubectl -n kuadrant-coredns rollout restart deployment kuadrant-coredns
```

Verify the SOA record contains the custom email (converted to DNS mailbox format):
```shell
NS1=`kubectl get service/kuadrant-coredns -n kuadrant-coredns -o yaml | yq '.status.loadBalancer.ingress[0].ip'`
dig @${NS1} k.example.com SOA +short
```
Expected output with custom RNAME:
```
ns1.k.example.com. admin.example.com. 12345 7200 1800 86400 60
```
Note: `admin@example.com` is converted to `admin.example.com.` in DNS mailbox format. According to RFC 1035 and RFC 2142, any dots in the local part (before @) are escaped with backslash (e.g., `dns.admin@example.com` becomes `dns\.admin.example.com.`).

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
(cd ../.. && kubectl apply -n dnstest -f config/local-setup/dnsrecords/basic/coredns/simple/dnsrecord-simple-coredns.yaml)
````

Verify zone (k.example.com) has updated records in the CoreDNS instance:
```shell
NS1=`kubectl get service/kuadrant-coredns -n kuadrant-coredns -o yaml | yq '.status.loadBalancer.ingress[0].ip'`
echo $NS1
dig @${NS1} -t AXFR k.example.com
```
Expected:
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
Expected:
```
172.18.200.1
```

## Setup multi cluster local environment (kind)

Create three kind clusters (2 primary and 1 secondary)
```shell
(cd ../.. && make multicluster-local-setup PRIMARY_CLUSTER_COUNT=2 CLUSTER_COUNT=3)
```

### Verify primary cluster setup 

CoreDNS is deployed and running:
```shell
kubectl get deployments,service -A -l app.kubernetes.io/name=coredns --context kind-kuadrant-dns-local-1
kubectl get deployments,service -A -l app.kubernetes.io/name=coredns --context kind-kuadrant-dns-local-2
```
Expected:
```
NAMESPACE          NAME                               READY   UP-TO-DATE   AVAILABLE   AGE
kuadrant-coredns   deployment.apps/kuadrant-coredns   1/1     1            1           10m

NAMESPACE          NAME                       TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                     AGE
kuadrant-coredns   service/kuadrant-coredns   LoadBalancer   10.96.253.138   172.18.0.17   53:30494/UDP,53:30494/TCP   10m
```

Test zone (k.example.com) accessible in CoreDNS instances:
```shell
NS1=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-1 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`
NS2=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-2 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`

dig @${NS1} -t AXFR k.example.com
dig @${NS2} -t AXFR k.example.com
```
Expected:
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

CoreDNS secret exists in the "dnstest" namespace with test zone (k.example.com) configured:
```shell
kubectl get secret/dns-provider-credentials-coredns -n dnstest -o jsonpath='{.data.ZONES}' --context kind-kuadrant-dns-local-1 | base64 --decode
kubectl get secret/dns-provider-credentials-coredns -n dnstest -o jsonpath='{.data.ZONES}' --context kind-kuadrant-dns-local-2 | base64 --decode
```
Expected:
```
k.example.com
```

Cluster secrets exists on kind-kuadrant-dns-local-1(primary 1) for kind-kuadrant-dns-local-2(primary 2) and kind-kuadrant-dns-local-3(secondary):
```shell
kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-kuadrant-dns-local-1
```
Expected:
```
NAMESPACE             NAME                        TYPE     DATA   AGE   LABELS
dns-operator-system   kind-kuadrant-dns-local-2   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
dns-operator-system   kind-kuadrant-dns-local-3   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
```

Cluster secrets exists on kind-kuadrant-dns-local-2(primary 2) for kind-kuadrant-dns-local-1(primary 1) and kind-kuadrant-dns-local-3(secondary):
```shell
kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-kuadrant-dns-local-1
```
Expected:
```
NAMESPACE             NAME                        TYPE     DATA   AGE   LABELS
dns-operator-system   kind-kuadrant-dns-local-1   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
dns-operator-system   kind-kuadrant-dns-local-3   Opaque   1      19m   kuadrant.io/multicluster-kubeconfig=true
```

### Verify

Create "dnstest" namespace on kind-kuadrant-dns-local-3(secondary):
```shell
kubectl create ns dnstest --context kind-kuadrant-dns-local-3
```

Set CoreDNS provider as the default in the "dnstest" namespace on both primary clusters:
```shell
kubectl label secret/dns-provider-credentials-coredns -n dnstest kuadrant.io/default-provider=true --context kind-kuadrant-dns-local-1
kubectl label secret/dns-provider-credentials-coredns -n dnstest kuadrant.io/default-provider=true --context kind-kuadrant-dns-local-2
```
Apply example dnsrecords:
```shell
(cd ../.. && kubectl apply -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster1.yaml --context kind-kuadrant-dns-local-1)
(cd ../.. && kubectl apply -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster2.yaml --context kind-kuadrant-dns-local-2)
(cd ../.. && kubectl apply -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster3.yaml --context kind-kuadrant-dns-local-3)
```

Verify zone (k.example.com) has updated records in both primary cluster CoreDNS instances:
```shell
NS1=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-1 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`
NS2=`kubectl get service -n kuadrant-coredns -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics --context kind-kuadrant-dns-local-2 -o yaml | yq '.items[0].status.loadBalancer.ingress[0].ip'`

echo $NS1
echo $NS2

dig @${NS1} -t AXFR k.example.com
dig @${NS2} -t AXFR k.example.com
```
Expected:
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
dig @$NS2 loadbalanced.k.example.com +short +subnet=127.0.100.0/24
```
Expected:
```
klb.loadbalanced.k.example.com.
ie.klb.loadbalanced.k.example.com.
cluster1-gw1-ns1.klb.loadbalanced.k.example.com.
172.18.200.1
```

Delete example dnsrecords:
```shell
(cd ../.. && kubectl delete -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster1.yaml --context kind-kuadrant-dns-local-1)
(cd ../.. && kubectl delete -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster2.yaml --context kind-kuadrant-dns-local-2)
(cd ../.. && kubectl delete -n dnstest -f config/local-setup/dnsrecords/delegating/coredns/loadbalanced/dnsrecord-loadbalanced-coredns-cluster3.yaml --context kind-kuadrant-dns-local-3)
```

## GEO
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
