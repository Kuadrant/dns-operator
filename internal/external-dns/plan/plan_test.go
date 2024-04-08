/*
Copyright 2017 The Kubernetes Authors.

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

package plan

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/suite"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/internal/external-dns/testutils"
)

type PlanTestSuite struct {
	suite.Suite
	fooV1Cname                       *endpoint.Endpoint
	fooV2Cname                       *endpoint.Endpoint
	fooV2CnameUppercase              *endpoint.Endpoint
	fooV2TXT                         *endpoint.Endpoint
	fooV2CnameNoLabel                *endpoint.Endpoint
	fooV3CnameSameResource           *endpoint.Endpoint
	fooA5                            *endpoint.Endpoint
	fooAAAA                          *endpoint.Endpoint
	dsA                              *endpoint.Endpoint
	dsAAAA                           *endpoint.Endpoint
	bar127A                          *endpoint.Endpoint
	bar127AWithTTL                   *endpoint.Endpoint
	bar127AWithProviderSpecificTrue  *endpoint.Endpoint
	bar127AWithProviderSpecificFalse *endpoint.Endpoint
	bar127AWithProviderSpecificUnset *endpoint.Endpoint
	bar192A                          *endpoint.Endpoint
	multiple1                        *endpoint.Endpoint
	multiple2                        *endpoint.Endpoint
	multiple3                        *endpoint.Endpoint
	domainFilterFiltered1            *endpoint.Endpoint
	domainFilterFiltered2            *endpoint.Endpoint
	domainFilterFiltered3            *endpoint.Endpoint
	domainFilterExcluded             *endpoint.Endpoint
	//A Records
	fooA1OwnerNone *endpoint.Endpoint
	fooA2OwnerNone *endpoint.Endpoint
	barA3OwnerNone *endpoint.Endpoint
	barA4OwnerNone *endpoint.Endpoint
	fooA1Owner1    *endpoint.Endpoint
	fooA2Owner1    *endpoint.Endpoint
	fooA2Owner2    *endpoint.Endpoint
	fooA12Owner12  *endpoint.Endpoint
	barA3Owner1    *endpoint.Endpoint
	barA3Owner2    *endpoint.Endpoint
	barA4Owner1    *endpoint.Endpoint
	barA4Owner2    *endpoint.Endpoint
	//A Records with SetIdentifier
	fooA1Owner1WithSetIdentifier1 *endpoint.Endpoint
	fooA2Owner1WithSetIdentifier1 *endpoint.Endpoint
	fooA2Owner1WithSetIdentifier2 *endpoint.Endpoint
	fooA2Owner2WithSetIdentifier2 *endpoint.Endpoint
	//CNAME Records
	fooCNAMEv1OwnerNone *endpoint.Endpoint
	fooCNAMEv2OwnerNone *endpoint.Endpoint
	barCNAMEv3OwnerNone *endpoint.Endpoint
	barCNAMEv4OwnerNone *endpoint.Endpoint
	fooCNAMEv1Owner1    *endpoint.Endpoint
	fooCNAMEv2Owner1    *endpoint.Endpoint
	fooCNAMEv2Owner2    *endpoint.Endpoint
	fooCNAMEv12Owner12  *endpoint.Endpoint
	barCNAMEv3Owner1    *endpoint.Endpoint
	barCNAMEv3Owner2    *endpoint.Endpoint
	barCNAMEv4Owner1    *endpoint.Endpoint
	barCNAMEv4Owner2    *endpoint.Endpoint
	barCNAMEv34Owner2   *endpoint.Endpoint
	barCNAMEv34Owner12  *endpoint.Endpoint
	//CNAME Records with SetIdentifier
	fooCNAMEv1Owner1WithSetIdentifier1 *endpoint.Endpoint
	fooCNAMEv1Owner2WithSetIdentifier1 *endpoint.Endpoint
	fooCNAMEv2Owner1WithSetIdentifier1 *endpoint.Endpoint
	fooCNAMEv2Owner1WithSetIdentifier2 *endpoint.Endpoint
	fooCNAMEv2Owner2WithSetIdentifier2 *endpoint.Endpoint
}

func (suite *PlanTestSuite) SetupTest() {
	suite.fooV1Cname = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v1"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v1",
			endpoint.OwnerLabelKey:    "pwner",
		},
	}
	// same resource as fooV1Cname, but target is different. It will never be picked because its target lexicographically bigger than "v1"
	suite.fooV3CnameSameResource = &endpoint.Endpoint{ // TODO: remove this once endpoint can support multiple targets
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v3"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v1",
			endpoint.OwnerLabelKey:    "pwner",
		},
	}
	suite.fooV2Cname = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v2"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v2",
		},
	}
	suite.fooV2CnameUppercase = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"V2"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-v2",
		},
	}
	suite.fooV2TXT = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "TXT",
	}
	suite.fooV2CnameNoLabel = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"v2"},
		RecordType: "CNAME",
	}
	suite.fooA5 = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"5.5.5.5"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-5",
		},
	}
	suite.fooAAAA = &endpoint.Endpoint{
		DNSName:    "foo",
		Targets:    endpoint.Targets{"2001:DB8::1"},
		RecordType: "AAAA",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-AAAA",
		},
	}
	suite.dsA = &endpoint.Endpoint{
		DNSName:    "ds",
		Targets:    endpoint.Targets{"1.1.1.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/ds",
		},
	}
	suite.dsAAAA = &endpoint.Endpoint{
		DNSName:    "ds",
		Targets:    endpoint.Targets{"2001:DB8::1"},
		RecordType: "AAAA",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/ds-AAAAA",
		},
	}
	suite.bar127A = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
	}
	suite.bar127AWithTTL = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		RecordTTL:  300,
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
	}
	suite.bar127AWithProviderSpecificTrue = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
		ProviderSpecific: endpoint.ProviderSpecific{
			endpoint.ProviderSpecificProperty{
				Name:  "alias",
				Value: "false",
			},
			endpoint.ProviderSpecificProperty{
				Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
				Value: "true",
			},
		},
	}
	suite.bar127AWithProviderSpecificFalse = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
		ProviderSpecific: endpoint.ProviderSpecific{
			endpoint.ProviderSpecificProperty{
				Name:  "external-dns.alpha.kubernetes.io/cloudflare-proxied",
				Value: "false",
			},
			endpoint.ProviderSpecificProperty{
				Name:  "alias",
				Value: "false",
			},
		},
	}
	suite.bar127AWithProviderSpecificUnset = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"127.0.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-127",
		},
		ProviderSpecific: endpoint.ProviderSpecific{
			endpoint.ProviderSpecificProperty{
				Name:  "alias",
				Value: "false",
			},
		},
	}
	suite.bar192A = &endpoint.Endpoint{
		DNSName:    "bar",
		Targets:    endpoint.Targets{"192.168.0.1"},
		RecordType: "A",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/bar-192",
		},
	}
	suite.multiple1 = &endpoint.Endpoint{
		DNSName:       "multiple",
		Targets:       endpoint.Targets{"192.168.0.1"},
		RecordType:    "A",
		SetIdentifier: "test-set-1",
	}
	suite.multiple2 = &endpoint.Endpoint{
		DNSName:       "multiple",
		Targets:       endpoint.Targets{"192.168.0.2"},
		RecordType:    "A",
		SetIdentifier: "test-set-1",
	}
	suite.multiple3 = &endpoint.Endpoint{
		DNSName:       "multiple",
		Targets:       endpoint.Targets{"192.168.0.2"},
		RecordType:    "A",
		SetIdentifier: "test-set-2",
	}
	suite.domainFilterFiltered1 = &endpoint.Endpoint{
		DNSName:    "foo.domain.tld",
		Targets:    endpoint.Targets{"1.2.3.4"},
		RecordType: "A",
	}
	suite.domainFilterFiltered2 = &endpoint.Endpoint{
		DNSName:    "bar.domain.tld",
		Targets:    endpoint.Targets{"1.2.3.5"},
		RecordType: "A",
	}
	suite.domainFilterFiltered3 = &endpoint.Endpoint{
		DNSName:    "baz.domain.tld",
		Targets:    endpoint.Targets{"1.2.3.6"},
		RecordType: "A",
	}
	suite.domainFilterExcluded = &endpoint.Endpoint{
		DNSName:    "foo.ex.domain.tld",
		Targets:    endpoint.Targets{"1.1.1.1"},
		RecordType: "A",
	}
	//Muti Owner
	// A Records
	suite.fooA1OwnerNone = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"1.1.1.1"},
	}
	suite.fooA2OwnerNone = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"2.2.2.2"},
	}
	suite.barA3OwnerNone = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "A",
		Targets:    endpoint.Targets{"3.3.3.3"},
	}
	suite.barA4OwnerNone = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "A",
		Targets:    endpoint.Targets{"4.4.4.4"},
	}
	suite.fooA1Owner1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"1.1.1.1"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.fooA2Owner1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"2.2.2.2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.fooA2Owner2 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"2.2.2.2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	suite.fooA12Owner12 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"1.1.1.1", "2.2.2.2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1&&owner2",
		},
	}
	suite.barA3Owner1 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "A",
		Targets:    endpoint.Targets{"3.3.3.3"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.barA3Owner2 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "A",
		Targets:    endpoint.Targets{"3.3.3.3"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	suite.barA4Owner1 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "A",
		Targets:    endpoint.Targets{"4.4.4.4"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.barA4Owner2 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "A",
		Targets:    endpoint.Targets{"4.4.4.4"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	// A Records with SetIdentifier
	suite.fooA1Owner1WithSetIdentifier1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"1.1.1.1"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
		SetIdentifier: "1",
	}
	suite.fooA2Owner1WithSetIdentifier1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"2.2.2.2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
		SetIdentifier: "1",
	}
	suite.fooA2Owner1WithSetIdentifier2 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"2.2.2.2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
		SetIdentifier: "2",
	}
	suite.fooA2Owner2WithSetIdentifier2 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "A",
		Targets:    endpoint.Targets{"2.2.2.2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
		SetIdentifier: "2",
	}

	// CNAME Records
	suite.fooCNAMEv1OwnerNone = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v1"},
	}
	suite.fooCNAMEv2OwnerNone = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v2"},
	}
	suite.barCNAMEv3OwnerNone = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v3"},
	}
	suite.barCNAMEv4OwnerNone = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v4"},
	}
	suite.fooCNAMEv1Owner1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v1"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.fooCNAMEv2Owner1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.fooCNAMEv2Owner2 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	suite.fooCNAMEv12Owner12 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v1", "v2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1&&owner2",
		},
	}
	suite.barCNAMEv3Owner1 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v3"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.barCNAMEv3Owner2 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v3"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	suite.barCNAMEv4Owner1 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v4"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
	}
	suite.barCNAMEv4Owner2 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v4"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	suite.barCNAMEv34Owner2 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v3", "v4"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
	}
	suite.barCNAMEv34Owner12 = &endpoint.Endpoint{
		DNSName:    "bar",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v3", "v4"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1&&owner2",
		},
	}
	// CNAME Records with SetIdentifier
	suite.fooCNAMEv1Owner1WithSetIdentifier1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v1"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
		SetIdentifier: "1",
	}
	suite.fooCNAMEv2Owner1WithSetIdentifier1 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
		SetIdentifier: "1",
	}
	suite.fooCNAMEv2Owner1WithSetIdentifier2 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAMEA",
		Targets:    endpoint.Targets{"v2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner1",
		},
		SetIdentifier: "2",
	}
	suite.fooCNAMEv2Owner2WithSetIdentifier2 = &endpoint.Endpoint{
		DNSName:    "foo",
		RecordType: "CNAME",
		Targets:    endpoint.Targets{"v2"},
		Labels: map[string]string{
			endpoint.OwnerLabelKey: "owner2",
		},
		SetIdentifier: "2",
	}
}

func (suite *PlanTestSuite) TestSyncFirstRound() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname, suite.bar127A}
	expectedCreate := []*endpoint.Endpoint{suite.fooV1Cname, suite.bar127A} // v1 is chosen because of resolver taking "min"
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRound() {
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooV2Cname, suite.fooV1Cname, suite.bar127A}
	expectedCreate := []*endpoint.Endpoint{suite.bar127A}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRoundMigration() {
	current := []*endpoint.Endpoint{suite.fooV2CnameNoLabel}
	desired := []*endpoint.Endpoint{suite.fooV2Cname, suite.fooV1Cname, suite.bar127A}
	expectedCreate := []*endpoint.Endpoint{suite.bar127A}
	expectedUpdateOld := []*endpoint.Endpoint{suite.fooV2CnameNoLabel}
	expectedUpdateNew := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithTTLChange() {
	current := []*endpoint.Endpoint{suite.bar127A}
	desired := []*endpoint.Endpoint{suite.bar127AWithTTL}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.bar127A}
	expectedUpdateNew := []*endpoint.Endpoint{suite.bar127AWithTTL}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithProviderSpecificChange() {
	current := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	desired := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	expectedUpdateNew := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithProviderSpecificRemoval() {
	current := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	desired := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificUnset}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificFalse}
	expectedUpdateNew := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificUnset}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithProviderSpecificAddition() {
	current := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificUnset}
	desired := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificUnset}
	expectedUpdateNew := []*endpoint.Endpoint{suite.bar127AWithProviderSpecificTrue}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSyncSecondRoundWithOwnerInherited() {
	suite.T().Skip("Skipping incompatible test")
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}

	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedUpdateNew := []*endpoint.Endpoint{{
		DNSName:    suite.fooV2Cname.DNSName,
		Targets:    suite.fooV2Cname.Targets,
		RecordType: suite.fooV2Cname.RecordType,
		RecordTTL:  suite.fooV2Cname.RecordTTL,
		Labels: map[string]string{
			endpoint.ResourceLabelKey: suite.fooV2Cname.Labels[endpoint.ResourceLabelKey],
			endpoint.OwnerLabelKey:    suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
		},
	}}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestIdempotency() {
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestRecordTypeChange() {
	suite.T().Skip("Skipping incompatible test, plan does not allow record types to change")
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooA5}
	expectedCreate := []*endpoint.Endpoint{suite.fooA5}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestExistingCNameWithDualStackDesired() {
	suite.T().Skip("Skipping incompatible test, plan does not allow record types to change")
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooA5, suite.fooAAAA}
	expectedCreate := []*endpoint.Endpoint{suite.fooA5, suite.fooAAAA}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestExistingDualStackWithCNameDesired() {
	suite.T().Skip("Skipping incompatible test, plan does not allow record types to change")
	suite.fooA5.Labels[endpoint.OwnerLabelKey] = "nerf"
	suite.fooAAAA.Labels[endpoint.OwnerLabelKey] = "nerf"
	current := []*endpoint.Endpoint{suite.fooA5, suite.fooAAAA}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooA5, suite.fooAAAA}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        suite.fooA5.Labels[endpoint.OwnerLabelKey],
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// TestExistingOwnerNotMatchingDualStackDesired validates that if there is an existing
// record for a domain but there is no ownership claim over it and there are desired
// records no changes are planed. Only domains that have explicit ownership claims should
// be updated.
func (suite *PlanTestSuite) TestExistingOwnerNotMatchingDualStackDesired() {
	suite.fooA5.Labels = nil
	current := []*endpoint.Endpoint{suite.fooA5}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        "pwner",
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// TestConflictingCurrentNonConflictingDesired is a bit of a corner case as it would indicate
// that the provider is not following valid DNS rules or there may be some
// caching issues. In this case since the desired records are not conflicting
// the updates will end up with the conflict resolved.
func (suite *PlanTestSuite) TestConflictingCurrentNonConflictingDesired() {
	suite.T().Skip("Skipping incompatible test, plan does not allow record types to change")
	suite.fooA5.Labels[endpoint.OwnerLabelKey] = suite.fooV1Cname.Labels[endpoint.OwnerLabelKey]
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooA5}
	desired := []*endpoint.Endpoint{suite.fooA5}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// TestConflictingCurrentNoDesired is a bit of a corner case as it would indicate
// that the provider is not following valid DNS rules or there may be some
// caching issues. In this case there are no desired enpoint candidates so plan
// on deleting the records.
func (suite *PlanTestSuite) TestConflictingCurrentNoDesired() {
	suite.fooA5.Labels[endpoint.OwnerLabelKey] = suite.fooV1Cname.Labels[endpoint.OwnerLabelKey]
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooA5}
	desired := []*endpoint.Endpoint{}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooA5}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// TestCurrentWithConflictingDesired simulates where the desired records result in conflicting records types.
// This could be the result of multiple sources generating conflicting records types. In this case the conflict
// resolver should prefer the A and AAAA record candidate and delete the other records.
func (suite *PlanTestSuite) TestCurrentWithConflictingDesired() {
	suite.T().Skip("Skipping incompatible test, plan does not allow record types to change")
	suite.fooV1Cname.Labels[endpoint.OwnerLabelKey] = "nerf"
	current := []*endpoint.Endpoint{suite.fooV1Cname}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooA5, suite.fooAAAA}
	expectedCreate := []*endpoint.Endpoint{suite.fooA5, suite.fooAAAA}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
		OwnerID:        suite.fooV1Cname.Labels[endpoint.OwnerLabelKey],
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// TestNoCurrentWithConflictingDesired simulates where the desired records result in conflicting records types.
// This could be the result of multiple sources generating conflicting records types. In this case there the
// conflict resolver should prefer the A and AAAA record and drop the other candidate record types.
func (suite *PlanTestSuite) TestNoCurrentWithConflictingDesired() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooV1Cname, suite.fooA5, suite.fooAAAA}
	expectedCreate := []*endpoint.Endpoint{suite.fooA5, suite.fooAAAA}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestIgnoreTXT() {
	current := []*endpoint.Endpoint{suite.fooV2TXT}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestExcludeTXT() {
	current := []*endpoint.Endpoint{suite.fooV2TXT}
	desired := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedCreate := []*endpoint.Endpoint{suite.fooV2Cname}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME, endpoint.RecordTypeTXT},
		ExcludeRecords: []string{endpoint.RecordTypeTXT},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestIgnoreTargetCase() {
	current := []*endpoint.Endpoint{suite.fooV2Cname}
	desired := []*endpoint.Endpoint{suite.fooV2CnameUppercase}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies: []Policy{&SyncPolicy{}},
		Current:  current,
		Desired:  desired,
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestRemoveEndpoint() {
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.bar192A}
	desired := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.bar192A}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestRemoveEndpointWithUpsert() {
	current := []*endpoint.Endpoint{suite.fooV1Cname, suite.bar192A}
	desired := []*endpoint.Endpoint{suite.fooV1Cname}
	expectedCreate := []*endpoint.Endpoint{}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&UpsertOnlyPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestMultipleRecordsSameNameDifferentSetIdentifier() {
	current := []*endpoint.Endpoint{suite.multiple1}
	desired := []*endpoint.Endpoint{suite.multiple2, suite.multiple3}
	expectedCreate := []*endpoint.Endpoint{suite.multiple3}
	expectedUpdateOld := []*endpoint.Endpoint{suite.multiple1}
	expectedUpdateNew := []*endpoint.Endpoint{suite.multiple2}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestSetIdentifierUpdateCreatesAndDeletes() {
	current := []*endpoint.Endpoint{suite.multiple2}
	desired := []*endpoint.Endpoint{suite.multiple3}
	expectedCreate := []*endpoint.Endpoint{suite.multiple3}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.multiple2}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestDomainFiltersInitial() {
	current := []*endpoint.Endpoint{suite.domainFilterExcluded}
	desired := []*endpoint.Endpoint{suite.domainFilterExcluded, suite.domainFilterFiltered1, suite.domainFilterFiltered2, suite.domainFilterFiltered3}
	expectedCreate := []*endpoint.Endpoint{suite.domainFilterFiltered1, suite.domainFilterFiltered2, suite.domainFilterFiltered3}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	domainFilter := endpoint.NewDomainFilterWithExclusions([]string{"domain.tld"}, []string{"ex.domain.tld"})
	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		DomainFilter:   endpoint.MatchAllDomainFilters{&domainFilter},
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestDomainFiltersUpdate() {
	current := []*endpoint.Endpoint{suite.domainFilterExcluded, suite.domainFilterFiltered1, suite.domainFilterFiltered2}
	desired := []*endpoint.Endpoint{suite.domainFilterExcluded, suite.domainFilterFiltered1, suite.domainFilterFiltered2, suite.domainFilterFiltered3}
	expectedCreate := []*endpoint.Endpoint{suite.domainFilterFiltered3}
	expectedUpdateOld := []*endpoint.Endpoint{}
	expectedUpdateNew := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectedUpdateOld,
		UpdateNew: expectedUpdateNew,
		Delete:    expectedDelete,
	}

	domainFilter := endpoint.NewDomainFilterWithExclusions([]string{"domain.tld"}, []string{"ex.domain.tld"})
	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		DomainFilter:   endpoint.MatchAllDomainFilters{&domainFilter},
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestAAAARecords() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooAAAA}
	expectedCreate := []*endpoint.Endpoint{suite.fooAAAA}
	expectNoChanges := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectNoChanges,
		UpdateNew: expectNoChanges,
		Delete:    expectNoChanges,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestDualStackRecords() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.dsA, suite.dsAAAA}
	expectedCreate := []*endpoint.Endpoint{suite.dsA, suite.dsAAAA}
	expectNoChanges := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectedCreate,
		UpdateOld: expectNoChanges,
		UpdateNew: expectNoChanges,
		Delete:    expectNoChanges,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestDualStackRecordsDelete() {
	current := []*endpoint.Endpoint{suite.dsA, suite.dsAAAA}
	desired := []*endpoint.Endpoint{}
	expectedDelete := []*endpoint.Endpoint{suite.dsA, suite.dsAAAA}
	expectNoChanges := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectNoChanges,
		UpdateOld: expectNoChanges,
		UpdateNew: expectNoChanges,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func (suite *PlanTestSuite) TestDualStackToSingleStack() {
	suite.T().Skip("Skipping incompatible test, plan does not allow record types to change")
	current := []*endpoint.Endpoint{suite.dsA, suite.dsAAAA}
	desired := []*endpoint.Endpoint{suite.dsA}
	expectedDelete := []*endpoint.Endpoint{suite.dsAAAA}
	expectNoChanges := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    expectNoChanges,
		UpdateOld: expectNoChanges,
		UpdateNew: expectNoChanges,
		Delete:    expectedDelete,
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Multiple Owner Tests

//A Records

// Should create record with plan owner.
func (suite *PlanTestSuite) TestMultiOwnerARecordCreate() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooA1Owner1}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{suite.fooA1Owner1.DeepCopy()},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner1",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not merge targets of records with a shared dnsName, type and a set identifier.
func (suite *PlanTestSuite) TestMultiOwnerARecordWithSetIdentifierCreate() {
	current := []*endpoint.Endpoint{suite.fooA1Owner1WithSetIdentifier1}
	desired := []*endpoint.Endpoint{suite.fooA2Owner2WithSetIdentifier2}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{suite.fooA2Owner2WithSetIdentifier2.DeepCopy()},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should allow owned records to be updated from the creation of records with a shared dnsName and type by a plan with a different owner.
func (suite *PlanTestSuite) TestMultiOwnerARecordUpdateCreate() {
	current := []*endpoint.Endpoint{suite.fooA1Owner1, suite.barA3Owner2}
	previous := []*endpoint.Endpoint{suite.barA3Owner2}
	desired := []*endpoint.Endpoint{suite.fooA2OwnerNone, suite.barA3Owner2}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooA1Owner1.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooA12Owner12.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should allow owned records to be updated from the change to a record with a shared dnsName and type by a plan with the same owner.
// Should remove the previous target value
// Should merge targets of records with a shared dnsName and type.
func (suite *PlanTestSuite) TestMultiOwnerARecordUpdateSameOwner() {
	current := []*endpoint.Endpoint{suite.barA3Owner2}
	previous := []*endpoint.Endpoint{suite.barA3Owner2}
	desired := []*endpoint.Endpoint{suite.barA4OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.barA3Owner2.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.barA4Owner2.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not allow the update of a record with a shared dnsName to a different record type (A -> CNAME) (CONFLICT).
// ToDo This needs to expose the conflicting endpoints
func (suite *PlanTestSuite) TestMultiOwnerARecordUpdateRecordTypeConflict() {
	current := []*endpoint.Endpoint{suite.fooA1Owner1}
	previous := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv1OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not merge targets of records with a shared dnsName, type and set identifier.
func (suite *PlanTestSuite) TestMultiOwnerARecordUpdateSameOwnerWithSetIdentifier() {
	current := []*endpoint.Endpoint{suite.fooA1Owner1WithSetIdentifier1}
	desired := []*endpoint.Endpoint{suite.fooA2Owner1WithSetIdentifier1}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooA1Owner1WithSetIdentifier1.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooA2Owner1WithSetIdentifier1.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner1",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not allow owned records to be updated by a plan with no owner
func (suite *PlanTestSuite) TestNoPlanOwnerARecordUpdate() {
	current := []*endpoint.Endpoint{suite.fooA1Owner1}
	desired := []*endpoint.Endpoint{suite.fooA2OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not merge targets of unowned records with a shared dnsName and type
func (suite *PlanTestSuite) TestNoOwnerARecordUpdate() {
	current := []*endpoint.Endpoint{suite.fooA1OwnerNone}
	desired := []*endpoint.Endpoint{suite.fooA2OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooA1OwnerNone.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooA2OwnerNone.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should only delete records solely owned by the plan owner and update records with shared ownership to remove the plan owner.
func (suite *PlanTestSuite) TestMultiOwnerARecordDelete() {
	current := []*endpoint.Endpoint{suite.barA3Owner1, suite.barA4Owner2, suite.fooA12Owner12}
	previous := []*endpoint.Endpoint{suite.barA4Owner2, suite.fooA2Owner2}
	desired := []*endpoint.Endpoint{}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooA12Owner12.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooA1Owner1.DeepCopy()},
		Delete:    []*endpoint.Endpoint{suite.barA4Owner2.DeepCopy()},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		Previous:       previous,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

//CNAME Records

// Should create record with plan owner.
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordCreate() {
	current := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{suite.fooCNAMEv1Owner1.DeepCopy()},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner1",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not merge targets of records with a shared dnsName, type and a set identifier.
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordWithSetIdentifierCreate() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1WithSetIdentifier1}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv2Owner2WithSetIdentifier2}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{suite.fooCNAMEv2Owner2WithSetIdentifier2.DeepCopy()},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should allow owned records to be updated from the creation of records with a shared dnsName and type by a plan with a different owner.
// ToDo Whether we do the merge targets of CNAMES or not needs to be decided on per provider basis
// This can create invalid CNAME records for AWS currently
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordUpdateDifferentOwner() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1, suite.barCNAMEv3Owner2}
	previous := []*endpoint.Endpoint{suite.barCNAMEv3Owner2}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv2OwnerNone, suite.barCNAMEv3Owner2}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooCNAMEv1Owner1.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooCNAMEv12Owner12.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should allow owned records to be updated from the change to a record with a shared dnsName and type by a plan with the same owner.
// Should remove the previous target value
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordUpdateSameOwner() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1, suite.barCNAMEv3Owner2}
	previous := []*endpoint.Endpoint{suite.barCNAMEv3Owner2}
	desired := []*endpoint.Endpoint{suite.barCNAMEv4Owner2}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.barCNAMEv3Owner2.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.barCNAMEv4Owner2.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not allow the update of a record with a shared dnsName to a different record type (CNAME -> A) (CONFLICT).
// ToDo This needs to expose the conflicting endpoints
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordUpdateRecordTypeConflict() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1}
	previous := []*endpoint.Endpoint{}
	desired := []*endpoint.Endpoint{suite.fooA1OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not merge targets of records with a shared dnsName, type and set identifier.
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordUpdateSameOwnerWithSetIdentifier() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1WithSetIdentifier1}
	previous := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1WithSetIdentifier1}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv2Owner1WithSetIdentifier1}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooCNAMEv1Owner1WithSetIdentifier1.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooCNAMEv2Owner1WithSetIdentifier1.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		OwnerID:        "owner1",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Previous:       previous,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not allow owned records to be updated by a plan with no owner
func (suite *PlanTestSuite) TestNoPlanOwnerCNAMERecordUpdate() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1Owner1}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv2OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should not merge targets of unowned records with a shared dnsName and type
func (suite *PlanTestSuite) TestNoPlanOwnerCNAMERecordUpdateNoOwner() {
	current := []*endpoint.Endpoint{suite.fooCNAMEv1OwnerNone}
	desired := []*endpoint.Endpoint{suite.fooCNAMEv2OwnerNone}
	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooCNAMEv1OwnerNone.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooCNAMEv2OwnerNone.DeepCopy()},
		Delete:    []*endpoint.Endpoint{},
	}

	p := &Plan{
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

// Should only delete records solely owned by the plan owner and update records with shared ownership to remove the plan owner.
func (suite *PlanTestSuite) TestMultiOwnerCNAMERecordDelete() {
	current := []*endpoint.Endpoint{suite.barCNAMEv3Owner1, suite.barCNAMEv4Owner2, suite.fooCNAMEv12Owner12}
	previous := []*endpoint.Endpoint{suite.barCNAMEv4Owner2, suite.fooCNAMEv2Owner2}
	desired := []*endpoint.Endpoint{}

	expectedChanges := &plan.Changes{
		Create:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{suite.fooCNAMEv12Owner12.DeepCopy()},
		UpdateNew: []*endpoint.Endpoint{suite.fooCNAMEv1Owner1.DeepCopy()},
		Delete:    []*endpoint.Endpoint{suite.barCNAMEv4Owner2.DeepCopy()},
	}

	p := &Plan{
		OwnerID:        "owner2",
		Policies:       []Policy{&SyncPolicy{}},
		Current:        current,
		Desired:        desired,
		Previous:       previous,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := p.Calculate().Changes
	validateChanges(suite.T(), changes, expectedChanges)
}

func TestPlan(t *testing.T) {
	suite.Run(t, new(PlanTestSuite))
}

// validateEntries validates that the list of entries matches expected.
func validateEntries(t *testing.T, entries, expected []*endpoint.Endpoint) {
	if !testutils.SameEndpoints(entries, expected) {
		t.Fatalf("expected %q to match %q", entries, expected)
	}
}

func validateChangeEntries(t string, entries, expected []*endpoint.Endpoint) error {
	if !testutils.SameEndpoints(entries, expected) {
		return fmt.Errorf("expected %s %q to match %q", t, entries, expected)
	}
	return nil
}

func validateChanges(t *testing.T, changes, expected *plan.Changes) {
	var validateErr error

	if err := validateChangeEntries("changes.Create", changes.Create, expected.Create); err != nil {
		validateErr = errors.Join(validateErr, err)
	}
	if err := validateChangeEntries("changes.UpdateNew", changes.UpdateNew, expected.UpdateNew); err != nil {
		validateErr = errors.Join(validateErr, err)
	}
	if err := validateChangeEntries("changes.UpdateOld", changes.UpdateOld, expected.UpdateOld); err != nil {
		validateErr = errors.Join(validateErr, err)
	}
	if err := validateChangeEntries("changes.Delete", changes.Delete, expected.Delete); err != nil {
		validateErr = errors.Join(validateErr, err)
	}

	if validateErr != nil {
		t.Fatalf(validateErr.Error())
	}
}

func TestNormalizeDNSName(t *testing.T) {
	records := []struct {
		dnsName string
		expect  string
	}{
		{
			"3AAAA.FOO.BAR.COM    ",
			"3aaaa.foo.bar.com.",
		},
		{
			"   example.foo.com.",
			"example.foo.com.",
		},
		{
			"example123.foo.com ",
			"example123.foo.com.",
		},
		{
			"foo",
			"foo.",
		},
		{
			"123foo.bar",
			"123foo.bar.",
		},
		{
			"foo.com",
			"foo.com.",
		},
		{
			"foo.com.",
			"foo.com.",
		},
		{
			"foo123.COM",
			"foo123.com.",
		},
		{
			"my-exaMple3.FOO.BAR.COM",
			"my-example3.foo.bar.com.",
		},
		{
			"   my-example1214.FOO-1235.BAR-foo.COM   ",
			"my-example1214.foo-1235.bar-foo.com.",
		},
		{
			"my-example-my-example-1214.FOO-1235.BAR-foo.COM",
			"my-example-my-example-1214.foo-1235.bar-foo.com.",
		},
	}
	for _, r := range records {
		gotName := normalizeDNSName(r.dnsName)
		assert.Equal(t, r.expect, gotName)
	}
}

func TestShouldUpdateProviderSpecific(tt *testing.T) {
	for _, test := range []struct {
		name         string
		current      *endpoint.Endpoint
		desired      *endpoint.Endpoint
		shouldUpdate bool
	}{
		{
			name: "skip AWS target health",
			current: &endpoint.Endpoint{
				DNSName: "foo.com",
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "aws/evaluate-target-health", Value: "true"},
				},
			},
			desired: &endpoint.Endpoint{
				DNSName: "bar.com",
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "aws/evaluate-target-health", Value: "true"},
				},
			},
			shouldUpdate: false,
		},
		{
			name: "custom property unchanged",
			current: &endpoint.Endpoint{
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "custom/property", Value: "true"},
				},
			},
			desired: &endpoint.Endpoint{
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "custom/property", Value: "true"},
				},
			},
			shouldUpdate: false,
		},
		{
			name: "custom property value changed",
			current: &endpoint.Endpoint{
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "custom/property", Value: "true"},
				},
			},
			desired: &endpoint.Endpoint{
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "custom/property", Value: "false"},
				},
			},
			shouldUpdate: true,
		},
		{
			name: "custom property key changed",
			current: &endpoint.Endpoint{
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "custom/property", Value: "true"},
				},
			},
			desired: &endpoint.Endpoint{
				ProviderSpecific: []endpoint.ProviderSpecificProperty{
					{Name: "new/property", Value: "true"},
				},
			},
			shouldUpdate: true,
		},
	} {
		tt.Run(test.name, func(t *testing.T) {
			b := shouldUpdateProviderSpecific(test.desired, test.current)
			assert.Equal(t, test.shouldUpdate, b)
		})
	}
}
