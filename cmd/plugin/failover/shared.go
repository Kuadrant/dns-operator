package failover

import (
	"fmt"
	"slices"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/cmd/plugin/common"
)

const (
	// TXTRecord format is kuadrant-active-groups.<domain>
	TXTRecordPrefix        = "kuadrant-active-groups."
	GroupSeparator         = "&&"
	TXTRecordVersion       = "1"
	TXTRecordKeysSeparator = ";"
)

var (
	providerRef string
	resourceRef *common.ResourceRef
	assumeYes   bool
	domain      string
)

func GenerateGroupTXTRecord(domain string, groups ...string) *endpoint.Endpoint {
	return endpoint.NewEndpoint(TXTRecordPrefix+domain, endpoint.RecordTypeTXT, generateGroupTXTRecordTargets(groups...))
}

// we can get away with this because it is an initial generation
func generateGroupTXTRecordTargets(groups ...string) string {
	target := fmt.Sprintf("version=%s", TXTRecordVersion)

	if len(groups) == 0 {
		return target
	}

	target += TXTRecordKeysSeparator + "groups=" + strings.Join(groups, GroupSeparator)

	return fmt.Sprintf("\"%s\"", target)
}

func EnsureGroupTXTRecord(groupName string, existingRecord *endpoint.Endpoint) *endpoint.Endpoint {
	target := strings.Trim(existingRecord.Targets[0], "\"")

	// make sure we are expecting this version
	groups, found := strings.CutPrefix(target, fmt.Sprintf("version=%s%s", TXTRecordVersion, TXTRecordKeysSeparator))
	if !found {
		// unknown version - legacy support will be done here
		return existingRecord
	}

	activeGroups := strings.Split(groups, GroupSeparator)
	activeGroups = append(activeGroups, groupName)

	slices.Sort(activeGroups)
	activeGroups = slices.Compact(activeGroups)

	existingRecord.Targets[0] = fmt.Sprintf("\"version=%s%s%s\"", TXTRecordVersion, TXTRecordKeysSeparator, strings.Join(activeGroups, GroupSeparator))
	return existingRecord
}
