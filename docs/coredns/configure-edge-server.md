# Configure Local Edge Server (Bind9)

## Prerequisites

* local kind cluster
```shell
(cd ../.. && make local-setup)
```

## Update edge server (Bind9) with example.com zone

Generate an nsupdate file for the kuadrant CoreDNS instance:
```shell
EDGE_NS=`kubectl get service/kuadrant-bind9 -n kuadrant-bind9 -o json | jq -r '.status.loadBalancer.ingress[].ip'`
CORE_NS=`kubectl get service -A -l app.kubernetes.io/name=coredns,app.kubernetes.io/component!=metrics -o json | jq -r '[.items[] | (.status.loadBalancer.ingress[].ip)][0]'`
cat <<EOF >nsupdate-k-example-com
server ${EDGE_NS}
debug yes
zone example.com.
update add k.example.com 300 IN NS ns1.k.example.com
update add ns1.k.example.com 300 IN A ${CORE_NS}
update add k.kuadrant-active-groups.example.com 300 TXT "group=foo"
send
EOF
```

Apply the update:
```shell
nsupdate -k ../../config/bind9/ddns.key -v nsupdate-k-example-com
```

Verify the "example.com" zone is updated by issuing a transfer query:
```shell
dig @$EDGE_NS -k ../../config/bind9/ddns.key -t AXFR example.com
```

## Update on cluster CoreDNS instance to forward requests for "example.com" to our edge server (Bind9)

```shell
CLUSTER_EDGE_NS=`kubectl get service/kuadrant-bind9 -n kuadrant-bind9 -o yaml | yq '.spec.clusterIP'`
ZONE=example.com
kubectl get configmap/coredns -n kube-system -o yaml | yq .data.Corefile > kube.Corefile
echo "$ZONE:53 {
    forward . $CLUSTER_EDGE_NS
}"|cat - kube.Corefile > /tmp/out && mv /tmp/out kube.Corefile
cat kube.Corefile
kubectl create configmap coredns -n kube-system --from-file=Corefile=kube.Corefile --dry-run=client -o yaml | kubectl apply -f -
```

Verify pods can query the edge server:
```shell
kubectl run dig --attach --rm --restart=Never -q --image=toolbelt/dig -- -t TXT k.kuadrant-active-groups.example.com +short
```

## Update kuadrant-coredns to rewrite active group (kuadrant-active-groups.k.example.com -> k.kuadrant-active-groups.example.com)

```shell
CLUSTER_EDGE_NS=`kubectl get service/kuadrant-bind9 -n kuadrant-bind9 -o yaml | yq '.spec.clusterIP'`
ZONE=example.com
kubectl get configmap/kuadrant-coredns -n kuadrant-coredns -o yaml | yq .data.Corefile > kuadrant.Corefile
echo "kuadrant-active-groups.k.example.com {
    debug
    errors
    log
    rewrite stop {
      name kuadrant-active-groups.k.example.com k.kuadrant-active-groups.example.com
    }
    forward . $CLUSTER_EDGE_NS
}"|cat - kuadrant.Corefile > /tmp/out && mv /tmp/out kuadrant.Corefile
cat kuadrant.Corefile
kubectl create configmap kuadrant-coredns -n kuadrant-coredns --from-file=Corefile=kuadrant.Corefile --dry-run=client -o yaml | kubectl apply -f -
```

```shell
cd ../..
kubectl scale deployments/kuadrant-coredns -n kuadrant-coredns --replicas=0 && kubectl scale deployments/kuadrant-coredns -n kuadrant-coredns --replicas=1
kubectl logs -f deployments/kuadrant-coredns -n kuadrant-coredns
```

Verify pods can query the kuadrant-active-groups for the zone:
```shell
kubectl run dig --attach --rm --restart=Never -q --image=toolbelt/dig -- -t TXT kuadrant-active-groups.k.example.com
```
