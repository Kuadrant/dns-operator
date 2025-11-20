/*
Copyright 2025.

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

package controller

import (
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/types"
)

var _ DNSRecordAccessor = &DNSRecord{}
var _ DNSRecordAccessor = &RemoteDNSRecord{}

type DNSRecordAccessor interface {
	v1alpha1.ProviderAccessor
	GetDNSRecord() *v1alpha1.DNSRecord
	GetOwnerID() string
	GetGroup() types.Group
	GetRootHost() string
	GetZoneDomainName() string
	GetZoneID() string
	GetEndpoints() []*externaldns.Endpoint
	GetSpec() *v1alpha1.DNSRecordSpec
	GetStatus() *v1alpha1.DNSRecordStatus
	SetStatusConditions(hadChanges bool)
	SetStatusCondition(conditionType string, status metav1.ConditionStatus, reason, message string)
	SetStatusOwnerID(id string)
	SetStatusZoneID(id string)
	SetStatusZoneDomainName(domainName string)
	SetStatusDomainOwners(owners []string)
	SetStatusEndpoints(endpoints []*externaldns.Endpoint)
	SetStatusObservedGeneration(observedGeneration int64)
	SetStatusGroup(types.Group)
	HasOwnerIDAssigned() bool
	HasDNSZoneAssigned() bool
	HasProviderSecretAssigned() bool
	IsDeleting() bool
}

type DNSRecord struct {
	*v1alpha1.DNSRecord
}

func (s *DNSRecord) GetEndpoints() []*externaldns.Endpoint {
	return s.GetSpec().Endpoints
}

func (s *DNSRecord) GetDNSRecord() *v1alpha1.DNSRecord {
	return s.DNSRecord
}

func (s *DNSRecord) GetOwnerID() string {
	return s.GetStatus().OwnerID
}

func (s *DNSRecord) GetGroup() types.Group {
	return s.GetStatus().Group
}

func (s *DNSRecord) GetZoneDomainName() string {
	return s.GetStatus().ZoneDomainName
}

func (s *DNSRecord) GetZoneID() string {
	return s.GetStatus().ZoneID
}

func (s *DNSRecord) GetSpec() *v1alpha1.DNSRecordSpec {
	return &s.Spec
}

func (s *DNSRecord) GetStatus() *v1alpha1.DNSRecordStatus {
	return &s.Status
}

func (s *DNSRecord) SetStatusConditions(_ bool) {
	//We do nothing here at the moment!!
	return
}

func (s *DNSRecord) SetStatusCondition(conditionType string, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	}
	conditions := s.GetStatus().Conditions
	meta.SetStatusCondition(&conditions, cond)
	s.GetStatus().Conditions = conditions
}

func (s *DNSRecord) SetStatusOwnerID(id string) {
	s.GetStatus().OwnerID = id
}

func (s *DNSRecord) SetStatusZoneID(id string) {
	s.GetStatus().ZoneID = id
}

func (s *DNSRecord) SetStatusZoneDomainName(domainName string) {
	s.GetStatus().ZoneDomainName = domainName
}

func (s *DNSRecord) SetStatusDomainOwners(owners []string) {
	s.GetStatus().DomainOwners = owners
}

func (s *DNSRecord) SetStatusEndpoints(endpoints []*externaldns.Endpoint) {
	s.GetStatus().Endpoints = endpoints
}

func (s *DNSRecord) SetStatusObservedGeneration(observedGeneration int64) {
	s.GetStatus().ObservedGeneration = observedGeneration
}

func (s *DNSRecord) SetStatusGroup(group types.Group) {
	s.GetStatus().Group = group
}

type RemoteDNSRecord struct {
	*v1alpha1.DNSRecord
	ClusterID string
	status    *v1alpha1.DNSRecordStatus
}

func (s *RemoteDNSRecord) GetEndpoints() []*externaldns.Endpoint {
	return s.GetSpec().Endpoints
}

func (s *RemoteDNSRecord) GetDNSRecord() *v1alpha1.DNSRecord {
	return s.DNSRecord
}

func (s *RemoteDNSRecord) GetOwnerID() string {
	return s.DNSRecord.Status.OwnerID
}

func (s *RemoteDNSRecord) GetGroup() types.Group {
	return s.DNSRecord.Status.Group
}

func (s *RemoteDNSRecord) GetZoneDomainName() string {
	return s.GetStatus().ZoneDomainName
}

func (s *RemoteDNSRecord) GetZoneID() string {
	return s.GetStatus().ZoneID
}

func (s *RemoteDNSRecord) GetSpec() *v1alpha1.DNSRecordSpec {
	return &s.Spec
}

// GetStatus returns the status set for the current cluster ID.
// If none is set an empty DNSRecordStatus is returned.
func (s *RemoteDNSRecord) GetStatus() *v1alpha1.DNSRecordStatus {
	if s.status == nil {
		stat := s.Status.GetRemoteRecordStatus(s.ClusterID)
		s.status = &stat
	}
	return s.status
}

func (s *RemoteDNSRecord) SetStatusConditions(_ bool) {
	//We do nothing here at the moment!!
	return
}

func (s *RemoteDNSRecord) SetStatusCondition(conditionType string, status metav1.ConditionStatus, reason, message string) {
	cond := metav1.Condition{
		Type:               conditionType,
		Status:             status,
		Reason:             reason,
		Message:            message,
		ObservedGeneration: s.Generation,
	}
	conditions := s.GetStatus().Conditions
	meta.SetStatusCondition(&conditions, cond)
	s.GetStatus().Conditions = conditions
	s.setStatus()
}

func (s *RemoteDNSRecord) SetStatusOwnerID(_ string) {
	panic("cannot set OwnerID on remote record")
}

func (s *RemoteDNSRecord) SetStatusZoneID(id string) {
	s.GetStatus().ZoneID = id
	s.setStatus()
}

func (s *RemoteDNSRecord) SetStatusZoneDomainName(domainName string) {
	s.GetStatus().ZoneDomainName = domainName
}

func (s *RemoteDNSRecord) SetStatusDomainOwners(owners []string) {
	s.GetStatus().DomainOwners = owners
	s.setStatus()
}

func (s *RemoteDNSRecord) SetStatusEndpoints(endpoints []*externaldns.Endpoint) {
	s.GetStatus().Endpoints = endpoints
	s.setStatus()
}

func (s *RemoteDNSRecord) SetStatusObservedGeneration(observedGeneration int64) {
	s.GetStatus().ObservedGeneration = observedGeneration
	s.setStatus()
}

func (s *RemoteDNSRecord) SetStatusGroup(_ types.Group) {
	panic("cannot set Group on remote record")
}

func (s *RemoteDNSRecord) setStatus() {
	s.DNSRecord.Status.SetRemoteRecordStatus(s.ClusterID, *s.status)
}

func (s *RemoteDNSRecord) HasDNSZoneAssigned() bool {
	return s.GetStatus().ZoneID != "" && s.GetStatus().ZoneDomainName != ""
}
