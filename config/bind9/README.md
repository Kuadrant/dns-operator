# Bind9

**Note:** All shell commands in this doc are written as if you were executing them from this directory(`config/bind9`).

## Install

Install Bind9 edge server and verify deployments:
```shell
cd ../..
make install-bind9
kubectl get deployments -l app.kubernetes.io/name=bind9 -A
```
Example output:
```
NAMESPACE        NAME   READY   UP-TO-DATE   AVAILABLE   AGE
kuadrant-bind9   edge   1/1     1            1           22s
```

Retrieve the edge dns server ip:
```shell
EDGE_NS=`kubectl get service/kuadrant-bind9 -n kuadrant-bind9 -o json | jq -r '.status.loadBalancer.ingress[].ip'`
echo $EDGE_NS 
```

Verify the "example.com" zone is present by issuing a transfer query:
```shell
dig @$EDGE_NS -k ddns.key -t AXFR example.com
```
Example output:
```
; <<>> DiG 9.18.28 <<>> @172.18.0.17 -k ddns.key -t AXFR example.com
; (1 server found)
;; global options: +cmd
example.com.            30      IN      SOA     example.com. root.example.com. 16 30 30 30 30
example.com.            30      IN      NS      ns.example.com.
ns.example.com.         30      IN      A       127.0.0.1
example.com.            30      IN      SOA     example.com. root.example.com. 16 30 30 30 30
example.com-key.        0       ANY     TSIG    hmac-sha256. 1746003117 300 32 l2Mvzp/WbWeajEWx8Vh7ZQDMuHAvCdemYR/k2acFY2E= 26723 NOERROR 0 
```
**Note:** The ddns.key should be a path relative to where you are running this command i.e. config/bind9/ddns.key from the root of this repo

Verify the "example.com" SOA is correct:
```shell
dig @$EDGE_NS soa example.com +norec
```
Example Output:
```
; <<>> DiG 9.18.28 <<>> @172.18.0.17 soa example.com +norec
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 20450
;; flags: qr aa ra; QUERY: 1, ANSWER: 1, AUTHORITY: 1, ADDITIONAL: 2

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags:; udp: 1232
; COOKIE: 2ffab744a5ba3196010000006811e75013667fa267a13cc1 (good)
;; QUESTION SECTION:
;example.com.                   IN      SOA

;; ANSWER SECTION:
example.com.            30      IN      SOA     example.com. root.example.com. 16 30 30 30 30

;; AUTHORITY SECTION:
example.com.            30      IN      NS      ns.example.com.

;; ADDITIONAL SECTION:
ns.example.com.         30      IN      A       127.0.0.1
```

At this point you have a working Bind9 instance running that we can query.

## Configure kuadrant CoreDNS zone (k.example.com) delegation

Check for CoreDNS instances currently running on the cluster:

```shell
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics
```
Example output:
```
NAMESPACE          NAME               TYPE           CLUSTER-IP      EXTERNAL-IP   PORT(S)                     AGE
kuadrant-coredns   kuadrant-coredns   LoadBalancer   10.96.242.254   172.18.0.16   53:31680/UDP,53:31680/TCP   31m
```

Each of the CoreDNS instances should be watching for records in the "k.example.com" zone. You can verify this by checking the configuration or logs:
```
kuadrant-coredns kuadrant-coredns-5fd84d57b5-dqbcf coredns [INFO] plugin/kuadrant: Starting informer 0 for zone k.example.com.
```

Get a list of all the CoreDNS services external ips:
```shell
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip)] | join(",")'
```
Example output:
```
172.18.0.16
```
**Note:** In the case of multiple deployments this would be a comma seperated list of IPs.

For each IP create a nsupdate command and issue it against the edge server to add the zone records.

To generate a nsupdate file for the first CoreDNS instance found:
```shell
EDGE_NS=`kubectl get service/kuadrant-bind9 -n kuadrant-bind9 -o json | jq -r '.status.loadBalancer.ingress[].ip'`
CORE_NS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip)][0]'`
cat <<EOF >>nsupdate-k-example-com-1
server ${EDGE_NS}
debug yes
zone example.com.
update add k.example.com 300 IN NS ns1.k.example.com
update add ns1.k.example.com 300 IN A ${CORE_NS}
send
EOF
```

Check the resulting nsupdate file:
```shell
cat nsupdate-k-example-com-1
```
Example output:
```
server 172.18.0.17
debug yes
zone example.com.
update add k.example.com 300 IN NS ns1.k.example.com
update add ns1.k.example.com 300 IN A 172.18.0.16
send
```

Apply the update:
```shell
nsupdate -k ddns.key -v nsupdate-k-example-com-1
```
Example output:
```
Sending update to 172.18.0.17#53
Outgoing update query:
;; ->>HEADER<<- opcode: UPDATE, status: NOERROR, id:  35292
;; flags:; ZONE: 1, PREREQ: 0, UPDATE: 2, ADDITIONAL: 1
;; ZONE SECTION:
;example.com.                   IN      SOA

;; UPDATE SECTION:
k.example.com.          300     IN      NS      ns1.k.example.com.
ns1.k.example.com.      300     IN      A       172.18.0.16

;; TSIG PSEUDOSECTION:
example.com-key.        0       ANY     TSIG    hmac-sha256. 1746017622 300 32 KcrBVlBh3qMii9PnVdyo/yvL5nyNyzRd/0UMh63NXCU= 35292 NOERROR 0 


Reply from update query:
;; ->>HEADER<<- opcode: UPDATE, status: NOERROR, id:  35292
;; flags: qr; ZONE: 1, PREREQ: 0, UPDATE: 0, ADDITIONAL: 1
;; ZONE SECTION:
;example.com.                   IN      SOA

;; TSIG PSEUDOSECTION:
example.com-key.        0       ANY     TSIG    hmac-sha256. 1746017622 300 32 XzSXAxLJ4Stl3kvtSAPyeALoSyKyYsb6Y3i9ktoxcag= 35292 NOERROR 0 
```

Verify the "example.com" zone is updated by issuing a transfer query:
```shell
dig @$EDGE_NS -k ddns.key -t AXFR example.com
```
Example output:
```
; <<>> DiG 9.18.28 <<>> @172.18.0.17 -k ddns.key -t AXFR example.com
; (1 server found)
;; global options: +cmd
example.com.            30      IN      SOA     example.com. root.example.com. 17 30 30 30 30
example.com.            30      IN      NS      ns.example.com.
k.example.com.          300     IN      NS      ns1.k.example.com.
ns1.k.example.com.      300     IN      A       172.18.0.16
ns.example.com.         30      IN      A       127.0.0.1
example.com.            30      IN      SOA     example.com. root.example.com. 17 30 30 30 30
example.com-key.        0       ANY     TSIG    hmac-sha256. 1746017678 300 32 YoIj5YKtm5M4J/pnsqQFFOPWdunw4CoEoaTS0sXlv8I= 21316 NOERROR 0 
;; Query time: 0 msec
;; SERVER: 172.18.0.17#53(172.18.0.17) (TCP)
;; WHEN: Wed Apr 30 13:54:38 IST 2025
;; XFR size: 6 records (messages 1, bytes 302)
```

Verify the "k.example.com" SOA is correct:
```shell
dig @$EDGE_NS soa k.example.com
```
Example output:
```
; <<>> DiG 9.18.28 <<>> @172.18.0.17 soa k.example.com
; (1 server found)
;; global options: +cmd
;; Got answer:
;; ->>HEADER<<- opcode: QUERY, status: NOERROR, id: 24558
;; flags: qr rd ra; QUERY: 1, ANSWER: 1, AUTHORITY: 0, ADDITIONAL: 1

;; OPT PSEUDOSECTION:
; EDNS: version: 0, flags:; udp: 1232
; COOKIE: ee7dddeaafafeb5a0100000068121f2e5148c240270af1ba (good)
;; QUESTION SECTION:
;k.example.com.                 IN      SOA

;; ANSWER SECTION:
k.example.com.          60      IN      SOA     ns1.k.example.com. hostmaster.k.example.com. 12345 7200 1800 86400 60

;; Query time: 53 msec
;; SERVER: 172.18.0.17#53(172.18.0.17) (UDP)
;; WHEN: Wed Apr 30 14:01:34 IST 2025
;; MSG SIZE  rcvd: 121
```

## Verify

At this point you should have a zone(k.example.com) in the edge server(bind9) that is delegated to the CoreDNS instance. 
To verify this is working as expected we can create a DNSRecord and verify that we can query it via the edge recursive resolver.


Create the example DNSRecord(api.k.example.com) and label appropriately so it's picked up by the CoreDNS instance: 
```shell
kubectl apply -f ../../coredns/examples/dnsrecord-api-k-example-com_geo_weight.yaml
kubectl label dnsrecord/api-k-example-com kuadrant.io/coredns-zone-name=k.example.com
```

```shell
dig @$EDGE_NS api.k.example.com +short
```
Example output: 
```
klb.api.k.example.com.
geo-us.klb.api.k.example.com.
cluster1.klb.api.k.example.com.
127.0.0.1
```

## Other commands:

Generate new ddns key:
```shell
ddns-confgen -k example.com-key -z example.com.
```

Tail all CoreDNS and Bind9 logs:
```shell
kubectl stern -l 'app.kubernetes.io/name in (coredns, bind9)' -A
```
