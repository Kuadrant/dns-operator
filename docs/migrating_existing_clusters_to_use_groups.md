# Migrating Existing Clusters To Use Groups

## Why use Groups
By using groups it allows for planned migrations, and failovers in unexpected outages of deployments.

For the planned migrations, new deployment can be configured with the required infrastructure. 
Once operational, the active group can be switched to the newer deployment.

Unexpected failures do happen, and by planning ahead with having a replica deployment of the production deployment.
The groups will allow for a timely switch of DNS traffic from the failing deployment to the replica deployment. 

## Terminology
### Group
A group is a method of tagging a dns-operator to reconcile on resources.
The dns-operator can only be part of one group at a time.
Cluster of dns-operator can be enabled, and disable by modifying TXT records with a DNS provider.

### Active groups
The active groups are the groups that are stated in the active-groups TXT records.
Dns-operators that are configured with a group that is in the active-groups will reconcile the dnsrecords on that clusters, and any dnsrecords delegated in a multi cluster setup.

Dns-operators in the active groups will tidy up orphaned DNS records from any dns-operator that may now be part of an inactive group.

**Note:** In a multi cluster setup, the secondary clusters do not have to be part of a group.
But it is recommended to have secondary clusters in the same group the primary cluster is in.


### Inactive group
Dns-operators configured with a group that is not in the active-groups are considered to be part of the inactive group.
DNS records in the provider that were previously created by the dns-operator that is now in the inactive group while be removed by the dns-operators that are in the active groups.

Inactive group dns-operators do not clean up DNS records related to the resources they manage. 
Dns-operators in an inactive group may be part of an outage, and can not be trusted to be able to clean up their resources.
For this reason dns-operators in inactive groups don't attempt any clean up.

### Ungrouped. 
When a dns-operator does not have a group assigned it is regarded as being ungrouped.
A ungrouped dns-operator will reconcile all dnsrecords that the operator has access to no matter what groups are configured in the active groups.

Ungrouped dns-operators will not unpublish any records not related to their resources.
This includes DNS records created by now inactive group dns-operators which do require cleaning up.

## How To Configure Existing Cluster
### Starting point 
There is an existing cluster configured with dns-operator deployed without groups configured. 
A long with the dns-operator deployment, dns-records have being reconciled, and are in a ready state.

### Create the active-groups TXT record
The active-groups record list is a TXT record that is created in the provider.
The TXT record contains a list of groups that are active.

By creating the TXT record before updating the dns-operator the reconcile of DNS records will not cause unexpected service interruptions. 

#### Creation of active-groups TXT record via the CLI.
Using the `kubectl-kuadrant_dns` CLI the active groups can be easily created.
The CLI can be used as a plugin in the kuadrantctl, or as a plugin in the kubectl.

To create the record three pieces of information is required.
- Group ID to be added to the active-groups TXT record
- providerRef, this is the secret used by the dns-operator to connect to the provider. Format required \<namespace\>/\<name\>.
- domain, this is the route domain for the zone.

With this information the active-groups TXT record is created with the command below.
```sh
kubectl-kuadrant_dns add-active-group GROUP_ID --providerRef <namespace>/<name> --domain <domain>
```
This command will also add a new active group to an existing active-groups TXT record.

Once the active-groups TXT record has being created, a new TXT record will be created in the provider.
The TXT record has a naming convention of `kuadrant-active-groups.<domain>`.
The TXT record structure is as follows.
Fields are separated by `;`.
A `version` field is added, current version = 1.
The `groups` field contains a list of active groups.
Group IDs are separated by `&&`.

Sample TXT Record
```txt
Record name: kuadrant-active-groups.<domain>
Record type: TXT
Value: "version=1;groups=GROUP_ID1&&GROUP_ID2"
```

#### Creation of active-groups TXT record manual.
As the active-groups are maintained as a TXT record, it is possible to create, and maintain the record manually. 
This assumes the users has access to the DNS provider, and has permission to create TXT records in a given zone.
To create the active-groups the following bits of information is required.
- Group ID to be added to the active-groups TXT record
- domain, this is the route domain for the zone.

There are strict requirements on the active-groups TXT record.
It must be named as: `kuadrant-active-groups.<domain>`.
The record type must be TXT.
Finally, the fields in the record must contain a `version` and `groups` field.
Fields are separated by `;`.
The `groups` field is a list of active groups, and the entries are separated by `&&`.

Below is a sample TXT record.

```txt
Record name: kuadrant-active-groups.<domain>
Record type: TXT
Value: "version=1;groups=GROUP_ID1&&GROUP_ID2"
```

### Checking the active groups list
Before moving on to configuring the dns-operator to be part of a group, it is good practice to ensure the correct groups are active.
This can be easily done via the `kubectl-kuadrant_dns` CLI.

To do this the follow information is required.
- providerRef, this is the secret used by the dns-operator to connect to the provider. Format required \<namespace\>/\<name\>.
- domain, this is the route domain for the zone.
With this information the CLI command to use is.
```sh
kubectl-kuadrant_dns get-active-groups --providerRef <namespace>/<name> --domain <domain>
```

### Configuring the dns-operator to be part of a group
The dns-operator can be added to a group via an env var set in the `dns-operator-controller-env` configmap.
This configmap exists in the same namespace as the dns-operator.

By using kubectl patch, the configmap can be updated with affecting other env vars that may be configured.
```sh
kubectl patch configmap dns-operator-controller-env --namespace <namespace> --type merge --patch '{"data:{"GROUP":"<GROUP ID>"}}'
```
Once the configmap has being patched, the deployment needs to be redeployed for the changes to take effect.
```sh
kubectl rollout restart deployment dns-operator-controller-manager --namespace <namespace>
```

### Confirming the dns-operator is configured correctly

It can take some time for the all the dnsrecords to be fully reconciled to use the new group setting.
However, by checking the logs of the operator, it can be confirmed that the group was configured correctly.
There is are two setup log messages that confirm the group was correctly configured.
These log messages can be retrieved from the cluster with the following command.
```sh
NS=<namespace> kubectl logs $(kubectl get pods -l control-plane=dns-operator-controller-manager --sort-by=.metadata.creationTimestamp -o name --namespace $NS | tail -n 1) --namespace $NS | head -n 20 | grep group

# expected output
{"level":"info","ts":"2006-1-2T15:4:5","logger":"setup","msg":"overriding group flag with \"group1\" value"}
{"level":"info","ts":"2006-1-2T15:4:5","logger":"setup","msg":"using group: group1"}
```

When the reconcile finally completes on the dnsrecords in the status block there will be mention of the group.
The current group of the dnsrecords can be checked with the following command.
```sh
kubectl get dnsrecords -A -o custom-columns="NAMESPACE:.metadata.namespace,NAME:.metadata.name,OWNER_ID:.status.ownerID,STATUS.GROUP:.status.group"
```

The last place that changes can be checked is the TXT record for the dnsrecords in the provider.
In the provider a TXT record is created with the owner ID of the related dnsrecord.
Each of these TXT records will state what group they are part of.

**Note:** If using the above command will list all the owner IDs for the dnsrecords however, if cluster delegation is used not all dnsrecords will have TXT created 

The TXT records that were created will have the following fields, and structure.
```txt
heritage=external-dns
external-dns/group=<GROUP ID>
external-dns/owner=<ownerID>
external-dns/targets=...
external-dns/version=1
```
## Beyond the initial configuration
After configuring the dns-operator to be a part of a group there is some next possible steps.

It is possible to have more than one active group at a time.
This allows finer control over the networking design, and relicences of the infrastructure.

When a dns-operator is configured with a group it is possible to change the group that dns-operator is a part of.
All the dnsrecords, and TXT related to does records will be updated to use the new group.
Changing the group from an active group to an inactive group at controller level is currently unsupported.
See known issues for more details.

## Known Issues
- [DNS Operator - always write groupID to provider](https://github.com/Kuadrant/dns-operator/issues/637)
