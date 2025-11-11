//go:build unit

package failover

import (
	"fmt"
	"reflect"
	"slices"
	"testing"

	"sigs.k8s.io/external-dns/endpoint"
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
			if got := EnsureGroupTXTRecord(tt.groupName, tt.existingRecord); !txtRecordsAreEqual(got, tt.wantRecord) {
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
			name:             "reports old version",
			target:           fmt.Sprintf("\"version=%s%s%s=group1\"", "oldVersion", TXTRecordKeysSeparator, TXTRecordGroupKey),
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

func txtRecordsAreEqual(a, b *endpoint.Endpoint) bool {
	return a.DNSName == b.DNSName &&
		slices.Equal(a.Targets, b.Targets)
}

func getTestTXTWithGroups(groups ...string) *endpoint.Endpoint {
	return GenerateGroupTXTRecord("cat.com", groups...)
}
