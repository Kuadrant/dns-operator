# Migrating away from DNS Groups

## How

### Remove the group value from the operator
Modify the `dns-operator-controller-env` configmap in the dns-operator namespace and remove the `GROUP` entry.

If the dns-operator is configured to use a group but the configmap does not have the group configured, the configuration was added directly to the dns-operator deployment.
Edit the deployment.
From `spec.containers[name=manager].args` remove the argument `--group=<group>`.

### Restart the operator
After modifying the `dns-operator-controller-env` configmap the dns-operator needs to be restarted to roll out the changes.

```sh
kubectl --namespace <namespace> rollout restart deployment dns-operator-controller-manager
```

Check the controller logs to ensure the group has been removed.
The following command can be used; the "using group:" output is expected to be empty.

```sh
NS=<namespace> kubectl logs $(kubectl get pods -l control-plane=dns-operator-controller-manager --sort-by=.metadata.creationTimestamp -o name --namespace $NS | tail -n 1) --namespace $NS | head -n 20 | grep group
```

### Verify DNSRecord statuses
The removal of groups will not happen when the operator restarts, but after the `Valid for` time period has passed.
By default, this can be up to 15 minutes.

The following command can be used to monitor the group assigned to a DNSRecord.

```sh
kubectl get dnsrecords -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,OWNER_ID:.status.ownerID,GROUP:.status.group,ACTIVE_GROUPS:.status.activeGroups,ACTIVE:.status.active,READY:.status.conditions[?(@.type==\"Ready\")].status" --watch
```

### Confirm zone is as expected
Once reconciliation has fully completed, the group references in the TXT records should be removed from the DNS provider.

### Delete active-groups TXT record (Optional)
If there are no more dns-operators using groups, it is safe to remove the `kuadrant-active-groups.<domain>` TXT record from the DNS provider.
This can be done directly within the DNS provider, or by using the `kubectl-kuadrant_dns` CLI.

Within the DNS provider, remove the TXT record for the domain.
The naming convention is `kuadrant-active-groups.<domain>`.

Alternatively, use the CLI with the following command:

```sh
kubectl-kuadrant_dns remove-active-group GROUP --domain <domain> --providerRef <providerRef>
```

The `<providerRef>` is a reference to the secret used by the dns-operator to connect to the DNS provider, in the format `<namespace>/<name>`.

#### Note for CoreDNS users
When using CoreDNS the active groups TXT record can proxy to any user defined TXT.
Meaning `kuadrant-active-groups.<domain>` could point to `company-kuadrant-record-for-groups.<company domain>` TXT record.
In the case of CoreDNS, the record can be read from the Corefile configmap

Within this guide when the `kuadrant-active-groups.<domain>` TXT record is referred, it is the top level TXT record that being talked about, which may be proxied in the CoreDNS configuration.

