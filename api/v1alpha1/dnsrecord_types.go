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

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

type HealthProtocol string

const HttpProtocol HealthProtocol = "HTTP"
const HttpsProtocol HealthProtocol = "HTTPS"

// HealthCheckSpec configures health checks in the DNS provider.
// By default this health check will be applied to each unique DNS A Record for
// the listeners assigned to the target gateway
type HealthCheckSpec struct {
	// Endpoint is the path to append to the host to reach the expected health check.
	// Must start with "?" or "/", contain only valid URL characters and end with alphanumeric char or "/". For example "/" or "/healthz" are common
	// +kubebuilder:validation:Pattern=`^(?:\?|\/)[\w\-.~:\/?#\[\]@!$&'()*+,;=]+(?:[a-zA-Z0-9]|\/){1}$`
	Endpoint string `json:"endpoint,omitempty"`

	// Port to connect to the host on. Must be either 80, 443 or 1024-49151
	// +kubebuilder:validation:XValidation:rule="self in [80, 443] || (self >= 1024 && self <= 49151)",message="Only ports 80, 443, 1024-49151 are allowed"
	Port *int `json:"port,omitempty"`

	// Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"
	// +kubebuilder:validation:XValidation:rule="self in ['HTTP','HTTPS']",message="Only HTTP or HTTPS protocols are allowed"
	Protocol *HealthProtocol `json:"protocol,omitempty"`

	// FailureThreshold is a limit of consecutive failures that must occur for a host to be considered unhealthy
	// +kubebuilder:validation:XValidation:rule="self > 0",message="Failure threshold must be greater than 0"
	FailureThreshold *int `json:"failureThreshold,omitempty"`
}

type HealthCheckStatus struct {
	Conditions []metav1.Condition       `json:"conditions,omitempty"`
	Probes     []HealthCheckStatusProbe `json:"probes,omitempty"`
}

type HealthCheckStatusProbe struct {
	ID         string             `json:"id"`
	IPAddress  string             `json:"ipAddress"`
	Host       string             `json:"host"`
	Synced     bool               `json:"synced,omitempty"`
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// DNSRecordSpec defines the desired state of DNSRecord
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.ownerID) || has(self.ownerID)", message="OwnerID can't be unset if it was previously set"
// +kubebuilder:validation:XValidation:rule="has(oldSelf.ownerID) || !has(self.ownerID)", message="OwnerID can't be set if it was previously unset"
type DNSRecordSpec struct {
	// ownerID is a unique string used to identify the owner of this record.
	// If unset or set to an empty string the record UID will be used.
	// +kubebuilder:validation:Optional
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="OwnerID is immutable"
	// +kubebuilder:validation:MinLength=6
	// +kubebuilder:validation:MaxLength=36
	OwnerID string `json:"ownerID,omitempty"`

	// rootHost is the single root for all endpoints in a DNSRecord.
	// it is expected all defined endpoints are children of or equal to this rootHost
	// Must contain at least two groups of valid URL characters separated by a "."
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^(?:[\w\-.~:\/?#[\]@!$&'()*+,;=]+)\.(?:[\w\-.~:\/?#[\]@!$&'()*+,;=]+)$`
	RootHost string `json:"rootHost"`

	// managedZone is a reference to a ManagedZone instance to which this record will publish its endpoints.
	ManagedZoneRef *ManagedZoneReference `json:"managedZone"`

	// endpoints is a list of endpoints that will be published into the dns provider.
	// +kubebuilder:validation:MinItems=1
	// +optional
	Endpoints []*externaldns.Endpoint `json:"endpoints,omitempty"`

	// +optional
	HealthCheck *HealthCheckSpec `json:"healthCheck,omitempty"`
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

	// QueuedAt is a time when DNS record was received for the reconciliation
	QueuedAt metav1.Time `json:"queuedAt,omitempty"`

	// QueuedFor is a time when we expect a DNS record to be reconciled again
	QueuedFor metav1.Time `json:"queuedFor,omitempty"`

	// ValidFor indicates duration since the last reconciliation we consider data in the record to be valid
	ValidFor string `json:"validFor,omitempty"`

	// WriteCounter represent a number of consecutive write attempts on the same generation of the record.
	// It is being reset to 0 when the generation changes or there are no changes to write.
	WriteCounter int64 `json:"writeCounter,omitempty"`

	// endpoints are the last endpoints that were successfully published by the provider
	//
	// Provides a simple mechanism to store the current provider records in order to
	// delete any that are no longer present in DNSRecordSpec.Endpoints
	//
	// Note: This will not be required if/when we switch to using external-dns since when
	// running with a "sync" policy it will clean up unused records automatically.
	Endpoints []*externaldns.Endpoint `json:"endpoints,omitempty"`

	HealthCheck *HealthCheckStatus `json:"healthCheck,omitempty"`

	// ownerID is a unique string used to identify the owner of this record.
	OwnerID string `json:"ownerID,omitempty"`

	// DomainOwners is a list of all the owners working against the root domain of this record
	DomainOwners []string `json:"domainOwners,omitempty"`
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

const WildcardPrefix = "*."

func (s *DNSRecord) Validate() error {
	root := s.Spec.RootHost
	if len(s.Spec.Endpoints) == 0 {
		return fmt.Errorf("no endpoints defined for DNSRecord. Nothing to do")
	}

	root, _ = strings.CutPrefix(root, WildcardPrefix)

	rootEndpointFound := false
	for _, ep := range s.Spec.Endpoints {
		if !strings.HasSuffix(ep.DNSName, root) {
			return fmt.Errorf("invalid endpoint discovered %s all endpoints should be equal to or end with the rootHost %s", ep.DNSName, root)
		}
		if !rootEndpointFound {
			//check original root
			if ep.DNSName == s.Spec.RootHost {
				rootEndpointFound = true
			}
		}
	}
	if !rootEndpointFound {
		return fmt.Errorf("invalid endpoint set. rootHost is set but found no endpoint defining a record for the rootHost %s", root)
	}
	return nil
}

// GetUIDHash returns a hash of the current records UID with a fixed length of 8.
func (s *DNSRecord) GetUIDHash() string {
	return hash.ToBase36HashLen(string(s.GetUID()), 8)
}

func init() {
	SchemeBuilder.Register(&DNSRecord{}, &DNSRecordList{})
}
