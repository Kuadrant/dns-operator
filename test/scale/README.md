# DNS Operator Scale Test

Scale testing using [kube-burner](https://kube-burner.github.io/kube-burner/latest).


## Setup local environment (kind)

Create a kind cluster with prometheus/thanos installed and configured
```shell
cd ../.. && make local-setup
kubectl apply --server-side -k config/observability
kubectl apply --server-side -k config/observability # Run twice if it fails the first time
kubectl -n monitoring wait --timeout=60s --for=condition=Available deployments --all
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

Export common variables:
```shell
export PROMETHEUS_URL=http://127.0.0.1:9090 
export PROMETHEUS_TOKEN=""
```

### inmemory

```shell
kubectl delete dnsrecord -A -l kube-burner-job=scale-test-loadbalanced
(cd ../.. && make test-scale JOB_ITERATIONS=2 NUM_RECORDS=2 SKIP_CLEANUP=true)
kubectl get dnsrecord -A -l kube-burner-job=scale-test-loadbalanced -o wide
```

### aws

```shell
export KUADRANT_AWS_ACCESS_KEY_ID="my aws access key"
export KUADRANT_AWS_SECRET_ACCESS_KEY="my aws secret access key"
export KUADRANT_AWS_REGION=""
export KUADRANT_AWS_ZONE_ROOT_DOMAIN="my.aws.hosted.domain"
```

```shell
kubectl delete dnsrecord -A -l kube-burner-job=scale-test-loadbalanced
(cd ../.. && make test-scale JOB_ITERATIONS=2 NUM_RECORDS=2 SKIP_CLEANUP=true DNS_PROVIDER=aws)
kubectl get dnsrecord -A -l kube-burner-job=scale-test-loadbalanced -o wide
```

### gcp

```shell
export KUADRANT_GCP_GOOGLE_CREDENTIALS="my gcp credentals json"
export KUADRANT_GCP_PROJECT_ID="my gcp project id"
export KUADRANT_GCP_ZONE_ROOT_DOMAIN="my.gcp.hosted.domain"
```

```shell
kubectl delete dnsrecord -A -l kube-burner-job=scale-test-loadbalanced
(cd ../.. && make test-scale JOB_ITERATIONS=2 NUM_RECORDS=2 SKIP_CLEANUP=true DNS_PROVIDER=gcp)
kubectl get dnsrecord -A -l kube-burner-job=scale-test-loadbalanced -o wide
```

### azure

```shell
export KUADRANT_AZURE_CREDENTIALS="my azure credentials json"
export KUADRANT_AZURE_ZONE_ROOT_DOMAIN="my.azure.hosted.domain"
```

```shell
kubectl delete dnsrecord -A -l kube-burner-job=scale-test-loadbalanced
(cd ../.. && make test-scale JOB_ITERATIONS=2 NUM_RECORDS=2 SKIP_CLEANUP=true DNS_PROVIDER=azure)
kubectl get dnsrecord -A -l kube-burner-job=scale-test-loadbalanced -o wide
```

### all

```shell
kubectl delete dnsrecord -A -l kube-burner-job=scale-test-loadbalanced
(cd ../.. && make test-scale JOB_ITERATIONS=1 NUM_RECORDS=1 SKIP_CLEANUP=true DNS_PROVIDER=aws,azure,gcp,inmemory)
kubectl get dnsrecord -A -l kube-burner-job=scale-test-loadbalanced -o wide
```

## Checking alerts

```shell
(cd ../.. && ./bin/kube-burner check-alerts -u $PROMETHEUS_URL -t '$PROMETHEUS_TOKEN' -a test/scale/alerts.yaml)
```
