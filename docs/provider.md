# Configuring a DNS Provider 

In order to be able to interact with supported DNS providers, Kuadrant needs a credential that it can use.

Credentials are expected to reside in a secret. The secret name can be specified in the `DNSRecord.Spec.ProviderRef` or the secret can have `kuadrant.io/default-provider=true` label. The secret that got assigned to the DNSRecord will be specified in the `Status.ProfiverRef`.

## Supported Providers

Kuadrant Supports the following DNS providers currently

- AWS Route 53 (aws)
- Google Cloud DNS (gcp)
- Azure (azure)
- CoreDNS (coredns)

### AWS Route 53 Provider

Kuadrant expects a `Secret` with a credential. Below is an example for AWS Route 53. It is important to set the secret type to `aws`:


```bash
kubectl create secret generic my-aws-credentials \
  --namespace=kuadrant-dns-system \
  --type=kuadrant.io/aws \
  --from-literal=AWS_ACCESS_KEY_ID=XXXX \
  --from-literal=AWS_REGION=eu-west-1 \
  --from-literal=AWS_SECRET_ACCESS_KEY=XXX
```

| Key                      | Example Value           | Description                                           |
|--------------------------|-------------------------|-------------------------------------------------------|
| `AWS_REGION`             | `eu-west-1`             | AWS Region                                            |
| `AWS_ACCESS_KEY_ID`      | `XXXX`                  | AWS Access Key ID (see note on permissions below)     |
| `AWS_SECRET_ACCESS_KEY`  | `XXXX`                  | AWS Secret Access Key                                 |

#### AWS IAM Permissions Required 
We have tested using the available policy `AmazonRoute53FullAccess` however it should also be possible to restrict the credential down to a particular zone. More info can be found in the AWS docs:

[https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/access-control-managing-permissions.html](https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/access-control-managing-permissions.html)

By default, Kuadrant will list the available zones and find the matching zone based on the listener host in the gateway listener. If it finds more than one matching zone for a given listener host, it will not update any of those zones. 
When providing a credential you should limit that credential down to just have write access to the zones you want Kuadrant to manage. Below is an example of a an AWS policy for doing this type of thing:

```
{
    "Version": "2012-10-17",
    "Statement": [
        {
            "Sid": "VisualEditor0",
            "Effect": "Allow",
            "Action": [
                "route53:ListTagsForResources",
                "route53:GetHealthCheckLastFailureReason",
                "route53:GetHealthCheckStatus",
                "route53:GetChange",
                "route53:GetHostedZone",
                "route53:ChangeResourceRecordSets",
                "route53:ListResourceRecordSets",
                "route53:GetHealthCheck",
                "route53:UpdateHostedZoneComment",
                "route53:UpdateHealthCheck",
                "route53:CreateHealthCheck",
                "route53:DeleteHealthCheck",
                "route53:ListTagsForResource",
                "route53:ListHealthChecks",
                "route53:GetGeoLocation",
                "route53:ListGeoLocations",
                "route53:ListHostedZonesByName",
                "route53:GetHealthCheckCount"
            ],
            "Resource": [
                "arn:aws:route53:::hostedzone/Z08187901Y93585DDGM6K",
                "arn:aws:route53:::healthcheck/*",
                "arn:aws:route53:::change/*"
            ]
        },
        {
            "Sid": "VisualEditor1",
            "Effect": "Allow",
            "Action": [
                "route53:ListHostedZones"
            ],
            "Resource": "*"
        }
    ]
}
```


### Google Cloud DNS Provider

Kuadant expects a secret with a credential. Below is an example for Google DNS. It is important to set the secret type to `gcp`:

```bash
kubectl create secret generic my-test-gcp-credentials \
  --namespace=kuadrant-dns-system \
  --type=kuadrant.io/gcp \
  --from-literal=PROJECT_ID=xxx \
  --from-file=GOOGLE=$HOME/.config/gcloud/application_default_credentials.json
```

| Env Var      | Example Value                                                                                  | Description                                                                                                           |
|--------------|------------------------------------------------------------------------------------------------|-----------------------------------------------------------------------------------------------------------------------|
| `GOOGLE`     | `{"client_id": "***","client_secret": "***","refresh_token": "***","type": "authorized_user"}` | This is the JSON created from either the credential created by the `gcloud` CLI, or the JSON from the Service account |
| `PROJECT_ID` | `my_project_id`                                                                                | ID to the Google project                                                                                              |


#### Google Cloud DNS Access permissions required

We have tested with the `dns.admin` role. See for more details:

[https://cloud.google.com/dns/docs/access-control#dns.admin](https://cloud.google.com/dns/docs/access-control#dns.admin)


### Azure Cloud DNS Provider

Kuadrant expects a `Secret` with a credential. Below is an example for Azure. It is important to set the secret type to `azure`:

We recommend creating a new service principal for managing DNS. [Azure Service Principal Docs](https://learn.microsoft.com/en-us/entra/identity-platform/app-objects-and-service-principals?tabs=browser#service-principal-object)

```
# Create the service principal
$ DNS_NEW_SP_NAME=kuadrantDnsPrincipal
$ DNS_SP=$(az ad sp create-for-rbac --name $DNS_NEW_SP_NAME)
$ DNS_SP_APP_ID=$(echo $DNS_SP | jq -r '.appId')
$ DNS_SP_PASSWORD=$(echo $DNS_SP | jq -r '.password')

```


#### Azure Cloud DNS Access permissions required


You will need to grant read and contributor access to the zone(s) you want managed for the service principal you are using.


1)  fetch DNS id used to grant access to the service principal

    ```bash
    DNS_ID=$(az network dns zone show --name example.com \
     --resource-group ExampleDNSResourceGroup --query "id" --output tsv)
    
    # get yor resource group id
    
    RESOURCE_GROUP_ID=az group show --resource-group ExampleDNSResourceGroup | jq ".id" -r
    ``` 

2) provide reader access to the resource group
    ```bash
    az role assignment create --role "Reader" --assignee $DNS_SP_APP_ID --scope $DNS_ID
    ```

3) provide contributor access to DNS Zone itself
    ```bash
    $ az role assignment create --role "Contributor" --assignee $DNS_SP_APP_ID --scope $DNS_ID
    ```

4) As we are setting up advanced traffic rules for GEO and Weighted responses, you will also need to grant traffic manager access:

    ```bash
    az role assignment create --role "Traffic Manager Contributor" --assignee $DNS_SP_APP_ID --scope $RESOURCE_GROUP_ID
    ```
    
    ```bash
    cat <<-EOF > /local/path/to/azure.json
    {
      "tenantId": "$(az account show --query tenantId -o tsv)",
      "subscriptionId": "$(az account show --query id -o tsv)",
      "resourceGroup": "ExampleDNSResourceGroup",
      "aadClientId": "$DNS_SP_APP_ID",
      "aadClientSecret": "$DNS_SP_PASSWORD"
    }
    EOF
    ```

5) Finally setup the secret with the credential azure.json file

    ```bash
    kubectl create secret generic my-test-azure-credentials \
      --namespace=kuadrant-dns-system \
      --type=kuadrant.io/azure \
      --from-file=azure.json=/local/path/to/azure.json
    ```

### CoreDNS Provider

CoreDNS provider enables self-hosted DNS using CoreDNS in Kubernetes. Unlike cloud providers, it requires configuration (zones and nameservers) rather than only supplying authentication credentials.

Kuadrant expects a `Secret` with zone and nameserver configuration. Below is an example for CoreDNS. It is important to set the secret type to `coredns`:

```bash
kubectl create secret generic my-coredns-credentials \
  --namespace=kuadrant-dns-system \
  --type=kuadrant.io/coredns \
  --from-literal=ZONES=example.com \
  --from-literal=NAMESERVERS=ns1.example.com,ns2.example.com
```

| Key           | Example Value                      | Description                                    |
|---------------|------------------------------------|------------------------------------------------|
| `ZONES`       | `example.com`                      | (Required) DNS zone this provider manages                 |
| `NAMESERVERS` | `ns1.example.com,ns2.example.com`  | (Optional) Comma-separated list of authoritative nameservers for the zone. Only required for DNS Groups feature (active-passive failover). |

**Note:** The CoreDNS provider does not require authentication credentials. It only needs to know which zone it manages (`ZONES` field is required). 

For setting the secret as the default provider in a namespace, add the `kuadrant.io/default-provider=true` label:

```bash
kubectl label secret my-coredns-credentials \
  -n kuadrant-dns-system \
  kuadrant.io/default-provider=true
```
