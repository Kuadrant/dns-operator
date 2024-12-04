
Create a kind cluster with prometheus/thanos installed and configured
```shell
make local-setup
kubectl apply --server-side -k config/observability
kubectl apply --server-side -k config/observability # Run twice if it fails the first time
```

Forward port for prometheus
```shell
kubectl -n monitoring port-forward service/thanos-query 9090:9090
```

Forward port for graphana (Optional)
```shell
kubectl -n monitoring port-forward service/grafana 3000:3000
```
Access dashboards http://127.0.0.1:3000

Tail all operator logs (Optional)
```shell
kubectl stern -l control-plane=dns-operator-controller-manager -A
```

Run default scale test(1 iteration using the inmemory provider)
```shell
PROMETHEUS_URL=http://127.0.0.1:9090 PROMETHEUS_TOKEN="" make test-scale
```
