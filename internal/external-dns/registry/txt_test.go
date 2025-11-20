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
}

func testTXTRegistryRecords(t *testing.T) {
	t.Run("With prefix [new format + old format]", testTXTRegistryRecordsPrefixed)
	t.Run("With suffix [old format]", testTXTRegistryRecordsSuffixed)
	t.Run("No prefix [old format]", testTXTRegistryRecordsNoPrefix)
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
			newEndpointWithLabels("foo.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{"foo": "somefoo"}, "foo.loadbalancer.com"),

			// EP2 - random cname in the zone that matches txt record format
			endpoint.NewEndpoint("txt-2tqs20a7-cname-bar.test-zone.example.org", endpoint.RecordTypeCNAME, "baz.test-zone.example.org"),

			// EP3 - txt record that we are not managing
			endpoint.NewEndpoint("qux.test-zone.example.org", endpoint.RecordTypeTXT, "random"),

			// EP4 - TXT record has the wrong format - should not be used
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeTXT),

			// owner1
			// EP5 - wildcard record
			endpoint.NewEndpoint("*.wildcard.test-zone.example.org", endpoint.RecordTypeCNAME, "foo.loadbalancer.com"),
			endpoint.NewEndpoint("txt-2tqs20a7-cname-wc.wildcard.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP6 - cname we manage with extra labels
			newEndpointWithLabels("bar.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{"bar": "somebar"}, "my-domain.com"),
			endpoint.NewEndpoint("txt-cname-bar.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP7 - lb cname we manage with setID
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			endpoint.NewEndpoint("txt-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\"").WithSetIdentifier("test-set-1"),

			// EP8 - lb cname we manage with setID
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			endpoint.NewEndpoint("txt-2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\"").WithSetIdentifier("test-set-2"),

			// EP9 - a record that shares name with EP11
			endpoint.NewEndpoint("dualstack.test-zone.example.org", endpoint.RecordTypeA, "1.1.1.1"),
			endpoint.NewEndpoint("txt-2tqs20a7-a-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// owner2
			// EP10 - case-sensitive txt prefix for cname and composite target on TXT
			// We aren't generating composite targets anymore - this is a legacy check
			newEndpointWithLabels("tar.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{"tar": "sometar"}, "tar.loadbalancer.com"),
			endpoint.NewEndpoint("TxT-b1e3677c-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1,external-dns/resource=ingress/default/my-ingress\""),

			// EP11 - aaaa record that shares name with EP9
			endpoint.NewEndpoint("dualstack.test-zone.example.org", endpoint.RecordTypeAAAA, "2001:DB8::1"),
			endpoint.NewEndpoint("txt-aaaa-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2\""),
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
				"foo": "somefoo",
			},
		},
		// EP2
		{
			DNSName:    "txt-2tqs20a7-cname-bar.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP3
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels:     map[string]string{},
		},
		// EP4
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP5
		{
			DNSName:    "*.wildcard.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP6
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				"bar":                  "somebar",
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP7
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb1.loadbalancer.com"},
			SetIdentifier: "test-set-1",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP8
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb2.loadbalancer.com"},
			SetIdentifier: "test-set-2",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP9
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"1.1.1.1"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP10
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				// "resource": "ingress/default/my-ingress" // this label is ignored as it is from a different owner
				"tar":                  "sometar",
				endpoint.OwnerLabelKey: "owner2",
			},
		},
		// EP11
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"2001:DB8::1"},
			RecordType: endpoint.RecordTypeAAAA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner2",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "txt-", "", "owner1", time.Hour, "wc", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	// Ensure prefix is case-insensitive
	r, _ = NewTXTRegistry(context.Background(), p, "TxT-", "", "owner1", time.Hour, "wc", []string{}, []string{}, false, nil)
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
			newEndpointWithLabels("foo.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{"foo": "somefoo"}, "foo.loadbalancer.com"),

			// EP2 - random cname in the zone that matches txt record format
			endpoint.NewEndpoint("cname-bar-txt.test-zone.example.org", endpoint.RecordTypeCNAME, "baz.test-zone.example.org"),

			// EP3 - txt record that we are not managing
			endpoint.NewEndpoint("qux.test-zone.example.org", endpoint.RecordTypeTXT, "random"),

			// EP4 - invalid txt record format - it should not be used
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("foobar-cname-txt.test-zone.example.org", endpoint.RecordTypeTXT),

			// owner1
			// EP5 - cname that we manage with extra labels
			newEndpointWithLabels("bar.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{"bar": "somebar"}, "my-domain.com"),
			endpoint.NewEndpoint("cname-bar-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP6 - lb cname we manage with setID
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			endpoint.NewEndpoint("cname-multiple-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\"").WithSetIdentifier("test-set-1"),

			// EP7 - lb cname we manage with setID
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			endpoint.NewEndpoint("cname-multiple-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\"").WithSetIdentifier("test-set-2"),

			// EP8 - a record that shares name with EP10
			endpoint.NewEndpoint("dualstack.test-zone.example.org", endpoint.RecordTypeA, "1.1.1.1"),
			endpoint.NewEndpoint("a-dualstack-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// owner2
			// EP9 - case sensitive TXT record
			newEndpointWithLabels("tar.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{"tar": "sometar"}, "tar.loadbalancer.com"),
			endpoint.NewEndpoint("cname-tar-TxT.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2\""), // case-insensitive TXT suffix

			// EP10 - aaaa record that shares name with EP8
			endpoint.NewEndpoint("dualstack.test-zone.example.org", endpoint.RecordTypeAAAA, "2001:DB8::1"),
			endpoint.NewEndpoint("aaaa-dualstack-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2\""),
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
				"foo": "somefoo",
			},
		},
		// EP2
		{
			DNSName:    "cname-bar-txt.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP3
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels:     map[string]string{},
		},
		// EP4
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP5
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				"bar":                  "somebar",
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP6
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb1.loadbalancer.com"},
			SetIdentifier: "test-set-1",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP7
		{
			DNSName:       "multiple.test-zone.example.org",
			Targets:       endpoint.Targets{"lb2.loadbalancer.com"},
			SetIdentifier: "test-set-2",
			RecordType:    endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP8
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"1.1.1.1"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP9
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				"tar":                  "sometar",
				endpoint.OwnerLabelKey: "owner2",
			},
		},
		// EP10
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"2001:DB8::1"},
			RecordType: endpoint.RecordTypeAAAA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner2",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "-txt", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))

	// Ensure prefix is case-insensitive
	r, _ = NewTXTRegistry(context.Background(), p, "", "-TxT", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ = r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryRecordsNoPrefix(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	ctx := context.Background()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// no owner
			// EP1 - random cname that we do not manage
			endpoint.NewEndpoint("foo.test-zone.example.org", endpoint.RecordTypeCNAME, "foo.loadbalancer.com"),

			// EP2 - random cname in the zone that matches txt record format
			endpoint.NewEndpoint("bar.test-zone.example.org", endpoint.RecordTypeCNAME, "my-domain.com"),

			// EP3 - txt record that we are not managing
			endpoint.NewEndpoint("qux.test-zone.example.org", endpoint.RecordTypeTXT, "random"),

			// EP4 - invalid txt record format - it should not be used
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("invalid-foobar.test-zone.example.org", endpoint.RecordTypeTXT),

			// EP5 - record with prefix when we expect no prefix
			endpoint.NewEndpoint("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "tar.loadbalancer.com"),
			endpoint.NewEndpoint("txt-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT),

			// owner
			// EP6 - cname that we manage with multiple targets on TXT
			endpoint.NewEndpoint("txt.bar.test-zone.example.org", endpoint.RecordTypeCNAME, "baz.test-zone.example.org"),
			endpoint.NewEndpoint("cname-txt.bar.test-zone.example.org", endpoint.RecordTypeTXT,
				"\"heritage=external-dns,external-dns/foo=bar\"",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\"",
				"\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP7 - alias A record
			endpoint.NewEndpoint("alias.test-zone.example.org", endpoint.RecordTypeA, "my-domain.com").WithProviderSpecific("alias", "true"),
			endpoint.NewEndpoint("cname-alias.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP8 - A record that shares hostname with EP9
			endpoint.NewEndpoint("dualstack.test-zone.example.org", endpoint.RecordTypeA, "1.1.1.1"),
			endpoint.NewEndpoint("a-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// owner-2
			// EP9 - AAAA record that shares the name with EP8
			endpoint.NewEndpoint("dualstack.test-zone.example.org", endpoint.RecordTypeAAAA, "2001:DB8::1"),
			endpoint.NewEndpoint("aaaa-dualstack.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2\""),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "foo.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP2
		{
			DNSName:    "bar.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP3
		{
			DNSName:    "qux.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels:     map[string]string{},
		},
		// EP4
		{
			DNSName:    "foobar.test-zone.example.org",
			Targets:    endpoint.Targets{"foobar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP5
		{
			DNSName:    "tar.test-zone.example.org",
			Targets:    endpoint.Targets{"tar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     map[string]string{},
		},
		// EP6
		{
			DNSName:    "txt.bar.test-zone.example.org",
			Targets:    endpoint.Targets{"baz.test-zone.example.org"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				"foo":                     "bar",
				endpoint.ResourceLabelKey: "ingress/default/my-ingress",
				endpoint.OwnerLabelKey:    "owner1",
			},
		},
		// EP7
		{
			DNSName:    "alias.test-zone.example.org",
			Targets:    endpoint.Targets{"my-domain.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
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
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP9
		{
			DNSName:    "dualstack.test-zone.example.org",
			Targets:    endpoint.Targets{"2001:DB8::1"},
			RecordType: endpoint.RecordTypeAAAA,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner2",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	records, _ := r.Records(ctx)

	assert.True(t, testutils.SameEndpoints(records, expectedRecords))
}

func testTXTRegistryApplyChanges(t *testing.T) {
	t.Run("Multiple owners", testTXTRegistryApplyChangesMultipleOwners)
	t.Run("With Prefix", testTXTRegistryApplyChangesWithPrefix)
	t.Run("With Templated Prefix", testTXTRegistryApplyChangesWithTemplatedPrefix)
	t.Run("With Templated Suffix", testTXTRegistryApplyChangesWithTemplatedSuffix)
	t.Run("With Suffix", testTXTRegistryApplyChangesWithSuffix)
	t.Run("No prefix", testTXTRegistryApplyChangesNoPrefix)
}

func testTXTRegistryApplyChangesMultipleOwners(t *testing.T) {
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{

			// EP1
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("txt.2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP2
			endpoint.NewEndpoint("txt.b1e3677c-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1\""),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	// Update registry with content of the zone (create request above)
	// otherwise will be creating exising TXT records
	_, _ = r.Records(ctx)

	changes := &plan.Changes{
		UpdateNew: []*endpoint.Endpoint{
			// EP1 / EP2 cname is owner by two. On delete of one of the owners plan will instead move cname into update new without deleted owner
			// simulate this scenario and expect there to be a delete on the TXT record
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner2", "foobar.loadbalancer.com"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1&&owner2", "foobar.loadbalancer.com"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{},
		Delete: []*endpoint.Endpoint{
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org", "\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
		},
		UpdateNew: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner2", "foobar.loadbalancer.com"),
		},
		UpdateOld: []*endpoint.Endpoint{
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1&&owner2", "foobar.loadbalancer.com"),
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
		assert.True(t, testutils.SameEndpoints(mGot["Create"], mExpected["Create"]))
		assert.True(t, testutils.SameEndpoints(mGot["UpdateNew"], mExpected["UpdateNew"]))
		assert.True(t, testutils.SameEndpoints(mGot["UpdateOld"], mExpected["UpdateOld"]))
		assert.True(t, testutils.SameEndpoints(mGot["Delete"], mExpected["Delete"]))
		assert.Equal(t, nil, ctx.Value(provider.RecordsContextKey))
	}
	err := r.ApplyChanges(ctx, changes)
	require.NoError(t, err)
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
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("txt.2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT,
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP5
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			endpoint.NewEndpoint("txt.2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT,
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\"").WithSetIdentifier("test-set-1"),

			// EP6 / EP8
			endpoint.NewEndpoint("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "tar.loadbalancer.com"),
			// do not create this TXT record. Simulates scenario when CNAME record already exists from another owner
			// on plan.Calsulate() the cname record will be moved to update, however TXTs are unique per owner
			// so we should have this appear in create request instead of an update
			// newEndpointWithOwner("txt.2tqs20a7-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "",
			//				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP7 / EP9
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			endpoint.NewEndpoint("txt.2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT,
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\"").WithSetIdentifier("test-set-2"),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	// Update registry with content of the zone (create request above)
	// otherwise will be creating exising TXT records
	_, _ = r.Records(ctx)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - a new cname
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),

			// EP2 - a new cname with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),

			// EP3 - a new cname outside the zone
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete: []*endpoint.Endpoint{
			//EP4 - deleting cname
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),

			// EP5 - deleting cname with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP6 - updating cname
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1&&owner2", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),

			// EP7 - updating cname with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP8 - updating EP6
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner2", "tar.loadbalancer.com"),

			// EP9 - updating old cname with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/resource=ingress/default/my-ingress,external-dns/version=1\""),

			// EP2
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/resource=ingress/default/my-ingress,external-dns/version=1\"").WithSetIdentifier("test-set-3"),

			// EP3
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-example", endpoint.RecordTypeTXT, "example",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/resource=ingress/default/my-ingress,external-dns/version=1\""),

			// EP6
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "tar.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/resource=ingress/default/my-ingress-2,external-dns/version=1\""),
		},
		Delete: []*endpoint.Endpoint{
			// EP4
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP5
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\"").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP6
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1&&owner2", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),
			// this change is transferred to the create
			//newEndpointWithOwnedRecord("txt.2tqs20a7-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "tar.test-zone.example.org",
			//	"\"heritage=external-dns,external-dns/owner=owner1,external-dns/resource=ingress/default/my-ingress-2,external-dns/version=1\""

			// EP7
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/resource=ingress/default/my-ingress-2,external-dns/version=1\"").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP8
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner2", "tar.loadbalancer.com"),
			// this change is transfered to the create and overriden with updateNew
			//newEndpointWithOwnerAndOwnedRecord("txt.2tqs20a7-cname-tar.test-zone.example.org", endpoint.RecordTypeTXT, "", "tar.test-zone.example.org",
			//				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""

			// EP9
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-multiple.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\"").WithSetIdentifier("test-set-2"),
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
	t.Skip("New format does not support templated prefix")
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
	r, _ := NewTXTRegistry(context.Background(), p, "prefix%{record_type}.", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)

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
			newEndpointWithResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("prefixcname.2tqs20a7-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
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
	t.Skip("New format does not support templated suffix")
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	ctxEndpoints := []*endpoint.Endpoint{}
	ctx := context.WithValue(context.Background(), provider.RecordsContextKey, ctxEndpoints)
	p.OnApplyChanges = func(ctx context.Context, got *plan.Changes) {
		assert.Equal(t, ctxEndpoints, ctx.Value(provider.RecordsContextKey))
	}
	r, _ := NewTXTRegistry(context.Background(), p, "", "-%{record_type}suffix", "owner1", time.Hour, "", []string{}, []string{}, false, nil)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete:    []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
		UpdateNew: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			newEndpointWithResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("new-record-1-owner1-cnamesuffix.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
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
	t.Skip("New format does not support suffix")
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
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("cname-foobar-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"\""),

			// EP6
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			endpoint.NewEndpoint("cname-multiple-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"\"").WithSetIdentifier("test-set-1"),

			// EP9
			endpoint.NewEndpoint("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "tar.loadbalancer.com"),
			endpoint.NewEndpoint("cname-tar-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"\""),

			// EP10
			endpoint.NewEndpoint("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			endpoint.NewEndpoint("cname-multiple-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "\"\"").WithSetIdentifier("test-set-2"),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "", "-txt", "owner1", time.Hour, "wildcard", []string{}, []string{}, false, nil)
	// Update registry with content of the zone (create request above)
	// otherwise will be creating exising TXT records
	_, _ = r.Records(ctx)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - a new record
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),

			// EP2 - a new record with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),

			// EP3 - a new record outside zone
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),

			// EP4 - a new wildcard record
			newEndpointWithOwnerResource("*.wildcard.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
		},
		Delete: []*endpoint.Endpoint{
			// EP5 - delete cname
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),

			// EP6 - delete cname with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP7 - updating new record
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),

			// EP8 - updating new record with set id
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP9 - updating old record
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "tar.loadbalancer.com"),

			// EP10 - updating old record with set id
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
		},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwnerResource("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("cname-new-record-1-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),

			// EP2
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "lb3.loadbalancer.com").WithSetIdentifier("test-set-3"),
			newEndpointWithOwnedRecord("cname-multiple-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\"").WithSetIdentifier("test-set-3"),

			// EP3
			newEndpointWithOwnerResource("example", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("cname-example-owner1-txt", endpoint.RecordTypeTXT, "example",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),

			// EP4
			newEndpointWithOwnerResource("*.wildcard.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("cname-wildcard-owner1-txt.wildcard.test-zone.example.org", endpoint.RecordTypeTXT, "*.wildcard.test-zone.example.org",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress\""),
		},
		Delete: []*endpoint.Endpoint{
			// EP5
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("cname-foobar-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org"),

			// EP6
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb1.loadbalancer.com").WithSetIdentifier("test-set-1"),
			newEndpointWithOwnedRecord("cname-multiple-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org").WithSetIdentifier("test-set-1"),
		},
		UpdateNew: []*endpoint.Endpoint{
			// EP7
			newEndpointWithOwnerResource("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress-2", "new-tar.loadbalancer.com"),
			newEndpointWithOwnedRecord("cname-tar-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "tar.test-zone.example.org",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress-2\""),

			// EP8
			newEndpointWithOwnerResource("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "ingress/default/my-ingress-2", "new.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnedRecord("cname-multiple-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org",
				"\"heritage=external-dns,external-dns/resource=ingress/default/my-ingress-2\"").WithSetIdentifier("test-set-2"),
		},
		UpdateOld: []*endpoint.Endpoint{
			// EP9
			newEndpointWithOwner("tar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "tar.loadbalancer.com"),
			newEndpointWithOwnedRecord("cname-tar-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "tar.test-zone.example.org"),

			// EP10
			newEndpointWithOwner("multiple.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "lb2.loadbalancer.com").WithSetIdentifier("test-set-2"),
			newEndpointWithOwnedRecord("cname-multiple-owner1-txt.test-zone.example.org", endpoint.RecordTypeTXT, "multiple.test-zone.example.org").WithSetIdentifier("test-set-2"),
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
	t.Skip("New format does not support no prefix")
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
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "\"\""),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	// Update registry with content of the zone (create request above)
	// otherwise will be creating exising TXT records
	_, _ = r.Records(ctx)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - new cname
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			// EP2 - new cname outside zone
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			// EP3 - new cname with alial
			newEndpointWithOwner("new-alias.test-zone.example.org", endpoint.RecordTypeA, "owner1", "my-domain.com").WithProviderSpecific("alias", "true"),
		},
		Delete: []*endpoint.Endpoint{
			// EP4 - delete cname
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("owner1-cname-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org"),

			// EP2
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("owner1-cname-example", endpoint.RecordTypeTXT, "example"),

			// EP3
			newEndpointWithOwner("new-alias.test-zone.example.org", endpoint.RecordTypeA, "owner1", "my-domain.com").WithProviderSpecific("alias", "true"),
			// TODO: It's not clear why the TXT registry copies ProviderSpecificProperties to ownership records; that doesn't seem correct.
			newEndpointWithOwnedRecord("owner1-cname-new-alias.test-zone.example.org", endpoint.RecordTypeTXT, "new-alias.test-zone.example.org").WithProviderSpecific("alias", "true"),
		},
		Delete: []*endpoint.Endpoint{
			// EP4
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("owner1-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org"),
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
	t.Skip("New format does not support no prefix")
	ctx := context.Background()
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	p.ApplyChanges(ctx, &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1 - old (V1) format cname
			endpoint.NewEndpoint("v1format.test-zone.example.org", endpoint.RecordTypeCNAME, "foo.loadbalancer.com"),
			endpoint.NewEndpoint("v1format.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP2 - old (V2) format A record
			endpoint.NewEndpoint("v2format.test-zone.example.org", endpoint.RecordTypeA, "bar.loadbalancer.com"),
			endpoint.NewEndpoint("a-v2format.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP3 - new (V3) format ns recod
			endpoint.NewEndpoint("newformat.test-zone.example.org", endpoint.RecordTypeNS, "foobar.nameserver.com"),
			endpoint.NewEndpoint("owner1-ns-newformat.test-zone.example.org", endpoint.RecordTypeTXT),

			// EP4 - txt record of new format (V3) that has no endpoint associated - should not be returned
			endpoint.NewEndpoint("newformat.test-zone.example.org", endpoint.RecordTypeTXT),

			// EP5 - txt record with invalid heritage - will be returned
			endpoint.NewEndpoint("noheritage.test-zone.example.org", endpoint.RecordTypeTXT, "random"),

			// EP6 - old (V2) format with a different owner
			endpoint.NewEndpoint("oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeA, "bar.loadbalancer.com"),
			endpoint.NewEndpoint("a-oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2\""),

			// EP7 - unmanaged A record
			endpoint.NewEndpoint("unmanaged1.test-zone.example.org", endpoint.RecordTypeA, "unmanaged1.loadbalancer.com"),

			// EP8 - unmanaged cname
			endpoint.NewEndpoint("unmanaged2.test-zone.example.org", endpoint.RecordTypeCNAME, "unmanaged2.loadbalancer.com"),

			// EP9 - long cname
			endpoint.NewEndpoint("llong-63-characters-label-that-we-expect-to-work.test-zone.example.org", endpoint.RecordTypeCNAME, "foo.loadbalancer.com"),
			endpoint.NewEndpoint("owner1-cname-llong-63-characters-label-that-we-expect-to-work.test-zone.example.org", endpoint.RecordTypeTXT),
		},
	})
	expectedRecords := []*endpoint.Endpoint{
		// EP1
		{
			DNSName:    "v1format.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
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
				endpoint.OwnerLabelKey: "owner1",
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
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP5
		{
			DNSName:    "noheritage.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels:     endpoint.NewLabels(), // No owner because it's not external-dns heritage

		},
		// EP6
		{
			DNSName:    "oldformat-otherowner.test-zone.example.org",
			Targets:    endpoint.Targets{"bar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				// Records() retrieves all the records of the zone, no matter the owner
				endpoint.OwnerLabelKey: "owner2",
			},
		},
		// EP7
		{
			DNSName:    "unmanaged1.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged1.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels:     endpoint.NewLabels(),
		},
		// EP8
		{
			DNSName:    "unmanaged2.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged2.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     endpoint.NewLabels(),
		},
		// EP9
		{
			DNSName:    "llong-63-characters-label-that-we-expect-to-work.test-zone.example.org",
			Targets:    endpoint.Targets{"foo.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
			},
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "wc", []string{endpoint.RecordTypeCNAME, endpoint.RecordTypeA, endpoint.RecordTypeNS}, []string{}, false, nil)
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
			endpoint.NewEndpoint("v1format.test-zone.example.org", endpoint.RecordTypeCNAME, "foo.loadbalancer.com"),
			endpoint.NewEndpoint("txt.v1format.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP2 - old (V2) format a record
			endpoint.NewEndpoint("v2format2.test-zone.example.org", endpoint.RecordTypeA, "bar.loadbalancer.com"),
			endpoint.NewEndpoint("txt.a-v2format2.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP3 - new (V3) format ns record
			endpoint.NewEndpoint("newformat.test-zone.example.org", endpoint.RecordTypeNS, "foobar.nameserver.com"),
			endpoint.NewEndpoint("txt.2tqs20a7-ns-newformat.test-zone.example.org", endpoint.RecordTypeTXT,
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP4 - TXT record with invalid herritage will be returned
			endpoint.NewEndpoint("oldformat3.test-zone.example.org", endpoint.RecordTypeTXT, "random"),

			// EP5 - TXT record of old (V1) format with no endpoint - not returned
			endpoint.NewEndpoint("txt.oldformat3.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP6 - TXT record of old (V2) format with no endpoint - not returned
			endpoint.NewEndpoint("txt.cname-oldformat3.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1\""),

			// EP7 - TXT record of new format (V3) with no endpoint - not returned
			endpoint.NewEndpoint("txt.2tqs20a7-cname-newformat.test-zone.example.org", endpoint.RecordTypeTXT,
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),

			// EP8 - TXT record with invalid heritage - returned
			endpoint.NewEndpoint("noheritage.test-zone.example.org", endpoint.RecordTypeTXT, "random"),

			// EP9 - old format (V1) a record
			endpoint.NewEndpoint("oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeA, "bar.loadbalancer.com"),
			endpoint.NewEndpoint("txt.oldformat-otherowner.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner2\""),

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
				endpoint.OwnerLabelKey: "owner1",
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
				endpoint.OwnerLabelKey: "owner1",
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
				endpoint.OwnerLabelKey: "owner1",
			},
		},
		// EP4
		{
			DNSName:    "oldformat3.test-zone.example.org",
			Targets:    endpoint.Targets{"random"},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnerLabelKey: "owner1",
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
			Labels:     endpoint.NewLabels(), // No owner because it's not external-dns heritage
		},
		// EP9
		{
			DNSName:    "oldformat-otherowner.test-zone.example.org",
			Targets:    endpoint.Targets{"bar.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels: map[string]string{
				// All the records of the zone are retrieved, no matter the owner
				endpoint.OwnerLabelKey: "owner2",
			},
		},
		// EP10
		{
			DNSName:    "unmanaged1.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged1.loadbalancer.com"},
			RecordType: endpoint.RecordTypeA,
			Labels:     endpoint.NewLabels(),
		},
		// EP11
		{
			DNSName:    "unmanaged2.test-zone.example.org",
			Targets:    endpoint.Targets{"unmanaged2.loadbalancer.com"},
			RecordType: endpoint.RecordTypeCNAME,
			Labels:     endpoint.NewLabels(),
		},
	}

	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner1", time.Hour, "wc", []string{endpoint.RecordTypeCNAME, endpoint.RecordTypeA, endpoint.RecordTypeNS, endpoint.RecordTypeTXT}, []string{}, false, nil)
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
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			endpoint.NewEndpoint("2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
		},
	})
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)

	changes := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			// EP2
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
		},
		Delete: []*endpoint.Endpoint{
			// EP3
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),
		},
		UpdateNew: []*endpoint.Endpoint{},
		UpdateOld: []*endpoint.Endpoint{},
	}
	expected := &plan.Changes{
		Create: []*endpoint.Endpoint{
			// EP1
			newEndpointWithOwner("new-record-1.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("2tqs20a7-cname-new-record-1.test-zone.example.org", endpoint.RecordTypeTXT, "new-record-1.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
			// EP2
			newEndpointWithOwner("example", endpoint.RecordTypeCNAME, "owner1", "new-loadbalancer-1.lb.com"),
			newEndpointWithOwnedRecord("2tqs20a7-cname-example", endpoint.RecordTypeTXT, "example",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
		},
		Delete: []*endpoint.Endpoint{
			// EP3
			newEndpointWithOwner("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "foobar.loadbalancer.com"),
			newEndpointWithOwnedRecord("2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org",
				"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
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
	record := newEndpointWithOwner("foo.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "new-foo.loadbalancer.com")
	expectedTXT := []*endpoint.Endpoint{
		{
			DNSName:    "2tqs20a7-cname-foo.test-zone.example.org",
			Targets:    endpoint.Targets{"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnedRecordLabelKey: "foo.test-zone.example.org",
			},
		},
	}
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
	gotTXT := r.generateTXTRecord(record)
	assert.Equal(t, expectedTXT, gotTXT)
}

func TestGenerateTXTWildcard(t *testing.T) {
	record := newEndpointWithOwner("*.test-zone.example.org", endpoint.RecordTypeCNAME, "owner1", "new-foo.loadbalancer.com")
	expectedTXT := []*endpoint.Endpoint{
		{
			DNSName:    "2tqs20a7-cname-wc.test-zone.example.org",
			Targets:    endpoint.Targets{"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnedRecordLabelKey: "*.test-zone.example.org",
			},
		},
	}
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "wc", []string{}, []string{}, false, nil)
	gotTXT := r.generateTXTRecord(record)
	assert.Equal(t, expectedTXT, gotTXT)
}

func TestGenerateTXTForAAAA(t *testing.T) {
	record := newEndpointWithOwner("foo.test-zone.example.org", endpoint.RecordTypeAAAA, "owner1", "2001:DB8::1")
	expectedTXT := []*endpoint.Endpoint{
		{
			DNSName:    "2tqs20a7-aaaa-foo.test-zone.example.org",
			Targets:    endpoint.Targets{"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""},
			RecordType: endpoint.RecordTypeTXT,
			Labels: map[string]string{
				endpoint.OwnedRecordLabelKey: "foo.test-zone.example.org",
			},
		},
	}
	p := inmemory.NewInMemoryProvider()
	p.CreateZone(testZone)
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
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
	r, _ := NewTXTRegistry(context.Background(), p, "", "", "owner1", time.Hour, "", []string{}, []string{}, false, nil)
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
			endpoint.NewEndpoint("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, "foobar.loadbalancer.com"),
			// joined target:
			// key: value
			// txt-encryption-nonce: bqnDtPa1Eo9P4xsu
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org", "\"bqnDtPa1Eo9P4xsu1Qo+YZJ1sD+VoEvSVYD/l8sYGtdl25Rg7bffPWJxIS0DextjU93bTH/3eMFc8Gz4KGLPLUnlSkbo/dEDE6LbCZihI+HopxC0m4XA4p0MrQs6D84symwFmBlN\""),
		},
	})

	r, _ := NewTXTRegistry(context.Background(), p, "txt.", "", "owner1", time.Hour, "", []string{}, []string{}, true, []byte("12345678901234567890123456789012"))
	records, _ := r.Records(ctx)
	changes := &plan.Changes{
		Delete: records,
	}

	// ensure that encryption nonce gets reused when deleting records
	expected := &plan.Changes{
		Delete: []*endpoint.Endpoint{
			newEndpointWithLabels("foobar.test-zone.example.org", endpoint.RecordTypeCNAME, endpoint.Labels{
				endpoint.OwnerLabelKey: "owner1",
				"txt-encryption-nonce": "bqnDtPa1Eo9P4xsu",
			}, "foobar.loadbalancer.com"),
			// should not be split into two targets - second label is a nonce
			newEndpointWithOwnedRecord("txt.2tqs20a7-cname-foobar.test-zone.example.org", endpoint.RecordTypeTXT, "foobar.test-zone.example.org", "\"bqnDtPa1Eo9P4xsu1Qo+YZJ1sD+VoEvSVYD/l8sYGtdl25Rg7bffPWJxIS0DextjU93bTH/3eMFc8Gz4KGLPLUnlSkbo/dEDE6LbCZihI+HopxC0m4XA4p0MrQs6D84symwFmBlN\""),
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
			newEndpointWithOwner("bar.test-zone.example.org", endpoint.RecordTypeTXT, "", "\"heritage=external-dns,external-dns/owner=cat11111,external-dns/resource=ingress/default/foo\""),
			newEndpointWithOwner("bar.test-zone.example.org", endpoint.RecordTypeA, "", "1.2.3.4"),
		},
	})

	r, _ := NewTXTRegistry(context.Background(), p, "_owner.", "", "bar11111", time.Hour, "", []string{}, []string{}, false, nil)
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

func newEndpointWithOwnedRecord(dnsName, recordType, ownedRecord string, targets ...string) *endpoint.Endpoint {
	return newEndpointWithLabels(dnsName, recordType, endpoint.Labels{endpoint.OwnedRecordLabelKey: ownedRecord}, targets...)
}

func newEndpointWithOwnerAndLabels(dnsName, recordType, ownerID string, labels endpoint.Labels, targets ...string) *endpoint.Endpoint {
	if len(targets) == 0 {
		targets = append(targets, "\"\"")
	}
	e := endpoint.NewEndpoint(dnsName, recordType, targets...)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	for k, v := range labels {
		e.Labels[k] = v
	}
	return e
}

func newEndpointWithLabels(dnsName, recordType string, labels endpoint.Labels, targets ...string) *endpoint.Endpoint {
	if len(targets) == 0 {
		targets = append(targets, "\"\"")
	}
	e := endpoint.NewEndpoint(dnsName, recordType, targets...)
	for k, v := range labels {
		e.Labels[k] = v
	}
	return e
}

func newEndpointWithOwnerResource(dnsName, recordType, ownerID, resource string, targets ...string) *endpoint.Endpoint {
	e := newEndpointWithResource(dnsName, recordType, resource, targets...)
	e.Labels[endpoint.OwnerLabelKey] = ownerID
	return e
}

func newEndpointWithResource(dnsName, recordType, resource string, targets ...string) *endpoint.Endpoint {
	if len(targets) == 0 {
		targets = append(targets, "\"\"")
	}
	e := endpoint.NewEndpoint(dnsName, recordType, targets...)
	e.Labels[endpoint.ResourceLabelKey] = resource
	return e
}
