# The DNSRecord Custom Resource Definition (CRD)

- [DNSRecord](#DNSRecord)
- [DNSRecordSpec](#dnsrecordspec)
- [DNSRecordStatus](#dnsrecordstatus)

## DNSRecord

| **Field** | **Type**                            | **Required** | **Description**                                  |
|-----------|-------------------------------------|:------------:|--------------------------------------------------|
| `spec`    | [DNSRecordSpec](#dnsrecordspec)     |     Yes      | The specification for DNSRecord custom resource  |
| `status`  | [DNSRecordStatus](#dnsrecordstatus) |      No      | The status for the custom resource               | 

## DNSRecordSpec

| **Field**        | **Type**                                                                                 | **Required** | **Description**                                                                     |
|------------------|------------------------------------------------------------------------------------------|:------------:|-------------------------------------------------------------------------------------|
| `ownerID`        | String                                                                                   |      No      | Unique string used to identify the owner of this record                             | 
| `rootHost`       | String                                                                                   |      No      | Single root host of all endpoints in a DNSRecord                                    |
| `managedZone`    | [ManagedZoneReference](#managedzonereference)                                            |     Yes      | Reference to a ManagedZone instance to which this record will publish its endpoints |
| `endpoints`      | [][ExternalDNS Endpoint](https://pkg.go.dev/sigs.k8s.io/external-dns/endpoint#Endpoint)  |      No      | Endpoints to manage in the dns provider                                             |
| `healthCheck`    | [HealthCheckSpec](#healthcheckspec)                                                      |      No      | Health check configuration                                                          |

## ManagedZoneReference

| **Field**    | **Type** | **Required** | **Description**         |
|--------------|----------|:------------:|-------------------------|
| `name`       | String   |     Yes      | Name of a managed zone  | 

## HealthCheckSpec

| **Field**          | **Type**   | **Required** | **Description**                                                                                           |
|--------------------|------------|:------------:|-----------------------------------------------------------------------------------------------------------|
| `endpoint`         | String     |     Yes      | Endpoint is the path to append to the host to reach the expected health check                             | 
| `port`             | Number     |     Yes      | Port to connect to the host on                                                                            | 
| `protocol`         | String     |     Yes      | Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"                           | 
| `failureThreshold` | Number     |     Yes      | FailureThreshold is a limit of consecutive failures that must occur for a host to be considered unhealthy | 


## DNSRecordStatus

| **Field**            | **Type**                                                                                            | **Description**                                                                                                                    |
|----------------------|-----------------------------------------------------------------------------------------------------|------------------------------------------------------------------------------------------------------------------------------------|
| `observedGeneration` | String                                                                                              | Number of the last observed generation of the resource. Use it to check if the status info is up to date with latest resource spec |
| `conditions`         | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition) | List of conditions that define the status of the resource                                                                          |
| `queuedAt`           | [Kubernetes meta/v1.Time](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Time)             | QueuedAt is a time when DNS record was received for the reconciliation                                                             |
| `queuedFor`          | [Kubernetes meta/v1.Time](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Time)             | QueuedFor is a time when we expect a DNS record to be reconciled again                                                             |
| `validFor`           | String                                                                                              | ValidFor indicates duration since the last reconciliation we consider data in the record to be valid                               |
| `writeCounter`       | Number                                                                                              | WriteCounter represent a number of consecutive write attempts on the same generation of the record                                 |
| `endpoints`          | [][ExternalDNS Endpoint](https://pkg.go.dev/sigs.k8s.io/external-dns/endpoint#Endpoint)             | Endpoints are the last endpoints that were successfully published by the provider                                                  |
| `healthCheck`        | [HealthCheckStatus](#healthcheckstatus)                                                             | Health check status                                                                                                                |

## HealthCheckStatus

| **Field**    | **Type**                                                                                            | **Description**                                                 |
|--------------|-----------------------------------------------------------------------------------------------------|-----------------------------------------------------------------|
| `conditions` | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the health checks |
| `probes`     | [][HealthCheckStatusProbe](#healthcheckstatusprobe)                                                 | Health check Probe status                                       |

## HealthCheckStatusProbe

| **Field**    | **Type**                                                                                            | **Description**                                         |
|--------------|-----------------------------------------------------------------------------------------------------|---------------------------------------------------------|
| `id`         | String                                                                                              | The health check id                                     |
| `ipAddress`  | String                                                                                              | The ip address being monitored                          |
| `host`       | String                                                                                              | The host being monitored                                |
| `synced`     | Boolean                                                                                             | Synced                                                  |
| `conditions` | [][Kubernetes meta/v1.Condition](https://pkg.go.dev/k8s.io/apimachinery/pkg/apis/meta/v1#Condition) | List of conditions that define that status of the probe |
