# DNS Record Delegation

Delegation in context of DNS records is to pass the reconciliation responsibility to a **primary cluster**.
This is a multi cluster feature, with two clusters types, referred to as **primary cluster** and **secondary cluster**.

A **primary cluster** is a cluster that will reconcile delegated dns records into an **authoritative dns record**.
An **authoritative dns record** is a dns record that the dns-operator manages, and consists of all the delegated dns records for a root host.
The **primary cluster** requires a default provider secret, which is labeled with `kuadrant.io/default-provider=true`.
The dns-operator can have the `--delegation-role=primary` adding to the `args`, or `data.DELEGATION_ROLE: primary` to the `dns-operator-controller-env` configmap.
This is not strictly necessary as the default delegation role is primary.
Multi cluster communication is done via a secret with the label `kuadrant.io/multicluster-kubeconfig=true`.
These secrets are created within the same namespace as the dns-operator deployment.
The secret contains a kubeconfig that allow access to a cluster with multi cluster setup, and there will be one secret pre cluster.
The `kubectl-dns` plugin provides a command to help with the secret generation.
See `kubectl-dns add-cluster-secret --help` for more information.
If there are multiply **primary clusters**, each cluster must have the same cluster connection secrets, and a cluster connection secrets to the other **primary cluster**.
The **primary cluster B** will generate an identical **authoritative dns record** to **primary cluster A**

A **secondary cluster** is a cluster that will **not** reconcile delegated dns records.
The **secondary cluster** will do some validation, and status maintaining of delegated dns records, but does not interact with the dns provider.
As the **secondary cluster** does not interact with the dns provider, there is no need for a provider secret.
To configure a **secondary cluster** the dns-opeator deployment requires `--delegation-role=secondary` added to `args`.
This can be configured within the `dns-operator-controller-env` configmap with `data.DELEGATION_ROLE: secondary`.
An important note is a cluster in secondary mode can still reconcile dns records that do not have the delegation field set to true.
When a dns record on a **secondary cluster** is configured without delegation, the dns-operator acts like a normal installation, and requires a provider secret to reconcile the dns record.

The delegation of a dns record is achieved by setting `delegate=true` in the dns record spec.
Due to limitations of multi cluster communication, the `delegate` field is immutable,
Changing this field requires the removal of the dns record the cluster, and recreation with the newer values.
The `delegate=true`, and `providerRef` are mutually exclusive, and can not be set together.
A delegated dns record works in the same manner on a **primary cluster** as a **secondary**.
This allows multiply **primary clusters** to operate on the dns record, but also allow the resigning of clusters roles at a later stage without having to recreate the dns records on the cluster.

