//go:build unit

package failover

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

func TestGenerateGroupTXTRecord(t *testing.T) {
	type args struct {
		domain string
		groups []string
	}
	tests := []struct {
		name string
		args args
		want *endpoint.Endpoint
	}{
		{
			name: "generates valid record with one group",
			args: args{
				domain: "cat.com",
				groups: []string{"group1"},
			},
			want: &endpoint.Endpoint{
				DNSName: TXTRecordPrefix + "cat.com",
				Targets: endpoint.Targets{fmt.Sprintf("\"version=%s%s%s=group1\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey)},
			},
		},
		{
			name: "generates valid record with multiple groups",
			args: args{
				domain: "cat.com",
				groups: []string{"group1", "group2"},
			},
			want: &endpoint.Endpoint{
				DNSName: TXTRecordPrefix + "cat.com",
				Targets: endpoint.Targets{fmt.Sprintf("\"version=%s%s%s=group1%sgroup2\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey, GroupSeparator)},
			},
		},
		{
			name: "generates valid record with no groups",
			args: args{
				domain: "cat.com",
				groups: []string{},
			},
			want: &endpoint.Endpoint{
				DNSName: TXTRecordPrefix + "cat.com",
				Targets: endpoint.Targets{fmt.Sprintf("\"version=%s\"", TXTRecordVersion)},
			},
		},
		{
			name: "generates valid record with empty groups",
			args: args{
				domain: "cat.com",
				groups: []string{"group2", "group2", "group1", "group2", "group3", "group1", "group1"},
			},
			want: &endpoint.Endpoint{
				DNSName: TXTRecordPrefix + "cat.com",
				Targets: endpoint.Targets{fmt.Sprintf("\"version=%s%s%s=group1%sgroup2%sgroup3\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey, GroupSeparator, GroupSeparator)},
			},
		},
		{
			name: "generates valid record with unsorted and duplicate groups",
			args: args{
				domain: "cat.com",
				groups: []string{""},
			},
			want: &endpoint.Endpoint{
				DNSName: TXTRecordPrefix + "cat.com",
				Targets: endpoint.Targets{fmt.Sprintf("\"version=%s\"", TXTRecordVersion)},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GenerateGroupTXTRecord(tt.args.domain, tt.args.groups...); !txtRecordsAreEqual(got, tt.want) {
				t.Errorf("GenerateGroupTXTRecord() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestEnsureGroupTXTRecord(t *testing.T) {
	tests := []struct {
		name           string
		groupName      string
		existingRecord *endpoint.Endpoint
		wantRecord     *endpoint.Endpoint
	}{
		{
			name:           "adds a group",
			groupName:      "group2",
			existingRecord: getTestTXTWithGroups("group1"),
			wantRecord:     getTestTXTWithGroups("group1", "group2"),
		},
		{
			name:           "adds a group to no groups",
			groupName:      "group1",
			existingRecord: getTestTXTWithGroups(),
			wantRecord:     getTestTXTWithGroups("group1"),
		},
		{
			name:           "adds a group to multiple groups",
			groupName:      "group2",
			existingRecord: getTestTXTWithGroups("group1", "group3"),
			wantRecord:     getTestTXTWithGroups("group1", "group2", "group3"),
		},
		{
			name:           "does not duplicate group",
			groupName:      "group2",
			existingRecord: getTestTXTWithGroups("group1", "group2", "group3"),
			wantRecord:     getTestTXTWithGroups("group1", "group2", "group3"),
		},
		{
			name:           "adds a group with overlapping name",
			groupName:      "cat",
			existingRecord: getTestTXTWithGroups("catastrophe", "caterpillar"),
			wantRecord:     getTestTXTWithGroups("cat", "catastrophe", "caterpillar"),
		},
		{
			name:      "does not modify unknown record",
			groupName: "group",
			existingRecord: &endpoint.Endpoint{
				DNSName: "some.cat.com",
				Targets: endpoint.Targets{"target"},
			},
			wantRecord: &endpoint.Endpoint{
				DNSName: "some.cat.com",
				Targets: endpoint.Targets{"target"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := EnsureGroupIsActive(tt.groupName, tt.existingRecord); !txtRecordsAreEqual(got, tt.wantRecord) {
				t.Errorf("EnsureGroupTXTRecord() = %v, want %v", got, tt.wantRecord)
			}
		})
	}
}

func TestGetActiveGroupsFromTarget(t *testing.T) {

	tests := []struct {
		name             string
		target           string
		want             []string
		isCurrentVersion bool
	}{
		{
			name:             "gets a single group",
			target:           fmt.Sprintf("\"version=%s%s%s=group1\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey),
			want:             []string{"group1"},
			isCurrentVersion: true,
		},
		{
			name:             "gets multiple groups",
			target:           fmt.Sprintf("\"version=%s%s%s=group1%sgroup2%sgroup3\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey, GroupSeparator, GroupSeparator),
			want:             []string{"group1", "group2", "group3"},
			isCurrentVersion: true,
		},
		{
			name:             "gets no groups",
			target:           fmt.Sprintf("\"version=%s\"", TXTRecordVersion),
			want:             []string{},
			isCurrentVersion: true,
		},
		{
			name:             "reports legacy version",
			target:           fmt.Sprintf("\"version=%s%s%s=group1\"", "legacyVersion", TXTRecordKeysSeparator, TXTRecordGroupKey),
			want:             []string{},
			isCurrentVersion: false,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			activeGroups, isCurrentVersion := GetActiveGroupsFromTarget(tt.target)
			if !reflect.DeepEqual(activeGroups, tt.want) {
				t.Errorf("GetActiveGroupsFromTarget() activeGroups = %v, want %v", activeGroups, tt.want)
			}
			if isCurrentVersion != tt.isCurrentVersion {
				t.Errorf("GetActiveGroupsFromTarget() isCurrentVersion = %v, want %v", isCurrentVersion, tt.isCurrentVersion)
			}
		})
	}
}

func TestRemoveGroupFromActiveGroups(t *testing.T) {

	tests := []struct {
		name   string
		group  string
		target string
		want   string
	}{
		{
			name:   "removes a group from multiple groups",
			group:  "group1",
			target: fmt.Sprintf("\"version=%s%s%s=group1%sgroup2\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey, GroupSeparator),
			want:   fmt.Sprintf("\"version=%s%s%s=group2\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey),
		},
		{
			name:   "removes a group from a single group",
			group:  "group1",
			target: fmt.Sprintf("\"version=%s%s%s=group1\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey),
			want:   fmt.Sprintf("\"version=%s\"", TXTRecordVersion),
		},
		{
			// this should never happen but testing for it just in case
			name:   "removes non existent group",
			group:  "group1",
			target: fmt.Sprintf("\"version=%s%s%s=group2\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey),
			want:   fmt.Sprintf("\"version=%s%s%s=group2\"", TXTRecordVersion, TXTRecordKeysSeparator, TXTRecordGroupKey),
		},
		{
			name:   "ignores legacy records",
			group:  "group1",
			target: fmt.Sprintf("\"version=%s%s%s=group1%sgroup2\"", "legacyVersion", TXTRecordKeysSeparator, TXTRecordGroupKey, GroupSeparator),
			want:   fmt.Sprintf("\"version=%s%s%s=group1%sgroup2\"", "legacyVersion", TXTRecordKeysSeparator, TXTRecordGroupKey, GroupSeparator),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := RemoveGroupFromActiveGroups(tt.group, &endpoint.Endpoint{Targets: endpoint.Targets{tt.target}}); got.Targets[0] != tt.want {
				t.Errorf("RemoveGroupFromActiveGroups() = %s, want %s", got.Targets[0], tt.want)
			}
		})
	}
}

func TestAnalyseGroupRemovalImpact(t *testing.T) {
	// Helper to build a TXT registry record for a host with owner, group, and targets.
	// TXT record naming follows the kuadrant- prefix convention:
	// kuadrant-<base36hash(ownerID,8)>-<lowercase(recordType)>-<host>
	txtRecord := func(host, recordType, owner, group, targets string) *endpoint.Endpoint {
		label := fmt.Sprintf("\"heritage=external-dns,external-dns/owner=%s,external-dns/version=1", owner)
		if group != "" {
			label += ",external-dns/group=" + group
		}
		if targets != "" {
			label += ",external-dns/targets=" + targets
		}
		label += "\""

		ownerHash := hash.ToBase36HashLen(owner, 8)
		txtName := fmt.Sprintf("kuadrant-%s-%s-%s", ownerHash, strings.ToLower(recordType), host)

		return endpoint.NewEndpoint(txtName, endpoint.RecordTypeTXT, label)
	}

	tests := []struct {
		name                string
		endpoints           []*endpoint.Endpoint
		groupToRemove       string
		currentActiveGroups []string
		wantRemainingGroups []string
		wantImpacts         []EndpointImpact
	}{
		{
			name:                "no managed endpoints",
			endpoints:           []*endpoint.Endpoint{},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1", "group2"},
			wantRemainingGroups: []string{"group2"},
			wantImpacts:         nil,
		},
		{
			name: "endpoint with only the removed group is deleted",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner1", "group1", "1.2.3.4"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1"},
			wantRemainingGroups: nil,
			wantImpacts: []EndpointImpact{
				{
					DNSName:    "foo.example.com",
					RecordType: endpoint.RecordTypeA,
					Action:     ActionDelete,
					OldTargets: []string{"1.2.3.4"},
				},
			},
		},
		{
			name: "endpoint with remaining active group is not affected",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner1", "group2", "1.2.3.4"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1", "group2"},
			wantRemainingGroups: []string{"group2"},
			wantImpacts:         nil,
		},
		{
			name: "endpoint with mixed groups is modified",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4", "5.6.7.8"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner1", "group1", "1.2.3.4"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner2", "group2", "5.6.7.8"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1", "group2"},
			wantRemainingGroups: []string{"group2"},
			wantImpacts: []EndpointImpact{
				{
					DNSName:    "foo.example.com",
					RecordType: endpoint.RecordTypeA,
					Action:     ActionModify,
					OldTargets: []string{"1.2.3.4", "5.6.7.8"},
					NewTargets: []string{"5.6.7.8"},
				},
			},
		},
		{
			name: "endpoint with ungrouped owner is kept",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4", "9.9.9.9"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner1", "group1", "1.2.3.4"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner2", "", "9.9.9.9"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1"},
			wantRemainingGroups: nil,
			wantImpacts: []EndpointImpact{
				{
					DNSName:    "foo.example.com",
					RecordType: endpoint.RecordTypeA,
					Action:     ActionModify,
					OldTargets: []string{"1.2.3.4", "9.9.9.9"},
					NewTargets: []string{"9.9.9.9"},
				},
			},
		},
		{
			name: "non-managed record types are ignored",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeTXT, "some-value"),
				txtRecord("foo.example.com", endpoint.RecordTypeTXT, "owner1", "group1", "some-value"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1"},
			wantRemainingGroups: nil,
			wantImpacts:         nil,
		},
		{
			name: "endpoint with no registry entry is ignored",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1"},
			wantRemainingGroups: nil,
			wantImpacts:         nil,
		},
		{
			name: "shared target between removed and remaining group is kept",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4", "5.6.7.8"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner1", "group1", "1.2.3.4"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner2", "group2", "1.2.3.4,5.6.7.8"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1", "group2"},
			wantRemainingGroups: []string{"group2"},
			wantImpacts:         nil,
		},
		{
			name: "multiple endpoints affected",
			endpoints: []*endpoint.Endpoint{
				endpoint.NewEndpoint("foo.example.com", endpoint.RecordTypeA, "1.2.3.4"),
				endpoint.NewEndpoint("bar.example.com", endpoint.RecordTypeCNAME, "target.example.com"),
				txtRecord("foo.example.com", endpoint.RecordTypeA, "owner1", "group1", "1.2.3.4"),
				txtRecord("bar.example.com", endpoint.RecordTypeCNAME, "owner2", "group1", "target.example.com"),
			},
			groupToRemove:       "group1",
			currentActiveGroups: []string{"group1"},
			wantRemainingGroups: nil,
			wantImpacts: []EndpointImpact{
				{
					DNSName:    "foo.example.com",
					RecordType: endpoint.RecordTypeA,
					Action:     ActionDelete,
					OldTargets: []string{"1.2.3.4"},
				},
				{
					DNSName:    "bar.example.com",
					RecordType: endpoint.RecordTypeCNAME,
					Action:     ActionDelete,
					OldTargets: []string{"target.example.com"},
				},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := AnalyseGroupRemovalImpact(tt.endpoints, tt.groupToRemove, tt.currentActiveGroups)
			if !slices.Equal(result.RemainingGroups, tt.wantRemainingGroups) {
				t.Errorf("RemainingGroups = %v, want %v", result.RemainingGroups, tt.wantRemainingGroups)
			}
			got := result.Endpoints
			if len(got) == 0 && len(tt.wantImpacts) == 0 {
				return
			}
			if len(got) != len(tt.wantImpacts) {
				t.Fatalf("AnalyseGroupRemovalImpact() returned %d impacts, want %d\ngot: %+v", len(got), len(tt.wantImpacts), got)
			}
			for i, want := range tt.wantImpacts {
				if got[i].DNSName != want.DNSName {
					t.Errorf("impact[%d].DNSName = %s, want %s", i, got[i].DNSName, want.DNSName)
				}
				if got[i].RecordType != want.RecordType {
					t.Errorf("impact[%d].RecordType = %s, want %s", i, got[i].RecordType, want.RecordType)
				}
				if got[i].Action != want.Action {
					t.Errorf("impact[%d].Action = %s, want %s", i, got[i].Action, want.Action)
				}
				if !slices.Equal(got[i].OldTargets, want.OldTargets) {
					t.Errorf("impact[%d].OldTargets = %v, want %v", i, got[i].OldTargets, want.OldTargets)
				}
				if !slices.Equal(got[i].NewTargets, want.NewTargets) {
					t.Errorf("impact[%d].NewTargets = %v, want %v", i, got[i].NewTargets, want.NewTargets)
				}
			}
		})
	}
}

func txtRecordsAreEqual(a, b *endpoint.Endpoint) bool {
	return a.DNSName == b.DNSName &&
		slices.Equal(a.Targets, b.Targets)
}

func getTestTXTWithGroups(groups ...string) *endpoint.Endpoint {
	return GenerateGroupTXTRecord("cat.com", groups...)
}
