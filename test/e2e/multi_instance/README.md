# Multi Instance Test Suite

The multi instance test suite intended to test scenarios where multiple instances of the dns operator are running, each reconciling DNSRecord resources contributing to the same shared dns zone. 
The suite allows runtime configuration to alter the number of instances that are under test allowing stress testing scenarios to be executed using more extreme numbers of instances and records.

## Local Setup

Deploy the operator on a local kind cluster with X operator instances (DEPLOYMENT_COUNT):
```shell
make local-setup DEPLOY=true DEPLOYMENT_SCOPE=namespace DEPLOYMENT_COUNT=2
```

The above will create two dns operator deployments on the kind cluster, each configured to watch its own namespace, with the developmnet managedzones (Assuming you have configured them locally) created in each deployment namespace.

DNS Operator Deployments:
```shell
kubectl get deployments -l app.kubernetes.io/part-of=dns-operator -A
NAMESPACE        NAME                                READY   UP-TO-DATE   AVAILABLE   AGE
dns-operator-1   dns-operator-controller-manager-1   1/1     1            1           96s
dns-operator-2   dns-operator-controller-manager-2   1/1     1            1           96s
```

ManagedZones:
```shell
kubectl get managedzones -A
NAMESPACE        NAME         DOMAIN NAME             ...     READY
dns-operator-1   dev-mz-aws   mn.hcpapps.net          ...     True
dns-operator-1   dev-mz-gcp   mn.google.hcpapps.net   ...     True
dns-operator-2   dev-mz-aws   mn.hcpapps.net          ...     True
dns-operator-2   dev-mz-gcp   mn.google.hcpapps.net   ...     True
dnstest          dev-mz-aws   mn.hcpapps.net                                                                                                                                                                                                     
dnstest          dev-mz-gcp   mn.google.hcpapps.net
```

## Run the test suite
```shell
make test-e2e-multi TEST_DNS_MANAGED_ZONE_NAME=dev-mz-aws TEST_DNS_NAMESPACES=dns-operator DEPLOYMENT_COUNT=2
```

## Tailing operator pod logs

It's not possible to tail logs across namespaces with `kubectl logs -f`, but third party plugins such as [stern](https://github.com/stern/stern) can be used instead.

```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces
```

If development mode is disabled and json logs are being used you can pipe the logs into jq:
```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces -i 'logger' -o raw | jq .
```

Use jq to select the filter down to the logs you care about:
```shell
kubectl stern -l control-plane=dns-operator-controller-manager --all-namespaces -i 'logger' -o raw | jq 'select(.logger=="dnsrecord_controller")'
```
