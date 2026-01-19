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
	"testing"

	"github.com/stretchr/testify/assert"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/types"
)

func TestTxtRecordsToRegistryMap(t *testing.T) {
	tests := []struct {
		name      string
		endpoints []*endpoint.Endpoint
		validate  func(t *testing.T, result *RegistryMap)
	}{
		{
			name:      "Empty endpoints",
			endpoints: []*endpoint.Endpoint{},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.Empty(t, result.Hosts)
			},
		},
		{
			name: "No TXT records",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.org", endpoint.RecordTypeCNAME, "target.example.org"),
				endpoint.NewEndpoint("bar.example.org", endpoint.RecordTypeA, "1.2.3.4"),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.Empty(t, result.Hosts)
			},
		},
		{
			name: "TXT records without groups",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("txt.2tqs20a7-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
				endpoint.NewEndpoint("txt.b1e3677c-cname-bar.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1\""),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.NotNil(t, result)
				assert.Len(t, result.Hosts, 2)

				host1 := result.Hosts["foo.example.org"]
				assert.Contains(t, host1.UngroupedOwners, "owner1")
				assert.Empty(t, host1.Groups)

				host2 := result.Hosts["bar.example.org"]
				assert.Contains(t, host2.UngroupedOwners, "owner2")
				assert.Empty(t, host2.Groups)
			},
		},
		{
			name: "TXT records with groups",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("txt.2tqs20a7-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1\""),
				endpoint.NewEndpoint("txt.b1e3677c-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1\""),
				endpoint.NewEndpoint("txt.c2f4788d-cname-bar.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner3,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group2\""),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.NotNil(t, result)
				// TXT records for same endpoint with same groupID are consolidated
				assert.Len(t, result.Hosts, 2)

				// Check first host with group1 (should have 2 owners)
				host1 := result.Hosts["foo.example.org"]
				assert.Len(t, host1.Groups, 1)
				assert.Empty(t, host1.UngroupedOwners)

				group1 := host1.Groups["group1"]
				assert.Len(t, group1.Owners, 2)
				assert.Contains(t, group1.Owners, "owner1")
				assert.Contains(t, group1.Owners, "owner2")

				// Check second host with different group
				host2 := result.Hosts["bar.example.org"]
				assert.Len(t, host2.Groups, 1)

				group2 := host2.Groups["group2"]
				assert.Len(t, group2.Owners, 1)
				assert.Contains(t, group2.Owners, "owner3")
			},
		},
		{
			name: "TXT records with mixed grouped and ungrouped",
			endpoints: []*endpoint.Endpoint{
				// Grouped records
				endpoint.NewEndpoint("txt.2tqs20a7-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1\""),
				endpoint.NewEndpoint("txt.b1e3677c-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1\""),
				// Ungrouped record
				endpoint.NewEndpoint("txt.c2f4788d-cname-bar.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner3,external-dns/version=1\""),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.NotNil(t, result)
				assert.Len(t, result.Hosts, 2)

				// Check grouped hosts (consolidated into one)
				host1 := result.Hosts["foo.example.org"]
				assert.Len(t, host1.Groups, 1)
				assert.Empty(t, host1.UngroupedOwners)

				group1 := host1.Groups["group1"]
				assert.Len(t, group1.Owners, 2)
				assert.Contains(t, group1.Owners, "owner1")
				assert.Contains(t, group1.Owners, "owner2")

				// Check ungrouped host
				host2 := result.Hosts["bar.example.org"]
				assert.Empty(t, host2.Groups)
				assert.Contains(t, host2.UngroupedOwners, "owner3")
			},
		},
		{
			name: "TXT records with additional labels",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("txt.2tqs20a7-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1,external-dns/target=us-east-1,external-dns/weight=100\""),
				endpoint.NewEndpoint("txt.b1e3677c-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1,external-dns/target=us-west-2,external-dns/weight=200\""),
				endpoint.NewEndpoint("txt.c2f4788d-cname-bar.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner3,external-dns/version=1,external-dns/target=eu-west-1,external-dns/priority=high\""),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.NotNil(t, result)
				assert.Len(t, result.Hosts, 2)

				// Check grouped record with additional labels (both owners in same group)
				host1 := result.Hosts["foo.example.org"]
				group1 := host1.Groups["group1"]
				assert.Len(t, group1.Owners, 2)

				owner1 := group1.Owners["owner1"]
				assert.Equal(t, "us-east-1", owner1.Labels["target"])
				assert.Equal(t, "100", owner1.Labels["weight"])

				owner2 := group1.Owners["owner2"]
				assert.Equal(t, "us-west-2", owner2.Labels["target"])
				assert.Equal(t, "200", owner2.Labels["weight"])

				// Check ungrouped record with labels
				host2 := result.Hosts["bar.example.org"]
				owner3 := host2.UngroupedOwners["owner3"]
				assert.Equal(t, "eu-west-1", owner3.Labels["target"])
				assert.Equal(t, "high", owner3.Labels["priority"])
			},
		},
		{
			name: "TXT records with invalid heritage",
			endpoints: []*endpoint.Endpoint{
				// Valid record
				endpoint.NewEndpoint("txt.2tqs20a7-cname-foo.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner1,external-dns/version=1\""),
				// Invalid heritage - should be skipped
				endpoint.NewEndpoint("txt.invalid.example.org", endpoint.RecordTypeTXT,
					"\"some-random-text\""),
				// Another valid record
				endpoint.NewEndpoint("txt.b1e3677c-cname-bar.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=owner2,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=group1\""),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				assert.NotNil(t, result)
				// Only 2 hosts should be in the map (invalid heritage is skipped)
				assert.Len(t, result.Hosts, 2)
				assert.Contains(t, result.Hosts, "foo.example.org")
				assert.Contains(t, result.Hosts, "bar.example.org")
			},
		},
		{
			name: "Multiple groups and owners against a shared hostname",
			endpoints: []*endpoint.Endpoint{
				// Group 1 (geo-us) with two owners (cluster1 and cluster2) for shared-host.example.org
				endpoint.NewEndpoint("txt.2tqs20a7-cname-shared-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster1,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=geo-us,external-dns/target=us-east-1,external-dns/geo-code=NA\""),
				endpoint.NewEndpoint("txt.b1e3677c-cname-shared-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster2,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=geo-us,external-dns/target=us-west-2,external-dns/geo-code=NA\""),

				// Group 2 (geo-eu) with two different owners (cluster3 and cluster4) for the same shared-host.example.org
				endpoint.NewEndpoint("txt.c2f4788d-cname-shared-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster3,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=geo-eu,external-dns/target=eu-west-1,external-dns/geo-code=EU\""),
				endpoint.NewEndpoint("txt.d3g5899e-cname-shared-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster4,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=geo-eu,external-dns/target=eu-central-1,external-dns/geo-code=EU\""),

				// Group 3 (geo-asia) with a single owner (cluster5) for the same shared-host.example.org
				endpoint.NewEndpoint("txt.e4h6900f-cname-shared-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster5,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=geo-asia,external-dns/target=ap-southeast-1,external-dns/geo-code=AS\""),

				// An ungrouped owner (cluster6) for the same shared-host.example.org
				endpoint.NewEndpoint("txt.f5i7011g-cname-shared-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster6,external-dns/version=1,external-dns/target=us-south-1\""),

				// Different host to ensure proper separation
				endpoint.NewEndpoint("txt.g6j8122h-cname-other-host.example.org", endpoint.RecordTypeTXT,
					"\"heritage=external-dns,external-dns/owner=cluster7,external-dns/version=1,external-dns/"+types.GroupLabelKey+"=geo-us\""),
			},
			validate: func(t *testing.T, result *RegistryMap) {
				// Verify we have exactly 2 hosts
				assert.Len(t, result.Hosts, 2, "Expected 2 distinct hosts")
				assert.Contains(t, result.Hosts, "shared-host.example.org")
				assert.Contains(t, result.Hosts, "other-host.example.org")

				// Verify shared-host.example.org structure
				sharedHost := result.Hosts["shared-host.example.org"]

				// Verify it has 3 groups and 1 ungrouped owner
				assert.Len(t, sharedHost.Groups, 3, "Expected 3 groups for shared-host")
				assert.Len(t, sharedHost.UngroupedOwners, 1, "Expected 1 ungrouped owner for shared-host")

				// Verify Group 1 (geo-us) has 2 owners: cluster1 and cluster2
				geoUSGroup := sharedHost.Groups["geo-us"]
				assert.Len(t, geoUSGroup.Owners, 2, "geo-us group should have 2 owners")
				assert.Contains(t, geoUSGroup.Owners, "cluster1")
				assert.Contains(t, geoUSGroup.Owners, "cluster2")

				// Verify cluster1 labels in geo-us group
				cluster1 := geoUSGroup.Owners["cluster1"]
				assert.Equal(t, "us-east-1", cluster1.Labels["target"])
				assert.Equal(t, "NA", cluster1.Labels["geo-code"])

				// Verify cluster2 labels in geo-us group
				cluster2 := geoUSGroup.Owners["cluster2"]
				assert.Equal(t, "us-west-2", cluster2.Labels["target"])
				assert.Equal(t, "NA", cluster2.Labels["geo-code"])

				// Verify Group 2 (geo-eu) has 2 owners: cluster3 and cluster4
				geoEUGroup := sharedHost.Groups["geo-eu"]
				assert.Len(t, geoEUGroup.Owners, 2, "geo-eu group should have 2 owners")
				assert.Contains(t, geoEUGroup.Owners, "cluster3")
				assert.Contains(t, geoEUGroup.Owners, "cluster4")

				// Verify cluster3 labels in geo-eu group
				cluster3 := geoEUGroup.Owners["cluster3"]
				assert.Equal(t, "eu-west-1", cluster3.Labels["target"])
				assert.Equal(t, "EU", cluster3.Labels["geo-code"])

				// Verify cluster4 labels in geo-eu group
				cluster4 := geoEUGroup.Owners["cluster4"]
				assert.Equal(t, "eu-central-1", cluster4.Labels["target"])
				assert.Equal(t, "EU", cluster4.Labels["geo-code"])

				// Verify Group 3 (geo-asia) has 1 owner: cluster5
				geoAsiaGroup := sharedHost.Groups["geo-asia"]
				assert.Len(t, geoAsiaGroup.Owners, 1, "geo-asia group should have 1 owner")
				assert.Contains(t, geoAsiaGroup.Owners, "cluster5")

				// Verify cluster5 labels in geo-asia group
				cluster5 := geoAsiaGroup.Owners["cluster5"]
				assert.Equal(t, "ap-southeast-1", cluster5.Labels["target"])
				assert.Equal(t, "AS", cluster5.Labels["geo-code"])

				// Verify ungrouped owner (cluster6)
				assert.Contains(t, sharedHost.UngroupedOwners, "cluster6")
				cluster6 := sharedHost.UngroupedOwners["cluster6"]
				assert.Equal(t, "us-south-1", cluster6.Labels["target"])

				// Verify other-host.example.org structure
				otherHost := result.Hosts["other-host.example.org"]
				assert.Len(t, otherHost.Groups, 1, "Expected 1 group for other-host")
				assert.Len(t, otherHost.UngroupedOwners, 0, "Expected 0 ungrouped owners for other-host")

				// Verify other-host has geo-us group with cluster7
				otherGeoUSGroup := otherHost.Groups["geo-us"]
				assert.Len(t, otherGeoUSGroup.Owners, 1, "other-host geo-us group should have 1 owner")
				assert.Contains(t, otherGeoUSGroup.Owners, "cluster7")

				cluster7 := otherGeoUSGroup.Owners["cluster7"]
				assert.Equal(t, "cluster7", cluster7.OwnerID)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := TxtRecordsToRegistryMap(tt.endpoints, "txt.", "", "", nil)
			tt.validate(t, result)
		})
	}
}
