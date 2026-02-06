# Exercising DNS Fail-over via groups

## Why

The DNS fail-over via groups allows switching traffic from one set of clusters to a second set of clusters in an outage. 

## How 
### Starting Point
For this example use case the follow configuration will be assumed.
- There are two clusters configured, both with the dns-operator configured to use groups.
- Cluster_A is a member of the initial active group, and Cluster_B is a replica of Cluster_A, but in a currently inactive group.
- Cluster_A and Cluster_B have the same dnsrecords deployed for a set of hosts.
- The dnsrecords on each cluster are configured with IP addresses or CNAMEs that resolve to the service on that cluster.

Cluster_A goes offline for some reason.
Now traffic needs to be diverted from the group that Cluster_A is a member of to the group that Cluster_B is a member of, at the DNS level.

For the example Cluster\_A is configured in group\_1, and Cluster\_B is configured in group\_2.

### Note for CoreDNS users
When using CoreDNS the active groups TXT record can proxy to any user defined TXT.
Meaning `kuadrant-active-groups.<domain>` could point to `company-kuadrant-record-for-groups.<company domain>` TXT record.
In the case of CoreDNS, the record can be read from the Corefile configmap

Within this guide when the `kuadrant-active-groups.<domain>` TXT record is referred, it is the top level TXT record that being talk about, which may be proxied in the CoreDNS configuration.

### Confirming the current group

#### Via CLI
To get which groups are currently configured as the active groups the `kubectl-kuadrant_dns` CLI can be used.
For this we will need 2 pieces of information.
The domain that is covered by the `providerRef` secret, and a reference to the `providerRef` secret.
The `providerRef` has the format of `<namespace>/<name>`.
Using the below command the current group can be confirmed.

```sh
kubectl-kuadrant_dns get-active-groups --domain <domain> --providerRef <namespace>/<name>
```
This will return the list of currently actives groups.
It is possible to have more than one active group.

#### Manually
The currently configured active groups can be checked manually by logging into the DNS provider.
There a TXT record is created listing the active groups.
The record has naming format of `kuadrant-active-groups.<domain>`. 
An example of the record is below.

```txt
Record name: kuadrant-active-groups.<domain>
Record type: TXT
Value: "version=1;groups=group_1&&group_2"
```
This record has more than one active group configured.

As the active groups is stored in a TXT record it is also possible to get the data via a dig command.

```sh
dig kuadrant-active-groups.<domain> TXT +short
```

### Setting the new active group
There are two ways of updating the active groups.
The easiest method is by using the `kubectl-kuadrant_dns` CLI, which is shipped as part of the kuadrantctl, and can work as a plugin to kubectl.
But also the active groups can be modified by updating the TXT record in the DNS provider directly.

#### Adding a new active group via the CLI
In order to configure the groups via the CLI we will need 3 pieces of information.

- GROUP_ID this is the name of the group that is to be added to the list of active groups.
- `<domain>`, root domain of the zone to add the group to.
- `<providerRef>`, reference to the secret with provider credentials. Format: `<namespace>/<name>`.

**Note:** An active connection to the cluster with the `<providerRef>` is assumed.

With this information adding the new group can be done using the following command.

```sh
kubectl-kuadrant_dns add-active-group GROUP_ID --domain <domain> --providerRef <providerRef>

# Sample success message
added group "group_2" to active groups of <domain> zone
```

#### Setting active groups manually
Access to the DNS provider is required.
In the provider there will be an existing active groups TXT record.
This will have a naming format of `kuadrant-active-groups.<domain>`.
The value of the TXT record references a version, and a list of active groups.
List items are separated by `&&`.

```txt
Record name: kuadrant-active-groups.<domain>
Record type: TXT
Value: "version=1;groups=group_1&&group_2"
```

### Removing the downed cluster group
As cluster_A is offline, it is best to disable its group until it can be confirmed to be fully back online.
This can be done in the same ways as adding the group to the active-groups record. 
When a group is removed from the list of active-groups, other groups in the active-groups will tidy up DNS records created by clusters in inactive groups.
It is recommended to always have at least one cluster in an active group.

#### Removing old active group via CLI
In order to remove groups via the CLI three pieces of information is required.

- GROUP_ID this is the name of the group that is to be removed from the list of active groups.
- `<domain>`, root domain of the zone to add the group to.
- `<providerRef>`, reference to the secret with provider credentials. Format: `<namespace>/<name>`.

**Note:** An active connection to the cluster with the `<providerRef>` is assumed.

With this information removing the new group can be done using the following command.

```sh
kubectl-kuadrant_dns remove-active-group GROUP_ID --domain <domain> --providerRef <providerRef>
```

#### Removing the old active group manually
Access to the DNS provider is required.
In the provider the `kuadrant-active-groups.<domain>` TXT record will contain the current list of active groups.
This can be a list of groups that are separated by `&&`.
Remove the group which is wanted to be disabled.

### Record reconciliation
The dns-operator does not watch for changes in the active-groups TXT records.
During the scheduled reconciles of the dns-operator, the dns-operator evaluates the active-groups TXT record, acting as required.
In turn, this means fail-over will not be instant, and requires some time to complete.

In dev preview this delay can be up to 15 minutes by default.
This value can be modified by setting `--max-requeue-time` argument on the dns-operator deployment, or `MAX_REQUEUE_TIME` in the `dns-operator-controller-env` configmap.

### Confirming fail-over successful
To confirm the fail-over has been successful the dnsrecords on the cluster_B can be monitored for a ready status.
The following will list the state of all the dnsrecords on the cluster. 
Optionally the `--watch` flag can be added to the command to get updates on the resources

```sh
kubectl get dnsrecords -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,OWNER_ID:.status.ownerID,GROUP:.status.group,ACTIVE_GROUPS:.status.activeGroups,ACTIVE:.status.active,READY:.status.conditions[?(@.type==\"Ready\")].status"
```

