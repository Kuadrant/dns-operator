/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import corev1 "k8s.io/api/core/v1"

const (
	// SecretTypeKuadrantAWS contains data needed for aws(route53) authentication and configuration.
	//
	// Required fields:
	// - Secret.Data["AWS_ACCESS_KEY_ID"] - aws access key id
	// - Secret.Data["AWS_SECRET_ACCESS_KEY"] - aws secret access key
	SecretTypeKuadrantAWS corev1.SecretType = "kuadrant.io/aws"

	// AWSAccessKeyIDKey is the key of the required AWS access key id for SecretTypeKuadrantAWS provider secrets
	AWSAccessKeyIDKey = "AWS_ACCESS_KEY_ID"
	// AWSSecretAccessKeyKey is the key of the required AWS secret access key for SecretTypeKuadrantAWS provider secrets
	AWSSecretAccessKeyKey = "AWS_SECRET_ACCESS_KEY"
	// AWSRegionKey is the key of the optional region for SecretTypeKuadrantAWS provider secrets
	AWSRegionKey = "AWS_REGION"

	// SecretTypeKuadrantGCP contains data needed for gcp(google cloud dns) authentication and configuration.
	//
	// Required fields:
	// - Secret.Data["GOOGLE"] - json formatted google credentials string
	// - Secret.Data["PROJECT_ID"] - google project id
	SecretTypeKuadrantGCP corev1.SecretType = "kuadrant.io/gcp"

	// GoogleJsonKey is the key of the required json formatted credentials string for SecretTypeKuadrantGCP provider secrets
	GoogleJsonKey = "GOOGLE"
	// GoogleProjectIDKey is the key of the required project id for SecretTypeKuadrantGCP provider secrets
	GoogleProjectIDKey = "PROJECT_ID"

	// SecretTypeKuadrantAzure contains data needed for azure authentication and configuration.
	//
	// Required fields:
	// - Secret.Data["azure.json"] - json formatted azure credentials string
	SecretTypeKuadrantAzure corev1.SecretType = "kuadrant.io/azure"

	// AzureJsonKey is the key of the required data for SecretTypeDockerConfigJson provider secrets
	AzureJsonKey = "azure.json"

	// SecretTypeKuadrantInmemory contains data needed for inmemory configuration.
	SecretTypeKuadrantInmemory corev1.SecretType = "kuadrant.io/inmemory"

	// InmemInitZonesKey is the key of the optional comma separated list of zone names to initialise in the SecretTypeKuadrantInmemory provider secrets
	InmemInitZonesKey = "INMEM_INIT_ZONES"

	SecretTypeKuadrantCoreDNS corev1.SecretType = "kuadrant.io/coredns"

	DefaultProviderSecretLabel = "kuadrant.io/default-provider"

	// This label defines the GVR to use for the endpoints provider
	EndpointGVRKey = "ENDPOINT_GVR"

	// The GVR to use in the endpoint provider is none are provided
	DefaultEndpointGVR = "kuadrant.io/v1alpha1.dnsrecords"

	// This is a label selector for the endpoint provider to find available zones
	EndpointLabelSelectorKey = "ENDPOINT_ZONE_RECORD_LABEL"

	// Default label selector used by the endpoint provider
	DefaultLabelSelector = "kuadrant.io/zone-record=true"

	// SecretTypeKuadrantEndpoint contains required fields for setting up an endpoint provider.
	//
	// no required fields, optional fields are:
	// - ENDPOINT_GVR (defaults to: kuadrant.io/v1alpha1.dnsrecords)
	// - ENDPOINT_ZONE_RECORD_LABEL (defaults to: kuadrant.io/zone-record=true)
	SecretTypeKuadrantEndpoint corev1.SecretType = "kuadrant.io/endpoint"
)

type ProviderRef struct {
	Name string `json:"name"`
}

// +kubebuilder:object:generate=false
type ProviderAccessor interface {
	GetNamespace() string
	GetProviderRef() ProviderRef
	GetRootHost() string
	IsDelegating() bool
}
