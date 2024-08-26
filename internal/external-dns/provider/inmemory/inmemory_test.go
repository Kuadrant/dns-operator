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

package inmemory

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/internal/external-dns/testutils"
)

var _ provider.Provider = &InMemoryProvider{}

func TestInMemoryProvider(t *testing.T) {
	t.Run("Records", testInMemoryRecords)
	t.Run("validateChangeBatch", testInMemoryValidateChangeBatch)
	t.Run("ApplyChanges", testInMemoryApplyChanges)
	t.Run("NewInMemoryProvider", testNewInMemoryProvider)
	t.Run("CreateZone", testInMemoryCreateZone)
}

func testInMemoryRecords(t *testing.T) {
	for _, ti := range []struct {
		title       string
		zone        string
		expectError bool
		init        map[string]Zone
		expected    []*endpoint.Endpoint
	}{
		{
			title:       "no records, no Zone",
			zone:        "",
			init:        map[string]Zone{},
			expectError: false,
		},
		{
			title: "records, wrong Zone",
			zone:  "net",
			init: map[string]Zone{
				"org": {},
				"com": {},
			},
			expectError: false,
		},
		{
			title: "records, Zone with records",
			zone:  "org",
			init: map[string]Zone{
				"org": makeZone(
					"example.org", "8.8.8.8", endpoint.RecordTypeA,
					"example.org", "", endpoint.RecordTypeTXT,
					"foo.org", "4.4.4.4", endpoint.RecordTypeCNAME,
				),
				"com": makeZone("example.com", "4.4.4.4", endpoint.RecordTypeCNAME),
			},
			expectError: false,
			expected: []*endpoint.Endpoint{
				{
					DNSName:    "example.org",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				},
				{
					DNSName:    "example.org",
					RecordType: endpoint.RecordTypeTXT,
					Targets:    endpoint.Targets{""},
				},
				{
					DNSName:    "foo.org",
					Targets:    endpoint.Targets{"4.4.4.4"},
					RecordType: endpoint.RecordTypeCNAME,
				},
			},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			c := NewInMemoryClient()
			c.zones = ti.init
			im := NewInMemoryProvider(context.Background())
			im.client = c
			im.domain = endpoint.NewDomainFilter([]string{ti.zone})
			f := filter{domain: ti.zone}
			im.filter = &f
			records, err := im.Records(context.Background())
			if ti.expectError {
				assert.Nil(t, records)
				assert.EqualError(t, err, ErrZoneNotFound.Error())
			} else {
				require.NoError(t, err)
				assert.True(t, testutils.SameEndpoints(ti.expected, records), "Endpoints not the same: Expected: %+v Records: %+v", ti.expected, records)
			}
		})
	}
}

func testInMemoryValidateChangeBatch(t *testing.T) {
	init := map[string]Zone{
		"org": makeZone(
			"example.org", "8.8.8.8", endpoint.RecordTypeA,
			"example.org", "", endpoint.RecordTypeTXT,
			"foo.org", "bar.org", endpoint.RecordTypeCNAME,
			"foo.bar.org", "5.5.5.5", endpoint.RecordTypeA,
		),
		"com": makeZone("example.com", "another-example.com", endpoint.RecordTypeCNAME),
	}
	for _, ti := range []struct {
		title       string
		expectError bool
		errorType   error
		init        map[string]Zone
		changes     *plan.Changes
		zone        string
	}{
		{
			title:       "no zones, no update",
			expectError: true,
			zone:        "",
			init:        map[string]Zone{},
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrZoneNotFound,
		},
		{
			title:       "zones, no update",
			expectError: true,
			zone:        "",
			init:        init,
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrZoneNotFound,
		},
		{
			title:       "zones, update, wrong Zone",
			expectError: true,
			zone:        "test",
			init:        init,
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrZoneNotFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - already exists",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrRecordAlreadyExists,
		},
		{
			title:       "zones, update, right Zone, invalid batch - record not found for update",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "foo.org",
						Targets:    endpoint.Targets{"4.4.4.4"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "foo.org",
						Targets:    endpoint.Targets{"4.4.4.4"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrRecordNotFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - record not found for update",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "foo.org",
						Targets:    endpoint.Targets{"4.4.4.4"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "foo.org",
						Targets:    endpoint.Targets{"4.4.4.4"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrRecordNotFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - duplicated create",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "foo.org",
						Targets:    endpoint.Targets{"4.4.4.4"},
						RecordType: endpoint.RecordTypeA,
					},
					{
						DNSName:    "foo.org",
						Targets:    endpoint.Targets{"4.4.4.4"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrDuplicateRecordFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - duplicated update/delete",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateOld: []*endpoint.Endpoint{},
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
			errorType: ErrDuplicateRecordFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - duplicated update",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			errorType: ErrDuplicateRecordFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - wrong update old",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{
					{
						DNSName:    "new.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				Delete: []*endpoint.Endpoint{},
			},
			errorType: ErrRecordNotFound,
		},
		{
			title:       "zones, update, right Zone, invalid batch - wrong delete",
			expectError: true,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "new.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
			errorType: ErrRecordNotFound,
		},
		{
			title:       "zones, update, right Zone, valid batch - delete",
			expectError: false,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.org",
						Targets:    endpoint.Targets{"5.5.5.5"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
		},
		{
			title:       "zones, update, right Zone, valid batch - update and create",
			expectError: false,
			zone:        "org",
			init:        init,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.new.org",
						Targets:    endpoint.Targets{"4.8.8.9"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.org",
						Targets:    endpoint.Targets{"4.8.8.4"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateOld: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.org",
						Targets:    endpoint.Targets{"5.5.5.5"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				Delete: []*endpoint.Endpoint{},
			},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			c := &InMemoryClient{}
			c.zones = ti.init
			ichanges := &plan.Changes{
				Create:    ti.changes.Create,
				UpdateNew: ti.changes.UpdateNew,
				UpdateOld: ti.changes.UpdateOld,
				Delete:    ti.changes.Delete,
			}
			err := c.validateChangeBatch(ti.zone, ichanges)
			if ti.expectError {
				assert.EqualError(t, err, ti.errorType.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func getInitData() map[string]Zone {
	return map[string]Zone{
		"org": makeZone("example.org", "8.8.8.8", endpoint.RecordTypeA,
			"example.org", "", endpoint.RecordTypeTXT,
			"foo.org", "4.4.4.4", endpoint.RecordTypeCNAME,
			"foo.bar.org", "5.5.5.5", endpoint.RecordTypeA,
		),
		"com": makeZone("example.com", "4.4.4.4", endpoint.RecordTypeCNAME),
	}
}

func testInMemoryApplyChanges(t *testing.T) {
	for _, ti := range []struct {
		title              string
		expectError        bool
		init               map[string]Zone
		changes            *plan.Changes
		expectedZonesState map[string]Zone
	}{
		{
			title:       "unmatched Zone, should be ignored in the apply step",
			expectError: false,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{{
					DNSName:    "example.de",
					Targets:    endpoint.Targets{"8.8.8.8"},
					RecordType: endpoint.RecordTypeA,
				}},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete:    []*endpoint.Endpoint{},
			},
			expectedZonesState: getInitData(),
		},
		{
			title:       "expect error",
			expectError: true,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
				UpdateOld: []*endpoint.Endpoint{},
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
		},
		{
			title:       "zones, update, right Zone, valid batch - delete",
			expectError: false,
			changes: &plan.Changes{
				Create:    []*endpoint.Endpoint{},
				UpdateNew: []*endpoint.Endpoint{},
				UpdateOld: []*endpoint.Endpoint{},
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.org",
						Targets:    endpoint.Targets{"5.5.5.5"},
						RecordType: endpoint.RecordTypeA,
					},
				},
			},
			expectedZonesState: map[string]Zone{
				"org": makeZone("example.org", "8.8.8.8", endpoint.RecordTypeA,
					"example.org", "", endpoint.RecordTypeTXT,
					"foo.org", "4.4.4.4", endpoint.RecordTypeCNAME,
				),
				"com": makeZone("example.com", "4.4.4.4", endpoint.RecordTypeCNAME),
			},
		},
		{
			title:       "zones, update, right Zone, valid batch - update, create, delete",
			expectError: false,
			changes: &plan.Changes{
				Create: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.new.org",
						Targets:    endpoint.Targets{"4.8.8.9"},
						RecordType: endpoint.RecordTypeA,
						Labels:     endpoint.NewLabels(),
					},
				},
				UpdateNew: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.org",
						Targets:    endpoint.Targets{"4.8.8.4"},
						RecordType: endpoint.RecordTypeA,
						Labels:     endpoint.NewLabels(),
					},
				},
				UpdateOld: []*endpoint.Endpoint{
					{
						DNSName:    "foo.bar.org",
						Targets:    endpoint.Targets{"5.5.5.5"},
						RecordType: endpoint.RecordTypeA,
						Labels:     endpoint.NewLabels(),
					},
				},
				Delete: []*endpoint.Endpoint{
					{
						DNSName:    "example.org",
						Targets:    endpoint.Targets{"8.8.8.8"},
						RecordType: endpoint.RecordTypeA,
						Labels:     endpoint.NewLabels(),
					},
				},
			},
			expectedZonesState: map[string]Zone{
				"org": makeZone(
					"example.org", "", endpoint.RecordTypeTXT,
					"foo.org", "4.4.4.4", endpoint.RecordTypeCNAME,
					"foo.bar.org", "4.8.8.4", endpoint.RecordTypeA,
					"foo.bar.new.org", "4.8.8.9", endpoint.RecordTypeA,
				),
				"com": makeZone("example.com", "4.4.4.4", endpoint.RecordTypeCNAME),
			},
		},
	} {
		t.Run(ti.title, func(t *testing.T) {
			im := NewInMemoryProvider(context.Background())
			c := &InMemoryClient{}
			c.zones = getInitData()
			im.client = c

			err := im.ApplyChanges(context.Background(), ti.changes)
			if ti.expectError {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, ti.expectedZonesState, c.zones)
			}
		})
	}
}

func testNewInMemoryProvider(t *testing.T) {
	cfg := NewInMemoryProvider(context.Background())
	assert.NotNil(t, cfg.client)
}

func testInMemoryCreateZone(t *testing.T) {
	im := NewInMemoryProvider(context.Background())
	err := im.CreateZone("Zone")
	assert.NoError(t, err)
	err = im.CreateZone("Zone")
	assert.EqualError(t, err, ErrZoneAlreadyExists.Error())
}

func makeZone(s ...string) map[endpoint.EndpointKey]*endpoint.Endpoint {
	if len(s)%3 != 0 {
		panic("makeZone arguments must be multiple of 3")
	}

	output := map[endpoint.EndpointKey]*endpoint.Endpoint{}
	for i := 0; i < len(s); i += 3 {
		ep := endpoint.NewEndpoint(s[i], s[i+2], s[i+1])
		output[ep.Key()] = ep
	}

	return output
}
