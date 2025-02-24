# CoreDNS Integration

## Setup local environment (kind)

Create a kind cluster
```shell
(cd ../.. && make local-setup)
```

Enable MetalLB (Optional)
```shell
(cd ../.. && make install-metallb)
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

Configure CoreDNS
```shell
(cd ../.. && make install-coredns-multi)
```

Set up the provider and dns records and handle via dns operator

```
k get services -A 
# get the external IPs from the c1 and c2 services

kubectl create secret generic core-dns --namespace=kuadrant-coredns-1 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="10.89.0.18:53,10.89.0.16:53" --from-literal=ZONES="k.example.com"
kubectl create secret generic core-dns --namespace=kuadrant-coredns-2 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="10.89.0.18:53,10.89.0.16:53" --from-literal=ZONES="k.example.com"

kubectl apply -f coredns/examples/dnsrecord-c1.yaml
kubectl apply -f coredns/examples/dnsrecord-c2.yaml

make run

dig @10.89.0.18 k.example.com
dig @10.89.0.16 k.example.com


Clean up

kubectl delete -f coredns/examples/dnsrecord-c1.yaml
kubectl delete -f coredns/examples/dnsrecord-c2.yaml

```


View Corefile configmap data
```shell
kubectl get configmap/kuadrant-coredns -n kuadrant-coredns -o yaml | yq .data
```

Tail CoreDNS logs
```shell
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-coredns
```

Use DNS Server:
```shell
DNS_SRV=`kubectl get service/kuadrant-coredns -n kuadrant-coredns -o yaml | yq .status.loadBalancer.ingress[0].ip`
echo $DNS_SRV
echo "Dig command: dig @$DNS_SRV google.com"
dig @$DNS_SRV google.com
```

```shell
dig @$DNS_SRV google.com
```

Redeploy CoreDNS after modifications:
```shell
../../bin/kustomize build --enable-helm ../../config/coredns/ | kubectl apply -f -
```

### Setup multi CoreDNS POC

Create coredns provider secrets:
```shell
KNS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics,app.kubernetes.io/part-of=coredns-multi -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip + ":53")] | join(",")'`
echo $KNS
kubectl create secret generic core-dns --namespace=kuadrant-coredns-1 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="$KNS" --from-literal=ZONES="k.example.com"
kubectl create secret generic core-dns --namespace=kuadrant-coredns-2 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="$KNS" --from-literal=ZONES="k.example.com"
```

Apply example dnsrecords:
```shell
(cd ../.. && kubectl apply -f coredns/examples/dnsrecord-c1.yaml)
(cd ../.. && kubectl apply -f coredns/examples/dnsrecord-c2.yaml)
```

Delete example dnsrecords:
```shell
(cd ../.. && kubectl delete -f coredns/examples/dnsrecord-c1.yaml)
(cd ../.. && kubectl delete -f coredns/examples/dnsrecord-c2.yaml)
```

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
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics,app.kubernetes.io/part-of=coredns-multi -o json | jq -r .items[].status.loadBalancer.ingress[].ip
```

Get Name servers string:
```shell
kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics,app.kubernetes.io/part-of=coredns-multi -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip + ":53")] | join(",")'
```

Tail all coredns logs:
```shell
kubectl stern -A -l app.kubernetes.io/part-of=coredns-multi
```
