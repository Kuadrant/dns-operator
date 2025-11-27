package failover

import (
	"fmt"
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

func txtRecordsAreEqual(a, b *endpoint.Endpoint) bool {
	return a.DNSName == b.DNSName &&
		slices.Equal(a.Targets, b.Targets)
}

func getTestTXTWithGroups(groups ...string) *endpoint.Endpoint {
	return GenerateGroupTXTRecord("cat.com", groups...)
}
