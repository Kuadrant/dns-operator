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

package registry

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"
	"sigs.k8s.io/external-dns/provider/inmemory"

	"github.com/kuadrant/dns-operator/internal/external-dns/testutils"
)

const (
	testZone    = "test-zone.example.org"
	testTXTZone = "-txt"
)

func TestTXTRegistry(t *testing.T) {
	t.Run("TestNewTXTRegistry", testTXTRegistryNew)
	t.Run("TestRecords", testTXTRegistryRecords)
	t.Run("TestApplyChanges", testTXTRegistryApplyChanges)
	t.Run("TestMissingRecords", testTXTRegistryMissingRecords)
}

func testTXTRegistryNew(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	_, err := NewTXTRegistry(context.Background(), p, "txt", "", "", time.Hour, "", []string{}, []string{}, false, nil)
	require.Error(t, err)

	_, err = NewTXTRegistry(context.Background(), p, "", "txt", "", time.Hour, "", []string{}, []string{}, false, nil)
	require.Error(t, err)

	r, err := NewTXTRegistry(context.Background(), p, "txt", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	require.NoError(t, err)
	assert.Equal(t, p, r.provider)

	r, err = NewTXTRegistry(context.Background(), p, "", "txt", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	require.NoError(t, err)

	_, err = NewTXTRegistry(context.Background(), p, "txt", "txt", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	require.Error(t, err)

	_, ok := r.mapper.(affixNameMapper)
	require.True(t, ok)
	assert.Equal(t, "owner", r.ownerID)
	assert.Equal(t, p, r.provider)

	aesKey := []byte(";k&l)nUC/33:{?d{3)54+,AD?]SX%yh^")
	_, err = NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	require.NoError(t, err)

	_, err = NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, aesKey)
	require.NoError(t, err)

	_, err = NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, true, nil)
	require.Error(t, err)

	r, err = NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, true, aesKey)
	require.NoError(t, err)

	_, ok = r.mapper.(affixNameMapper)
	assert.True(t, ok)
}

func testTXTRegistryRecords(t *testing.T) {
	t.Run("With prefix", testTXTRegistryRecordsPrefixed)
	t.Run("With suffix", testTXTRegistryRecordsSuffixed)
	t.Run("No prefix", testTXTRegistryRecordsNoPrefix)
	t.Run("With templated prefix", testTXTRegistryRecordsPrefixedTemplated)
	t.Run("With templated suffix", testTXTRegistryRecordsSuffixedTemplated)
}

func testTXTRegistryRecordsPrefixed(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	// records in the zone
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// no owner
			// EP1 - random cname in the zone
			newEndpointWithOwnerAndLabels("foo.test-zone.example.org", endpoint.RecordTypeCNAME, "", endpoint.Labels{"foo": "somefoo"}, "foo.loadbalancer.com"),

			// EP2 - random cname in the zone that matches txt record format
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-cname-bar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "baz.test-zone.example.org"),

			// EP3 - txt record that we are not managing
			newEndpointWithOwner("qux.test-zone.example.org", endpoint.RecordTypeTXT, "", "random"),

			// EP4 - TXT record has the wrong format - should not be used
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// owner
			// EP5 - wildcard record
			newEndpointWithOwner("*.wildcard.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foo.loadbalancer.com"),
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-cname-wc.wildcard.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP6 - cname we manage with extra labels
			newEndpointWithOwnerAndLabels("bar.test-zone.example.org", endpoint.RecordTypeCNAME, "", endpoint.Labels{"bar": "somebar"}, "my-domain.com"),
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-cname-bar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP7 - lb cname we manage with setID
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-1"),

			// EP8 - lb cname we manage with setID
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-2"),

			// EP9 - a record that shares name with EP11
			newEndpointWithOwner("dualstack.test-zone.example.org", endpoint.RecordTypeA, "", "1.1.1.1"),
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-a-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// owner-2
			// EP10 - case-sensitive txt prefix for cname and composite target on TXT
			// We aren't generating composite targets anymore - this is a legacy check
			newEndpointWithOwnerAndLabels("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "", endpoint.Labels{"tar": "sometar"}, "tar.loadbalancer.com"),
			newEndpointWithOwner("TxT-3315r53pwfvka0g3-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner-2,external-dns/resource=ingress/default/my-ingress\""),

			// EP11 - aaaa record that shares name with EP9
			newEndpointWithOwner("dualstack.test-zone.example.org", endpoint.RecordTypeAAAA, "", "2001:DB8::1"),
			newEndpointWithOwner("txt-3315r53pwfvka0g3-aaaa-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner-2\""),
		},
	})
	// how we expect the registry to translate records from the zone
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
				"foo":                  "somefoo",
			},
		},
		// EP2
		{
			DNSName:    "txt-1y6zpvr0hylvs1p4-cname-bar.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP3
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP4
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP5
		{
			DNSName:    "*.wildcard.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP6
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
				"bar":                  "somebar",
			},
		},
		// EP7
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb1.loadbalancer.com"},
			SetIdentifier: "test-set-1",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP8
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb2.loadbalancer.com"},
			SetIdentifier: "test-set-2",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP9
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"1.1.1.1"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP10
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
				"tar":                  "sometar",
				"resource":             "ingress/default/my-ingress",
			},
		},
		// EP11
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"2001:DB8::1"},
			RecordType: endpoint.RecordTypeAAAA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "txt-", "", "owner", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	// Ensure prefix is case-insensitive
	r, _ = NewTXTRegistry(context.Background(), p, "TxT-", "", "owner", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryRecordsSuffixed(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// no owner
			// EP1 - random cname that we do not manage
			newEndpointWithOwnerAndLabels("foo.test-zone.example.org", endpoint.RecordTypeCNAME, "", endpoint.Labels{"foo": "somefoo"}, "foo.loadbalancer.com"),

			// EP2 - random cname in the zone that matches txt record format
			newEndpointWithOwner("cname-bar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeCNAME, "", "baz.test-zone.example.org"),

			// EP3 - txt record that we are not managing
			newEndpointWithOwner("qux.test-zone.example.org", endpoint.RecordTypeTXT, "", "random"),

			// EP4 - invalid txt record format - it should not be used
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("foobar-cname-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// owner
			// EP5 - cname that we manage with extra labels
			newEndpointWithOwnerAndLabels("bar.test-zone.example.org", endpoint.RecordTypeCNAME, "", endpoint.Labels{"bar": "somebar"}, "my-domain.com"),
			newEndpointWithOwner("cname-bar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP6 - lb cname we manage with setID
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-1"),

			// EP7 - lb cname we manage with setID
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-2"),

			// EP8 - a record that shares name with EP10
			newEndpointWithOwner("dualstack.test-zone.example.org", endpoint.RecordTypeA, "", "1.1.1.1"),
			newEndpointWithOwner("a-dualstack-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// owner-2
			// EP9 - case sensitive TXT record
			newEndpointWithOwnerAndLabels("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "", endpoint.Labels{"tar": "sometar"}, "tar.loadbalancer.com"),
			newEndpointWithOwner("cname-tar-3315r53pwfvka0g3-TxT.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner-2\""), // case-insensitive TXT suffix

			// EP10 - aaaa record that shares name with EP8
			newEndpointWithOwner("dualstack.test-zone.example.org", endpoint.RecordTypeAAAA, "", "2001:DB8::1"),
			newEndpointWithOwner("aaaa-dualstack-3315r53pwfvka0g3-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner-2\""),
		},
	})
	// compared to prefix missing wildcard case
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
				"foo":                  "somefoo",
			},
		},
		// EP2
		{
			DNSName:    "cname-bar-1y6zpvr0hylvs1p4-txt.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP3
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP4
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP5
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
				"bar":                  "somebar",
			},
		},
		// EP6
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb1.loadbalancer.com"},
			SetIdentifier: "test-set-1",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP7
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb2.loadbalancer.com"},
			SetIdentifier: "test-set-2",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP8
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"1.1.1.1"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP9
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
				"tar":                  "sometar",
			},
		},
		// EP10
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"2001:DB8::1"},
			RecordType: endpoint.RecordTypeAAAA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "-txt", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	// Ensure prefix is case-insensitive
	r, _ = NewTXTRegistry(context.Background(), p, "", "-TxT", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpointLabels(records, expectedRecords))
}

func testTXTRegistryRecordsNoPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	ctx := context.Background()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// no owner
			// EP1 - random cname that we do not manage
			newEndpointWithOwner("foo.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foo.loadbalancer.com"),

			// EP2 - random cname in the zone that matches txt record format
			newEndpointWithOwner("bar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "my-domain.com"),

			// EP3 - txt record that we are not managing
			newEndpointWithOwner("qux.test-zone.example.org", endpoint.RecordTypeTXT, "", "random"),

			// EP4 - invalid txt record format - it should not be used
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("invalid-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP5 - record with prefix when we expect npo prefix
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "tar.loadbalancer.com"),
			newEndpointWithOwner("txt-1y6zpvr0hylvs1p4-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner-2\""),

			// owner
			// EP6 - cname that we manage with extra labels when the prefix is just a part of the hostname
			newEndpointWithOwner("txt.bar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "baz.test-zone.example.org"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-cname-txt.bar.test-zone.example.org", endpoint.RecordTypeTXT, "",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),

			// EP7 - alias A record
			newEndpointWithOwner("alias.test-zone.example.org", endpoint.RecordTypeA, "", "my-domain.com").WithProviderSpecific("alias", "true"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-cname-alias.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP8 - A record that shares hostname with EP9
			newEndpointWithOwner("dualstack.test-zone.example.org", endpoint.RecordTypeA, "", "1.1.1.1"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-a-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// owner-2
			// EP9 - AAAA record that shares the name with EP8
			newEndpointWithOwner("dualstack.test-zone.example.org", endpoint.RecordTypeAAAA, "", "2001:DB8::1"),
			newEndpointWithOwner("3315r53pwfvka0g3-aaaa-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner-2\""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP2
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP3
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP4
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP5
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP6
		{
			DNSName:    "txt.bar.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey:    "owner",
				endpoint.ResourceLabelKey: "ingress/default/my-ingress",
			},
		},
		// EP7
		{
			DNSName:    "alias.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
			ProviderSpecific: []endpoint.ProviderSpecificProperty{
				{
					Name:  "alias",
					Value: "true",
				},
			},
		},
		// EP8
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"1.1.1.1"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP9
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"2001:DB8::1"},
			RecordType: endpoint.RecordTypeAAAA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner-2",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryRecordsPrefixedTemplated(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("foo.test-zone.example.org", endpoint.RecordTypeA, "", "1.1.1.1"),
			newEndpointWithOwner("txt-a.1y6zpvr0hylvs1p4-foo.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"1.1.1.1"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "txt-%{record_type}.", "", "owner", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	r, _ = NewTXTRegistry(context.Background(), p, "TxT-%{record_type}.", "", "owner", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryRecordsSuffixedTemplated(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("bar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "8.8.8.8"),
			newEndpointWithOwner("bar-1y6zpvr0hylvs1p4txtcname.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"8.8.8.8"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "txt%{record_type}", "owner", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	r, _ = NewTXTRegistry(context.Background(), p, "", "TxT%{record_type}", "owner", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryApplyChanges(t *testing.T) {
	t.Run("With Prefix", testTXTRegistryApplyChangesWithPrefix)
	t.Run("With Templated Prefix", testTXTRegistryApplyChangesWithTemplatedPrefix)
	t.Run("With Templated Suffix", testTXTRegistryApplyChangesWithTemplatedSuffix)
	t.Run("With Suffix", testTXTRegistryApplyChangesWithSuffix)
	t.Run("No prefix", testTXTRegistryApplyChangesNoPrefix)
}

func testTXTRegistryApplyChangesWithPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{

			// EP4
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("txt.1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP5
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("txt.1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-1"),

			// EP6 / EP8
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "tar.loadbalancer.com"),
			newEndpointWithOwner("txt.1y6zpvr0hylvs1p4-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP7 / EP9
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("txt.1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-2"),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - a new cname
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),

			// EP2 - a new cname with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),

			// EP3 - a new cname outside the zone
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete: []*endpoint.Endpoint{
			//EP4 - deleting cname
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),

			// EP5 - deleting cname with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP6 - updating cname
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),

			// EP7 - updating cname with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP8 - updating EP6
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "tar.loadbalancer.com"),

			// EP9 - updating old cname with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "", "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),

			// EP2
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\"").WithSetIdentifier("test-set-3"),

			// EP3
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-example", endpoint.RecordTypeTXT, "", "example",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),
		},
		Delete: []*endpoint.Endpoint{
			// EP4
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP5
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP6
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "tar.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress-2\""),

			// EP7
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress-2\"").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP8
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "tar.loadbalancer.com"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP9
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "", "multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-2"),
		},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryApplyChangesWithTemplatedPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "prefix%{record_type}.", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("prefixcname.1y6zpvr0hylvs1p4-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),
		},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryApplyChangesWithTemplatedSuffix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	r, _ := NewTXTRegistry(context.Background(), p, "", "-%{record_type}suffix", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("new-record-1-1y6zpvr0hylvs1p4-cnamesuffix.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),
		},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryApplyChangesWithSuffix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP5
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("cname-foobar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP6
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwner("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-1"),

			// EP9
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "tar.loadbalancer.com"),
			newEndpointWithOwner("cname-tar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP10
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwner("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-2"),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "", "-txt", "owner", time.Hour, "wildcard", []string{}, []string{}, false, nil)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - a new record
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),

			// EP2 - a new record with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),

			// EP3 - a new record outside zone
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),

			// EP4 - a new wildcard record
			newEndpointWithOwnerResource("*.wildcard.test-zone.example.org", endpoint.RecordTypeCNAME, "", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete: []*endpoint.Endpoint{
			// EP5 - delete cname
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),

			// EP6 - delete cname with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP7 - updating new record
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),

			// EP8 - updating new record with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP9 - updating old record
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "tar.loadbalancer.com"),

			// EP10 - updating old record with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("cname-new-record-1-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),

			// EP2
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnedRecord("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\"").WithSetIdentifier("test-set-3"),

			// EP3
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("cname-example-1y6zpvr0hylvs1p4-txt", endpoint.RecordTypeTXT, "example",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),

			// EP4
			newEndpointWithOwnerResource("*.wildcard.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("cname-wildcard-1y6zpvr0hylvs1p4-txt.wildcard.test-zone.example.org", endpoint.RecordTypeTXT, "*.wildcard.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),
		},
		Delete: []*endpoint.Endpoint{
			// EP5
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("cname-foobar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP6
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwnedRecord("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP7
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),
			newEndpointWithOwnedRecord("cname-tar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "tar.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress-2\""),

			// EP8
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnedRecord("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress-2\"").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP9
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "tar.loadbalancer.com"),
			newEndpointWithOwnedRecord("cname-tar-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "tar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP10
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnedRecord("cname-multiple-1y6zpvr0hylvs1p4-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"").WithSetIdentifier("test-set-2"),
		},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryApplyChangesNoPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP4
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - new cname
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "", "new-loadbalancer-1.lb.com"),
			// EP2 - new cname outside zone
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "", "new-loadbalancer-1.lb.com"),
			// EP3 - new cname with alial
			newEndpointWithOwner("new-alias.test-zone.example.org", endpoint.RecordTypeA, "", "my-domain.com").WithProviderSpecific("alias", "true"),
		},
		Delete: []*endpoint.Endpoint{
			// EP4 - delete cname
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("1y6zpvr0hylvs1p4-cname-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP2
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "owner", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("1y6zpvr0hylvs1p4-cname-example", endpoint.RecordTypeTXT, "example", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP3
			newEndpointWithOwner("new-alias.test-zone.example.org", endpoint.RecordTypeA, "owner", "my-domain.com").WithProviderSpecific("alias", "true"),
			// TODO: It's not clear why the TXT registry copies ProviderSpecificProperties to ownership records; that doesn't seem correct.
			newEndpointWithOwnedRecord("1y6zpvr0hylvs1p4-cname-new-alias.test-zone.example.org", endpoint.RecordTypeTXT, "new-alias.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\"").WithProviderSpecific("alias", "true"),
		},
		Delete: []*endpoint.Endpoint{
			// EP4
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func testTXTRegistryMissingRecords(t *testing.T) {
	t.Run("No prefix", testTXTRegistryMissingRecordsNoPrefix)
	t.Run("With Prefix", testTXTRegistryMissingRecordsWithPrefix)
}

func testTXTRegistryMissingRecordsNoPrefix(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - old (V1) format cname
			newEndpointWithOwner("v1format.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foo.loadbalancer.com"),
			newEndpointWithOwner("v1format.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP2 - old (V2) format A record
			newEndpointWithOwner("v2format.test-zone.example.org", endpoint.RecordTypeA, "", "bar.loadbalancer.com"),
			newEndpointWithOwner("a-v2format.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP3 - new (V3) format ns recod
			newEndpointWithOwner("newformat.test-zone.example.org", endpoint.RecordTypeNS, "", "foobar.nameserver.com"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-ns-newformat.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP4 - txt record of new format (V3) that has no endpoint associated - should not be returned
			newEndpointWithOwner("newformat.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP5 - txt record with invalid heritage - will be returned
			newEndpointWithOwner("noheritage.test-zone.example.org", endpoint.RecordTypeTXT, "", "random"),

			// EP6 - old (V2) format with a different owner
			newEndpointWithOwner("oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeA, "", "bar.loadbalancer.com"),
			newEndpointWithOwner("a-oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=otherowner\""),

			// EP7 - unmanaged A record
			endpoint.NewEndpoint("unmanaged1.test-zone.example.org", endpoint.RecordTypeA, "unmanaged1.loadbalancer.com"),

			// EP8 - unmanaged cname
			endpoint.NewEndpoint("unmanaged2.test-zone.example.org", endpoint.RecordTypeCNAME, "unmanaged2.loadbalancer.com"),

			// EP9 - long cname
			newEndpointWithOwner("a-63-characters-label-that-should-work.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foo.loadbalancer.com"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-cname-a-63-characters-label-that-should-work.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "v1format.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
			ProviderSpecific: []endpoint.ProviderSpecificProperty{
				{
					Name:  "txt/force-update",
					Value: "true",
				},
			},
		},
		// EP2
		{
			DNSName:    "v2format.test-zone.example.org",
			Targets:    endpoint.Targets{"bar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
			ProviderSpecific: []endpoint.ProviderSpecificProperty{
				{
					Name:  "txt/force-update",
					Value: "true",
				},
			},
		},
		// EP3
		{
			DNSName:    "newformat.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.nameserver.com"},
			RecordType: endpoint.RecordTypeNS,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP5
		{
			DNSName:    "noheritage.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				// No owner because it's not external-dns heritage
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP6
		{
			DNSName:    "oldformat-otherowner.test-zone.example.org",
			Targets:    endpoint.Targets{"bar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				// Records() retrieves all the records of the zone, no matter the owner
				endpoint.OwnerLabelKey: "otherowner",
			},
		},
		// EP7
		{
			DNSName:    "unmanaged1.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged1.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
		},
		// EP8
		{
			DNSName:    "unmanaged2.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged2.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
		},
		// EP9
		{
			DNSName:    "a-63-characters-label-that-should-work.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "wc", []string{endpoint.RecordTypeCNAME, endpoint.RecordTypeA, endpoint.RecordTypeNS}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryMissingRecordsWithPrefix(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - old (v1) format cname
			newEndpointWithOwner("v1format.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foo.loadbalancer.com"),
			newEndpointWithOwner("txt.v1format.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP2 - old (V2) format a record
			newEndpointWithOwner("v2format2.test-zone.example.org", endpoint.RecordTypeA, "", "bar.loadbalancer.com"),
			newEndpointWithOwner("txt.a-v2format2.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP3 - new (V3) format ns record
			newEndpointWithOwner("newformat.test-zone.example.org", endpoint.RecordTypeNS, "", "foobar.nameserver.com"),
			newEndpointWithOwner("txt.1y6zpvr0hylvs1p4-ns-newformat.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP4 - TXT record with invalid herritage will be returned
			newEndpointWithOwner("oldformat3.test-zone.example.org", endpoint.RecordTypeTXT, "", "random"),

			// EP5 - TXT record of old (V1) format with no endpoint - not returned
			newEndpointWithOwner("txt.oldformat3.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP6 - TXT record of old (V2) format with no endpoint - not returned
			newEndpointWithOwner("txt.cname-oldformat3.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP7 - TXT record of new format (V3) with no endpoin - not returned
			newEndpointWithOwner("txt.newformat.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),

			// EP8 - TXT record with invalid heritage - returned
			newEndpointWithOwner("noheritage.test-zone.example.org", endpoint.RecordTypeTXT, "", "random"),

			// EP9 - old format (V1) a record
			newEndpointWithOwner("oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeA, "", "bar.loadbalancer.com"),
			newEndpointWithOwner("txt.oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=otherowner\""),

			// EP10 - unmanaged a record
			endpoint.NewEndpoint("unmanaged1.test-zone.example.org", endpoint.RecordTypeA, "unmanaged1.loadbalancer.com"),

			// EP11 - unmanaged cname record
			endpoint.NewEndpoint("unmanaged2.test-zone.example.org", endpoint.RecordTypeCNAME, "unmanaged2.loadbalancer.com"),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "v1format.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				// owner was added from the TXT record's target
				endpoint.OwnerLabelKey: "owner",
			},
			ProviderSpecific: []endpoint.ProviderSpecificProperty{
				{
					Name:  "txt/force-update",
					Value: "true",
				},
			},
		},
		// EP2
		{
			DNSName:    "v2format2.test-zone.example.org",
			Targets:    endpoint.Targets{"bar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
			ProviderSpecific: []endpoint.ProviderSpecificProperty{
				{
					Name:  "txt/force-update",
					Value: "true",
				},
			},
		},
		// EP3
		{
			DNSName:    "newformat.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.nameserver.com"},
			RecordType: endpoint.RecordTypeNS,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
		},
		// EP4
		{
			DNSName:    "oldformat3.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner",
			},
			ProviderSpecific: []endpoint.ProviderSpecificProperty{
				{
					Name:  "txt/force-update",
					Value: "true",
				},
			},
		},
		// EP8
		{
			DNSName:    "noheritage.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				// No owner because it's not external-dns heritage
				endpoint.OwnerLabelKey: "",
			},
		},
		// EP9
		{
			DNSName:    "oldformat-otherowner.test-zone.example.org",
			Targets:    endpoint.Targets{"bar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				// All the records of the zone are retrieved, no matter the owner
				endpoint.OwnerLabelKey: "otherowner",
			},
		},
		// EP10
		{
			DNSName:    "unmanaged1.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged1.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
		},
		// EP11
		{
			DNSName:    "unmanaged2.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged2.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner", time.Hour, "wc", []string{endpoint.RecordTypeCNAME, endpoint.RecordTypeA, endpoint.RecordTypeNS, endpoint.RecordTypeTXT}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func TestCacheMethods(t *testing.T) {
	cache := []*endpoint.Endpoint{
		newEndpointWithOwner("thing.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing1.com", "A", "owner", "1.2.3.6"),
		newEndpointWithOwner("thing2.com", "CNAME", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing3.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing4.com", "A", "owner", "1.2.3.4"),
	}
	registry := &TXTRegistry{
		recordsCache:  cache,
		cacheInterval: time.Hour,
	}

	expectedCacheAfterAdd := []*endpoint.Endpoint{
		newEndpointWithOwner("thing.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing1.com", "A", "owner", "1.2.3.6"),
		newEndpointWithOwner("thing2.com", "CNAME", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing3.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing4.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing4.com", "AAAA", "owner", "2001:DB8::1"),
		newEndpointWithOwner("thing5.com", "A", "owner", "1.2.3.5"),
	}

	expectedCacheAfterUpdate := []*endpoint.Endpoint{
		newEndpointWithOwner("thing1.com", "A", "owner", "1.2.3.6"),
		newEndpointWithOwner("thing2.com", "CNAME", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing3.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing4.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing5.com", "A", "owner", "1.2.3.5"),
		newEndpointWithOwner("thing.com", "A", "owner2", "1.2.3.6"),
		newEndpointWithOwner("thing4.com", "AAAA", "owner", "2001:DB8::2"),
	}

	expectedCacheAfterDelete := []*endpoint.Endpoint{
		newEndpointWithOwner("thing1.com", "A", "owner", "1.2.3.6"),
		newEndpointWithOwner("thing2.com", "CNAME", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing3.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing4.com", "A", "owner", "1.2.3.4"),
		newEndpointWithOwner("thing5.com", "A", "owner", "1.2.3.5"),
	}
	// test add cache
	registry.addToCache(newEndpointWithOwner("thing4.com", "AAAA", "owner", "2001:DB8::1"))
	registry.addToCache(newEndpointWithOwner("thing5.com", "A", "owner", "1.2.3.5"))

	if !reflect.DeepEqual(expectedCacheAfterAdd, registry.recordsCache) {
		t.Fatalf("expected endpoints should match endpoints from cache: expected %v, but got %v", expectedCacheAfterAdd, registry.recordsCache)
	}

	// test update cache
	registry.removeFromCache(newEndpointWithOwner("thing.com", "A", "owner", "1.2.3.4"))
	registry.addToCache(newEndpointWithOwner("thing.com", "A", "owner2", "1.2.3.6"))
	registry.removeFromCache(newEndpointWithOwner("thing4.com", "AAAA", "owner", "2001:DB8::1"))
	registry.addToCache(newEndpointWithOwner("thing4.com", "AAAA", "owner", "2001:DB8::2"))
	// ensure it was updated
	if !reflect.DeepEqual(expectedCacheAfterUpdate, registry.recordsCache) {
		t.Fatalf("expected endpoints should match endpoints from cache: expected %v, but got %v", expectedCacheAfterUpdate, registry.recordsCache)
	}

	// test deleting a record
	registry.removeFromCache(newEndpointWithOwner("thing.com", "A", "owner2", "1.2.3.6"))
	registry.removeFromCache(newEndpointWithOwner("thing4.com", "AAAA", "owner", "2001:DB8::2"))
	// ensure it was deleted
	if !reflect.DeepEqual(expectedCacheAfterDelete, registry.recordsCache) {
		t.Fatalf("expected endpoints should match endpoints from cache: expected %v, but got %v", expectedCacheAfterDelete, registry.recordsCache)
	}
}

func TestDropPrefix(t *testing.T) {
	mapper := newaffixNameMapper("foo-", "", "")
	id := "id"
	expectedOutput := "test.example.com"

	tests := []string{
		"foo-id-cname-test.example.com",
		"foo-a-test.example.com",
		"foo-id-test.example.com",
	}

	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			actualOutput, _ := mapper.dropAffixExtractType(tc, id)
			assert.Equal(t, expectedOutput, actualOutput)
		})
	}
}

func TestDropSuffix(t *testing.T) {
	mapper := newaffixNameMapper("", "-%{record_type}-foo", "")
	setID := "set.id"
	expectedOutput := "test.example.com"

	tests := []string{
		"test-a-foo.example.com",
		"test--foo.example.com",
	}

	for _, tc := range tests {
		t.Run(tc, func(t *testing.T) {
			r := strings.SplitN(tc, ".", 2)
			rClean, _ := mapper.dropAffixExtractType(r[0], setID)
			actualOutput := rClean + "." + r[1]
			assert.Equal(t, expectedOutput, actualOutput)
		})
	}
}

func TestExtractRecordTypeDefaultPosition(t *testing.T) {
	tests := []struct {
		input        string
		expectedName string
		expectedType string
	}{
		{
			input:        "ns-zone.example.com",
			expectedName: "zone.example.com",
			expectedType: "NS",
		},
		{
			input:        "aaaa-zone.example.com",
			expectedName: "zone.example.com",
			expectedType: "AAAA",
		},
		{
			input:        "ptr-zone.example.com",
			expectedName: "ptr-zone.example.com",
			expectedType: "",
		},
		{
			input:        "zone.example.com",
			expectedName: "zone.example.com",
			expectedType: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actualName, actualType := extractRecordTypeDefaultPosition(tc.input)
			assert.Equal(t, tc.expectedName, actualName)
			assert.Equal(t, tc.expectedType, actualType)
		})
	}
}

func TestToEndpointNameNewTXT(t *testing.T) {
	tests := []struct {
		name       string
		mapper     affixNameMapper
		domain     string
		txtDomain  string
		recordType string
		id         string
	}{
		{
			name:       "prefix",
			mapper:     newaffixNameMapper("foo", "", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "fooid-a-example.com",
			id:         "id",
		},
		{
			name:       "suffix",
			mapper:     newaffixNameMapper("", "foo", ""),
			domain:     "example.com",
			recordType: "AAAA",
			txtDomain:  "aaaa-example-idfoo.com",
			id:         "id",
		},
		{
			name:       "prefix with dash",
			mapper:     newaffixNameMapper("foo-", "", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "foo-id-a-example.com",
			id:         "id",
		},
		{
			name:       "suffix with dash",
			mapper:     newaffixNameMapper("", "-foo", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cname-example-id-foo.com",
			id:         "id",
		},
		{
			name:       "prefix with dot",
			mapper:     newaffixNameMapper("foo.", "", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "foo.id-cname-example.com",
			id:         "id",
		},
		{
			name:       "suffix with dot",
			mapper:     newaffixNameMapper("", ".foo", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cname-example-id.foo.com",
			id:         "id",
		},
		{
			name:       "prefix with multiple dots",
			mapper:     newaffixNameMapper("foo.bar.", "", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "foo.bar.id-cname-example.com",
			id:         "id",
		},
		{
			name:       "suffix with multiple dots",
			mapper:     newaffixNameMapper("", ".foo.bar.test", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cname-example-id.foo.bar.test.com",
			id:         "id",
		},
		{
			name:       "templated prefix",
			mapper:     newaffixNameMapper("%{record_type}-foo", "", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "a-fooid-example.com",
			id:         "id",
		},
		{
			name:       "templated suffix",
			mapper:     newaffixNameMapper("", "foo-%{record_type}", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "example-idfoo-a.com",
			id:         "id",
		},
		{
			name:       "templated prefix with dot",
			mapper:     newaffixNameMapper("%{record_type}foo.", "", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cnamefoo.id-example.com",
			id:         "id",
		},
		{
			name:       "templated suffix with dot",
			mapper:     newaffixNameMapper("", ".foo%{record_type}", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "example-id.fooa.com",
			id:         "id",
		},
		{
			name:       "templated prefix with multiple dots",
			mapper:     newaffixNameMapper("bar.%{record_type}.foo.", "", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "bar.cname.foo.id-example.com",
			id:         "id",
		},
		{
			name:       "templated suffix with multiple dots",
			mapper:     newaffixNameMapper("", ".foo%{record_type}.bar", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "example-id.fooa.bar.com",
			id:         "id",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			txtDomain := tc.mapper.toNewTXTName(tc.domain, tc.id, tc.recordType)
			assert.Equal(t, tc.txtDomain, txtDomain)

			domain, _ := tc.mapper.toEndpointName(txtDomain, tc.id)
			assert.Equal(t, tc.domain, domain)
		})
	}
}

func TestNewTXTScheme(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP3
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			newEndpointWithOwner("1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "", "new-loadbalancer-1.lb.com"),
			// EP2
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "", "new-loadbalancer-1.lb.com"),
		},
		Delete: []*endpoint.Endpoint{
			// EP3
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnerAndOwnedRecord("1y6zpvr0hylvs1p4-cname-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "", "new-record-1.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),
			// EP2
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "owner", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnerAndOwnedRecord("1y6zpvr0hylvs1p4-cname-example", endpoint.RecordTypeTXT, "", "example", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
		Delete: []*endpoint.Endpoint{
			// EP3
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
			newEndpointWithOwnerAndOwnedRecord("1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner\""),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Create":    expected.Create,
			"UpdateNew": expected.UpdateNew,
			"UpdateOld": expected.UpdateOld,
			"Delete":    expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Create":    got.Create,
			"UpdateNew": got.UpdateNew,
			"UpdateOld": got.UpdateOld,
			"Delete":    got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

func TestGenerateTXT(t *testing.T) {
	record := newEndpointWithOwner("foo.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "new-foo.loadbalancer.com")
	expectedTXT := []*endpoint.Endpoint{
		{
			DNSName:    "1y6zpvr0hylvs1p4-cname-foo.test-zone.example.org",
			Targets:    endpoint.Targets{"\"heritage=external-dns,external-dns/owner=owner\""},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnedRecordLabelKey: "foo.test-zone.example.org",
			},
		},
	}
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	gotTXT := r.generateTXTRecord(record)
	assert.Equal(t, expectedTXT, gotTXT)
}

func TestGenerateTXTForAAAA(t *testing.T) {
	record := newEndpointWithOwner("foo.test-zone.example.org", endpoint.RecordTypeAAAA, "owner", "2001:DB8::1")
	expectedTXT := []*endpoint.Endpoint{
		{
			DNSName:    "1y6zpvr0hylvs1p4-aaaa-foo.test-zone.example.org",
			Targets:    endpoint.Targets{"\"heritage=external-dns,external-dns/owner=owner\""},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnedRecordLabelKey: "foo.test-zone.example.org",
			},
		},
	}
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	gotTXT := r.generateTXTRecord(record)
	assert.Equal(t, expectedTXT, gotTXT)
}

func TestFailGenerateTXT(t *testing.T) {

	cnameRecord := &endpoint.Endpoint{
		DNSName:    "foo-some-really-big-name-not-supported-and-will-fail-000000000000000000.test-zone.example.org",
		Targets:    endpoint.Targets{"new-foo.loadbalancer.com"},
		RecordType: endpoint.RecordTypeCNAME,
		Labels:     map[string]string{},
	}
	// A bad DNS name returns empty expected TXT
	expectedTXT := []*endpoint.Endpoint{}
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner", time.Hour, "", []string{}, []string{}, false, nil)
	gotTXT := r.generateTXTRecord(cnameRecord)
	assert.Equal(t, expectedTXT, gotTXT)
}

func TestTXTRegistryApplyChangesEncrypt(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)

	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "", "foobar.loadbalancer.com"),
			// joined target:
			// owner: owner
			// txt-encryption-nonce: h8UQ6jelUFUsEIn7
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "foobar.test-zone.example.org", "\"h8UQ6jelUFUsEIn7SbFktc2MYXPx/q8lySqI4VwfVtVaIbb2nkHWV/88KKbuLtu7fJNzMir8ELVeVnRSY01KdiIuj7ledqZe5ailEjQaU5Z6uEKd5pgs6sH8\""),
		},
	})

	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner", time.Hour, "", []string{}, []string{}, true, []byte("12345678901234567890123456789012"))
	records, _ := r.Records(ctx)
	changes := &plan.Changes{
		Delete: records,
	}

	// ensure that encryption nonce gets reused when deleting records
	expected := &plan.Changes{
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner", "foobar.loadbalancer.com"),
			// should not be split into two targets - second label is a nonce
			newEndpointWithOwnerAndOwnedRecord("txt.1y6zpvr0hylvs1p4-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "", "foobar.test-zone.example.org", "\"h8UQ6jelUFUsEIn7SbFktc2MYXPx/q8lySqI4VwfVtVaIbb2nkHWV/88KKbuLtu7fJNzMir8ELVeVnRSY01KdiIuj7ledqZe5ailEjQaU5Z6uEKd5pgs6sH8\""),
		},
	}

	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		mExpected := map[string][]*endpoint.Endpoint{
			"Delete": expected.Delete,
		}
		mGot := map[string][]*endpoint.Endpoint{
			"Delete": got.Delete,
		}
		assert.True(t, testutils.SamePlanChanges(mGot, mExpected))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
}

// TestMultiClusterDifferentRecordTypeOwnership validates the registry handles environments where the same zone is managed by
// external-dns in different clusters and the ingress record type is different. For example one uses A records and the other
// uses CNAME. In this environment the first cluster that establishes the owner record should maintain ownership even
// if the same ingress host is deployed to the other. With the introduction of Dual Record support each record type
// was treated independently and would cause each cluster to fight over ownership. This tests ensure that the default
// Dual Stack record support only treats AAAA records independently and while keeping A and CNAME record ownership intact.
func TestMultiClusterDifferentRecordTypeOwnership(t *testing.T) {
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// records on cluster using A record for ingress address
			newEndpointWithOwner("bar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=cat,external-dns/resource=ingress/default/foo\"", endpoint.RecordTypeTXT, ""),
			newEndpointWithOwner("bar.test-zone.example.org", "1.2.3.4", endpoint.RecordTypeA, ""),
		},
	})

	r, _ := NewTXTRegistry(context.Background(), p, "_owner.", "", "bar", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	// new cluster has same ingress host as other cluster and uses CNAME ingress address
	cname := &endpoint.Endpoint{
		DNSName:    "bar.test-zone.example.org",
		Targets:    endpoint.Targets{"cluster-b"},
		RecordType: "CNAME",
		Labels: map[string]string{
			endpoint.ResourceLabelKey: "ingress/default/foo-127",
		},
	}
	desired := []*endpoint.Endpoint{cname}

	pl := &plan.Plan{
		Policies:       []plan.Policy{&plan.SyncPolicy{}},
		Current:        records,
		Desired:        desired,
		ManagedRecords: []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME},
	}

	changes := pl.Calculate()
	p.OnApplyChanges = func(ctx context.Context, changes *plan.Changes) {
		got := map[string][]*endpoint.Endpoint{
			"Create":    changes.Create,
			"UpdateNew": changes.UpdateNew,
			"UpdateOld": changes.UpdateOld,
			"Delete":    changes.Delete,
		}
		expected := map[string][]*endpoint.Endpoint{
			"Create":    {},
			"UpdateNew": {},
			"UpdateOld": {},
			"Delete":    {},
		}
		testutils.SamePlanChanges(got, expected)
	}

	err := r.ApplyChanges(ctx, changes.Changes)
	if err != nil {
		t.Error(err)
	}
}

/**

helper methods

*/

func newEndpointWithOwner(dnsName, recordType, ownerID string, targets ...string) *endpoint.Endpoint {
	return newEndpointWithOwnerAndLabels(dnsName, recordType, ownerID, nil, targets...)
}

func newEndpointWithOwnerAndOwnedRecord(dnsName, recordType, ownerID, ownedRecord string, targets ...string) *endpoint.Endpoint {
	return newEndpointWithOwnerAndLabels(dnsName, recordType, ownerID, endpoint.Labels{endpoint.OwnedRecordLabelKey: ownedRecord}, targets...)
}

func newEndpointWithOwnedRecord(dnsName, recordType, ownedRecord string, targets ...string) *endpoint.Endpoint {
	return newEndpointWithLabels(dnsName, recordType, endpoint.Labels{endpoint.OwnedRecordLabelKey: ownedRecord}, targets...)
}

func newEndpointWithOwnerAndLabels(dnsName, recordType, ownerID string, labels endpoint.Labels, targets ...string) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, recordType, targets...)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	for k, v := range labels {
		e.Labels[k] = v
	}
	return e
}

func newEndpointWithLabels(dnsName, recordType string, labels endpoint.Labels, targets ...string) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, recordType, targets...)
	for k, v := range labels {
		e.Labels[k] = v
	}
	return e
}

func newEndpointWithOwnerResource(dnsName, recordType, ownerID, resource string, targets ...string) *endpoint.Endpoint {
	e := endpoint.NewEndpoint(dnsName, recordType, targets...)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	e.Labels[endpoint.ResourceLabelKey] = resource
	return e
}
