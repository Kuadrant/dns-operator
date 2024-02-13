# The ManagedZone Custom Resource Definition (CRD)

- [ManagedZone](#ManagedZone)
- [ManagedZoneSpec](#managedzonespec)
- [ManagedZoneStatus](#managedzonestatus)

## ManagedZone

| **Field** | **Type**                            | **Required** | **Description**                                |
|-----------|-------------------------------------|:------------:|------------------------------------------------|
| `spec`    | [ManagedZoneSpec](#managedzonespec) |    Yes       | The specification for ManagedZone custom resource |
| `status`  | [ManagedZoneStatus](#managedzonestatus) |      No      | The status for the custom resource             | 

## ManagedZoneSpec

| **Field**              | **Type**                                       | **Required** | **Description**                                                          |
|------------------------|------------------------------------------------|:------------:|--------------------------------------------------------------------------|
| `id`                   | String                                         |      No      | ID is the provider assigned id of this zone (i.e. route53.HostedZone.ID) | 
| `domainName`           | String                                         |     Yes      | Domain name of this ManagedZone                                          |
| `description`          | String                                         |      No      | Description for this ManagedZone                                         |
| `parentManagedZone`    | [ManagedZoneReference](#managedzonereference)  |      No      | Reference to another managed zone that this managed zone belongs to      |
| `dnsProviderSecretRef` | [SecretRef](#secretref)                        |      No      | Reference to a secret containing provider credentials                    |

## ManagedZoneReference

| **Field**    | **Type** | **Required** | **Description**         |
|--------------|----------|:------------:|-------------------------|
| `name`       | String   |     Yes      | Name of a managed zone  | 

## SecretRef

| **Field**    | **Type** | **Required** | **Description**         |
|--------------|----------|:------------:|-------------------------|
| `name`       | String   |     Yes      | Name of the secret      | 
| `namespace`  | String   |     Yes      | Namespace of the secret | 


## ManagedZoneStatus

| **Field**            | **Type**                                                                                             | **Description**                                                                                                                    |
|----------------------|------------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                               | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec |
| `conditions`         | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition)  | List of conditions that define that status of the resource                                                                         |
| `id`                 | String                                                                                               | The ID assigned by this provider for this zone (i.e. route53.HostedZone.ID)                                                        |
| `recordCount`        | Number                                                                                               | The number of records in the provider zone                                                                                         |
| `nameServers`        | []String                                                                                             | The NameServers assigned by the provider for this zone (i.e. route53.DelegationSet.NameServers)                                    |
