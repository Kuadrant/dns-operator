If the credentials that we use in the Azure e2e tests in github have expired, this is how to renew them:

Browse to [here](https://portal.azure.com/#view/Microsoft_AAD_RegisteredApps/ApplicationMenuBlade/~/Credentials/appId/f587998c-3e47-4bed-8f53-c05388b20dae) and generate a new secret. Copy the value into the `aadClientSecret` value below:
```
{"tenantId": "520cf09d-78ff-44ed-a731-abd623e73b09", "subscriptionId": "6a87facd-e4e1-4738-a497-fb325344c3d1", "resourceGroup": "kuadrant", "aadClientId": "f587998c-3e47-4bed-8f53-c05388b20dae", "aadClientSecret": ""}
```
Paste that into the `E2E_AZURE_CREDENTIALS` value [here](https://github.com/Kuadrant/dns-operator/settings/secrets/actions)