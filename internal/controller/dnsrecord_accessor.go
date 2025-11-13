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
	"k8s.io/apimachinery/pkg/runtime"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/types"
)

var _ DNSRecordAccessor = &RemoteDNSRecord{}

type DNSRecordAccessor interface {
	metav1.Object
	runtime.Object
	v1alpha1.ProviderAccessor
	GetDNSRecord() *v1alpha1.DNSRecord
	GetOwnerID() string
	GetRootHost() string
	GetZoneDomainName() string
	GetZoneID() string
	GetSpec() *v1alpha1.DNSRecordSpec
	GetStatus() *v1alpha1.DNSRecordStatus
	SetStatusCondition(conditionType string, status metav1.ConditionStatus, reason, message string)
	SetStatusZoneID(id string)
	SetStatusZoneDomainName(domainName string)
	SetStatusDomainOwners(owners []string)
	SetStatusEndpoints(endpoints []*externaldns.Endpoint)
	SetStatusObservedGeneration(observedGeneration int64)
	HasOwnerIDAssigned() bool
	HasDNSZoneAssigned() bool
	SetStatusGroup(types.Group)
}

type RemoteDNSRecord struct {
	*v1alpha1.DNSRecord
	ClusterID string
	status    *v1alpha1.DNSRecordStatus
}

func (s *RemoteDNSRecord) GetDNSRecord() *v1alpha1.DNSRecord {
	return s.DNSRecord
}

func (s *RemoteDNSRecord) GetOwnerID() string {
	return s.DNSRecord.Status.OwnerID
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

func (s *RemoteDNSRecord) setStatus() {
	s.DNSRecord.Status.SetRemoteRecordStatus(s.ClusterID, *s.status)
}

func (s *RemoteDNSRecord) HasDNSZoneAssigned() bool {
	return s.GetStatus().ZoneID != "" && s.GetStatus().ZoneDomainName != ""
}

func (s *RemoteDNSRecord) SetStatusGroup(group types.Group) {
	s.status.Group = group
}
