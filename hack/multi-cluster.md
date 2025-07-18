## Multi cluster

### Cluster Setup

Create two clusters with the DNS Operator deployed
```shell
make local-setup CLUSTER_COUNT=2 DEPLOY=true
```

Tail the operator logs on Cluster 1 (Optional)
```shell
kubectl stern deployment/dns-operator-controller-manager -n dns-operator-system --context kind-kuadrant-dns-local-1
```

Tail the operator logs on Cluster 2 (Optional)
```shell
kubectl stern deployment/dns-operator-controller-manager -n dns-operator-system --context kind-kuadrant-dns-local-2
```

#### Add cluster 2 kubeconfig secret to cluster 1

When testing locally with kind, depending on how you are running the dns controller, locally (make run), or on cluster (make deploy), you are going to need to generate the cluster secret with different `kubeconfig` data to allow correct communication between clusters.

#####  Remote("make deploy")
```shell
make kubeconfig-secret-create-kind-internal NAMESPACE=dns-operator-system NAME=kind-kuadrant-dns-local-2 TARGET_CONTEXT=kind-kuadrant-dns-local-1 REMOTE_CONTEXT=kind-kuadrant-dns-local-2 SERVICE_ACCOUNT=dns-operator-remote-cluster
```
>Note: This is running in a container on the kind cluster in order for the script to properly communicate with the remote and primary servers and generate a config that will work from inside the kind cluster.

##### Locally("make run")
```shell
make kubeconfig-secret-create NAMESPACE=dns-operator-system NAME=kind-kuadrant-dns-local-2 TARGET_CONTEXT=kind-kuadrant-dns-local-1 REMOTE_CONTEXT=kind-kuadrant-dns-local-2 SERVICE_ACCOUNT=dns-operator-remote-cluster
```

Verify cluster secret exists on cluster 1 only
```shell
kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-kuadrant-dns-local-1
kubectl get secrets -A -l kuadrant.io/multicluster-kubeconfig=true --show-labels --context kind-kuadrant-dns-local-2
```
