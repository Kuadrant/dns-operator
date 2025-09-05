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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

type Protocol string

const HttpProtocol Protocol = "HTTP"
const HttpsProtocol Protocol = "HTTPS"
const AuthoritativeRecordLabel = "kuadrant.io/authoritative-record"
const AuthoritativeRecordHashLabel = "kuadrant.io/authoritative-record-hash"

// HealthCheckSpec configures health checks in the DNS provider.
// By default this health check will be applied to each unique DNS A Record for
// the listeners assigned to the target gateway
type HealthCheckSpec struct {
	// Port to connect to the host on. Must be either 80, 443 or 1024-49151
	// Defaults to port 443
	// +kubebuilder:validation:XValidation:rule="self in [80, 443] || (self >= 1024 && self <= 49151)",message="Only ports 80, 443, 1024-49151 are allowed"
	// +kubebuilder:default=443
	Port int `json:"port,omitempty"`

	// Path is the path to append to the host to reach the expected health check.
	// Must start with "?" or "/", contain only valid URL characters and end with alphanumeric char or "/". For example "/" or "/healthz" are common
	// +kubebuilder:validation:Pattern=`^(?:\?|\/)[\w\-.~:\/?#\[\]@!$&'()*+,;=]+(?:[a-zA-Z0-9]|\/){1}$`
	Path string `json:"path,omitempty"`

	// Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"
	// Defaults to HTTPS
	// +kubebuilder:validation:XValidation:rule="self in ['HTTP','HTTPS']",message="Only HTTP or HTTPS protocols are allowed"
	// +kubebuilder:default=HTTPS
	Protocol Protocol `json:"protocol,omitempty"`

	// Interval defines how frequently this probe should execute
	// Defaults to 5 minutes
	// +kubebuilder:default="5m"
	Interval *metav1.Duration `json:"interval,omitempty"`

	// AdditionalHeadersRef refers to a secret that contains extra headers to send in the probe request, this is primarily useful if an authentication
	// token is required by the endpoint.
	// +optional
	AdditionalHeadersRef *AdditionalHeadersRef `json:"additionalHeadersRef,omitempty"`

	// FailureThreshold is a limit of consecutive failures that must occur for a host to be considered unhealthy
	// Defaults to 5
	// +kubebuilder:validation:XValidation:rule="self > 0",message="Failure threshold must be greater than 0"
	// +kubebuilder:default=5
	FailureThreshold int `json:"failureThreshold,omitempty"`
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
// +kubebuilder:validation:XValidation:rule="!has(oldSelf.delegate) || has(self.delegate)", message="Delegate can't be unset if it was previously set"
// +kubebuilder:validation:XValidation:rule="has(oldSelf.delegate) || !has(self.delegate)", message="Delegate can't be set if it was previously unset"
// +kubebuilder:validation:XValidation:rule="!(has(self.providerRef) && has(self.delegate) && self.delegate == true)", message="delegate=true and providerRef are mutually exclusive"
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
	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="RootHost is immutable"
	// +kubebuilder:validation:MinLength=1
	// +kubebuilder:validation:MaxLength=255
	// +kubebuilder:validation:Pattern=`^(?:[\w\-.~:\/?#[\]@!$&'()*+,;=]+)\.(?:[\w\-.~:\/?#[\]@!$&'()*+,;=]+)$`
	RootHost string `json:"rootHost"`

	// ProviderRef is a reference to a provider secret.
	// +optional
	ProviderRef *ProviderRef `json:"providerRef,omitempty"`

	// endpoints is a list of endpoints that will be published into the dns provider.
	// +kubebuilder:validation:MinItems=0
	// +optional
	Endpoints []*externaldns.Endpoint `json:"endpoints,omitempty"`

	// +optional
	HealthCheck *HealthCheckSpec `json:"healthCheck,omitempty"`

	// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="delegate is immutable"
	Delegate bool `json:"delegate,omitempty"`
}

// DNSRecordStatus defines the observed state of DNSRecord
type DNSRecordStatus struct {

	// conditions are any conditions associated with the record in the dns provider.
	//
	// If publishing the record fails, the "Failed" condition will be set with a
	// reason and message describing the cause of the failure.
	Conditions []metav1.Condition `json:"conditions,omitempty"`

	// observedGeneration is the most recently observed generation of the DNSRecord.
	// +optional
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`

	// QueuedAt is a time when DNS record was received for the reconciliation
	QueuedAt metav1.Time `json:"queuedAt,omitempty"`

	// ValidFor indicates duration since the last reconciliation we consider data in the record to be valid
	ValidFor string `json:"validFor,omitempty"`

	// WriteCounter represent a number of consecutive write attempts on the same generation of the record.
	// It is being reset to 0 when the generation changes or there are no changes to write.
	WriteCounter int64 `json:"writeCounter,omitempty"`

	// endpoints are the last endpoints that were successfully published to the provider zone
	Endpoints []*externaldns.Endpoint `json:"endpoints,omitempty"`

	// ZoneEndpoints are all the endpoints for the DNSRecordSpec.RootHost that are present in the provider
	ZoneEndpoints []*externaldns.Endpoint `json:"relatedEndpoints,omitempty"`

	HealthCheck *HealthCheckStatus `json:"healthCheck,omitempty"`

	// ownerID is a unique string used to identify the owner of this record.
	OwnerID string `json:"ownerID,omitempty"`

	// ProviderRef is a reference to a provider secret used to publish endpoints.
	ProviderRef ProviderRef `json:"providerRef,omitempty"`

	// DomainOwners is a list of all the owners working against the root domain of this record
	DomainOwners []string `json:"domainOwners,omitempty"`

	// zoneID is the provider specific id to which this dns record is publishing endpoints
	ZoneID string `json:"zoneID,omitempty"`

	// zoneDomainName is the domain name of the zone that the dns record is publishing endpoints
	ZoneDomainName string `json:"zoneDomainName,omitempty"`
}

func (s *DNSRecordStatus) ReadyForDelegation() bool {
	delegationReadyCond := meta.FindStatusCondition(s.Conditions, string(ConditionTypeReadyForDelegation))
	if delegationReadyCond != nil && delegationReadyCond.Status == metav1.ConditionTrue {
		return true
	}
	return false
}

// ProviderEndpointsRemoved return true if the ready status condition has the reason set to "ProviderEndpointsRemoved"
func (s *DNSRecordStatus) ProviderEndpointsRemoved() bool {
	readyCond := meta.FindStatusCondition(s.Conditions, string(ConditionTypeReady))
	if readyCond == nil || readyCond.Reason == string(ConditionReasonProviderEndpointsRemoved) {
		return true
	}
	return false
}

// ProviderEndpointsDeletion return true if the ready status condition has the reason set to "ProviderEndpointsDeletion"
func (s *DNSRecordStatus) ProviderEndpointsDeletion() bool {
	readyCond := meta.FindStatusCondition(s.Conditions, string(ConditionTypeReady))
	if readyCond == nil || readyCond.Reason == string(ConditionReasonProviderEndpointsDeletion) {
		return true
	}
	return false
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Ready",type="string",JSONPath=".status.conditions[?(@.type==\"Ready\")].status",description="DNSRecord ready."
//+kubebuilder:printcolumn:name="Healthy",type="string",JSONPath=".status.conditions[?(@.type==\"Healthy\")].status",description="DNSRecord healthy.",priority=2
//+kubebuilder:printcolumn:name="Root Host",type="string",JSONPath=".spec.rootHost",description="DNSRecord root host.",priority=2
//+kubebuilder:printcolumn:name="Owner ID",type="string",JSONPath=".status.ownerID",description="DNSRecord owner id.",priority=2
//+kubebuilder:printcolumn:name="Zone Domain",type="string",JSONPath=".status.zoneDomainName",description="DNSRecord zone domain name.",priority=2
//+kubebuilder:printcolumn:name="Zone ID",type="string",JSONPath=".status.zoneID",description="DNSRecord zone id.",priority=2

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
)

const WildcardPrefix = "*."

func (s *DNSRecord) Validate() error {
	root := s.GetRootHost()

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

	if len(s.Spec.Endpoints) == 0 {
		// probably a zone record with nothing merged into it yet, just ignore it until it has records
		return nil
	}

	return nil
}

// GetUIDHash returns a hash of the current records UID with a fixed length of 8.
func (s *DNSRecord) GetUIDHash() string {
	return hash.ToBase36HashLen(string(s.GetUID()), 8)
}

func (s *DNSRecord) HasDNSZoneAssigned() bool {
	return s.Status.ZoneID != "" && s.Status.ZoneDomainName != ""
}

func (s *DNSRecord) HasOwnerIDAssigned() bool {
	return s.Status.OwnerID != ""
}

func (s *DNSRecord) HasProviderSecretAssigned() bool {
	return s.Status.ProviderRef.Name != ""
}

func (s *DNSRecord) IsDeleting() bool {
	return s.DeletionTimestamp != nil && !s.DeletionTimestamp.IsZero()
}

// ProviderAccessor impl

var _ ProviderAccessor = &DNSRecord{}

func (s *DNSRecord) GetProviderRef() ProviderRef {
	return s.Status.ProviderRef
}

// GetRootHost returns the root host for the current record.
//
// Removes any wildcard prefix i.e. "*." that might exist. Access the spec directly if the raw value is required i.e. spec.RootHost
func (s *DNSRecord) GetRootHost() string {
	rootHost, _ := strings.CutPrefix(s.Spec.RootHost, WildcardPrefix)
	return rootHost
}

func (s *DNSRecord) IsAuthoritativeRecord() bool {
	_, okay := s.Labels[AuthoritativeRecordLabel]
	return okay
}

func (s *DNSRecord) IsDelegating() bool {
	return s.Spec.Delegate
}

func init() {
	SchemeBuilder.Register(&DNSRecord{}, &DNSRecordList{})
}
