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

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// ManagedZoneReference holds a reference to a ManagedZone
type ManagedZoneReference struct {
	// `name` is the name of the managed zone.
	// Required
	Name string `json:"name"`
}

// ManagedZoneSpec defines the desired state of ManagedZone
type ManagedZoneSpec struct {
	// id is the provider assigned id of this  zone (i.e. route53.HostedZone.ID).
	// +optional
	ID string `json:"id,omitempty"`

	//domainName of this ManagedZone
	// +kubebuilder:validation:Pattern=`^(([a-zA-Z0-9]|[a-zA-Z0-9][a-zA-Z0-9\-]*[a-zA-Z0-9])\.)*([A-Za-z0-9]|[A-Za-z0-9][A-Za-z0-9\-]*[A-Za-z0-9])$`
	DomainName string `json:"domainName"`

	//description for this ManagedZone
	Description string `json:"description"`

	// parentManagedZone reference to another managed zone that this managed zone belongs to.
	// +optional
	ParentManagedZone *ManagedZoneReference `json:"parentManagedZone,omitempty"`

	// dnsProviderSecretRef reference to a secret containing credentials to access a dns provider.
	SecretRef ProviderRef `json:"dnsProviderSecretRef"`
}

// ManagedZoneStatus defines the observed state of a Zone
type ManagedZoneStatus struct {
	// List of status conditions to indicate the status of a ManagedZone.
	// Known condition types are `Ready`.
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the most recently observed generation of the ManagedZone.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// The ID assigned by this provider for this zone (i.e. route53.HostedZone.ID)
	// +optional
	ID string `json:"id,omitempty"`

	// The number of records in the provider zone
	// +optional
	RecordCount int64 `json:"recordCount,omitempty"`

	// The NameServers assigned by the provider for this zone (i.e. route53.DelegationSet.NameServers)
	// +optional
	NameServers []*string `json:"nameServers,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Domain Name",type="string",JSONPath=".spec.domainName",description="Domain of this Managed Zone"
//+kubebuilder:printcolumn:name="ID",type="string",JSONPath=".status.id",description="The ID assigned by this provider for this zone ."
//+kubebuilder:printcolumn:name="Record Count",type="string",JSONPath=".status.recordCount",description="Number of records in the provider zone."
//+kubebuilder:printcolumn:name="NameServers",type="string",JSONPath=".status.nameServers",description="The NameServers assigned by the provider for this zone."
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="Managed Zone ready."

// ManagedZone is the Schema for the managedzones API
type ManagedZone struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   ManagedZoneSpec   `json:"spec,omitempty"`
	Status ManagedZoneStatus `json:"status,omitempty"`
}

func (mz *ManagedZone) GetProviderRef() ProviderRef {
	return mz.Spec.SecretRef
}

//+kubebuilder:object:root=true

// ManagedZoneList contains a list of ManagedZone
type ManagedZoneList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedZone `json:"items"`
}

type ManagedHost struct {
	Subdomain   string
	Host        string
	ManagedZone *ManagedZone
	DnsRecord   *DNSRecord
}

func init() {
	SchemeBuilder.Register(&ManagedZone{}, &ManagedZoneList{})
}
