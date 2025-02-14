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

kubectl create secret generic core-dns --namespace=kuadrant-dns-c1 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="10.89.0.18:53,10.89.0.16:53" --from-literal=ZONES="k.example.com"
kubectl create secret generic core-dns --namespace=kuadrant-dns-c2 --type=kuadrant.io/coredns --from-literal=NAMESERVERS="10.89.0.18:53,10.89.0.16:53" --from-literal=ZONES="k.example.com"

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
kubectl get configmap/kuadrant-coredns -n kuadrant-dns -o yaml | yq .data
```

Tail CoreDNS logs
```shell
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-dns
```

Use DNS Server:
```shell
DNS_SRV=`kubectl get service/kuadrant-coredns -n kuadrant-dns -o yaml | yq .status.loadBalancer.ingress[0].ip`
echo $DNS_SRV
echo "Dig command: dig @$DNS_SRV google.com"
dig @$DNS_SRV google.com
```

```shell
dig @$DNS_SRV lb.foo.bar.baz
```

Redeploy CoreDNS after modifications:
```shell
../../bin/kustomize build --enable-helm ../../config/coredns/ | kubectl apply -f -
```