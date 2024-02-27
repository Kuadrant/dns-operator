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
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"
)

// DNSRecordSpec defines the desired state of DNSRecord
type DNSRecordSpec struct {
	// +kubebuilder:validation:Required
	// +required
	ManagedZoneRef *ManagedZoneReference `json:"managedZone,omitempty"`
	// +kubebuilder:validation:MinItems=1
	// +optional
	Endpoints []*externaldns.Endpoint `json:"endpoints,omitempty"`
}

// DNSRecordStatus defines the observed state of DNSRecord
type DNSRecordStatus struct {

	// conditions are any conditions associated with the record in the managed zone.
	//
	// If publishing the record fails, the "Failed" condition will be set with a
	// reason and message describing the cause of the failure.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the most recently observed generation of the
	// DNSRecord.  When the DNSRecord is updated, the controller updates the
	// corresponding record in each managed zone.  If an update for a
	// particular zone fails, that failure is recorded in the status
	// condition for the zone so that the controller can determine that it
	// needs to retry the update for that specific zone.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// endpoints are the last endpoints that were successfully published by the provider
	//
	// Provides a simple mechanism to store the current provider records in order to
	// delete any that are no longer present in DNSRecordSpec.Endpoints
	//
	// Note: This will not be required if/when we switch to using external-dns since when
	// running with a "sync" policy it will clean up unused records automatically.
	Endpoints []*externaldns.Endpoint `json:"endpoints,omitempty"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="DNSRecord ready."

// DNSRecord is the Schema for the dnsrecords API
type DNSRecord struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSRecordSpec   `json:"spec,omitempty"`
	Status DNSRecordStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DNSRecordList contains a list of DNSRecord
type DNSRecordList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSRecord `json:"items"`
}

// DNSRecordType is a DNS resource record type.
// +kubebuilder:validation:Enum=CNAME;A
type DNSRecordType string

const (
	// CNAMERecordType is an RFC 1035 CNAME record.
	CNAMERecordType DNSRecordType = "CNAME"

	// ARecordType is an RFC 1035 A record.
	ARecordType DNSRecordType = "A"

	// NSRecordType is a name server record.
	NSRecordType DNSRecordType = "NS"

	DefaultGeo string = "default"
)

// GetRootDomain returns the shortest domain that is shared across all spec.Endpoints dns names.
// Validates that all endpoints share an equal root domain and returns an error if they don't.
func (s *DNSRecord) GetRootDomain() (string, error) {
	domain := ""
	dnsNames := []string{}
	for idx := range s.Spec.Endpoints {
		dnsNames = append(dnsNames, s.Spec.Endpoints[idx].DNSName)
	}
	for idx := range dnsNames {
		if domain == "" {
			domain = dnsNames[0]
			continue
		}
		if len(domain) > len(dnsNames[idx]) {
			domain = dnsNames[idx]
		}
	}

	if domain == "" {
		return "", fmt.Errorf("unable to determine root domain from %v", dnsNames)
	}

	for idx := range dnsNames {
		if !strings.HasSuffix(dnsNames[idx], domain) {
			return "", fmt.Errorf("inconsitent domains, got %s, expected suffix %s", dnsNames[idx], domain)
		}
	}

	return domain, nil
}

func init() {
	SchemeBuilder.Register(&DNSRecord{}, &DNSRecordList{})
}
