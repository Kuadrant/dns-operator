
## Setup local environment (kind)

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

Forward port for grafana (Optional)
```shell
kubectl -n monitoring port-forward service/grafana 3000:3000
```
Access dashboards http://127.0.0.1:3000

Tail all operator logs (Optional)
```shell
kubectl stern -l control-plane=dns-operator-controller-manager -A
```

## Run scale test

Export Environment variables:
```shell
#All
export PROMETHEUS_URL=http://127.0.0.1:9090 
export PROMETHEUS_TOKEN=""
#AWS
export KUADRANT_AWS_ACCESS_KEY_ID=<my aws access key id>
export KUADRANT_AWS_SECRET_ACCESS_KEY=<my aws secret access key>
export KUADRANT_AWS_REGION=""
#GCP
export KUADRANT_GCP_GOOGLE_CREDENTIALS=<my gcp credentals json>
export KUADRANT_GCP_PROJECT_ID=<my gcp project id>
#Azure
export KUADRANT_AZURE_CREDENTIALS=<my azure credentials json>
```

### inmemory

```shell
make test-scale
```
### aws

```shell
make test-scale DNS_PROVIDER=aws KUADRANT_ZONE_ROOT_DOMAIN=<my aws hosted domain>
```

### gcp

```shell
make test-scale DNS_PROVIDER=gcp KUADRANT_ZONE_ROOT_DOMAIN=<my gcp hosted domain>
```

### azure

```shell
make test-scale DNS_PROVIDER=azure KUADRANT_ZONE_ROOT_DOMAIN=<my azure hosted domain>
```