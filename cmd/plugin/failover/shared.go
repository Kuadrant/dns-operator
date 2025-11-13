package failover

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"

	"github.com/go-logr/logr"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/cmd/plugin/common"
)

const (
	// TXTRecord format is kuadrant-active-groups.<domain>
	TXTRecordPrefix        = "kuadrant-active-groups."
	GroupSeparator         = "&&"
	TXTRecordVersion       = "1"
	TXTRecordKeysSeparator = ";"
	TXTRecordGroupKey      = "groups"
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

	slices.Sort(groups)
	groups = slices.Compact(groups)

	if len(groups) == 0 || groups[0] == "" {
		return fmt.Sprintf("\"%s\"", target)
	}

	target += TXTRecordKeysSeparator + TXTRecordGroupKey + "=" + strings.Join(groups, GroupSeparator)

	return fmt.Sprintf("\"%s\"", target)
}

func EnsureGroupIsActive(groupName string, existingRecord *endpoint.Endpoint) *endpoint.Endpoint {
	activeGroups, isCurrentVersion := GetActiveGroupsFromTarget(existingRecord.Targets[0])
	if !isCurrentVersion {
		return existingRecord
	}

	activeGroups = append(activeGroups, groupName)
	slices.Sort(activeGroups)
	activeGroups = slices.Compact(activeGroups)

	existingRecord.Targets[0] = compileTXTRecordTarget(activeGroups)
	return existingRecord
}

func RemoveGroupFromActiveGroups(group string, existingRecord *endpoint.Endpoint) *endpoint.Endpoint {
	activeGroups, isCurrentVersion := GetActiveGroupsFromTarget(existingRecord.Targets[0])
	if !isCurrentVersion {
		return existingRecord
	}

	activeGroups = slices.DeleteFunc(activeGroups, func(s string) bool {
		return s == group
	})

	existingRecord.Targets[0] = compileTXTRecordTarget(activeGroups)
	return existingRecord
}

// inputYes reads input and returns bool - yes/no
func inputYes(log logr.Logger) bool {
	reader := bufio.NewReader(os.Stdin)
	answer, err := reader.ReadString('\n')
	if err != nil {
		log.Error(err, "failed to read answer", "answer", answer)
	}
	answer = strings.TrimSpace(strings.ToLower(answer))

	return answer == "y" || answer == "yes"
}

// GetActiveGroupsFromTarget returns a list of active groups from the endpoint target and a boolean indication that it is a current version
func GetActiveGroupsFromTarget(target string) ([]string, bool) {
	target = strings.Trim(target, "\"")
	activeGroups := make([]string, 0)

	// make sure we are expecting this version
	groups, found := strings.CutPrefix(target, fmt.Sprintf("version=%s", TXTRecordVersion))
	if !found {
		// unknown version - legacy support will be done here
		return activeGroups, false
	}

	// cut off groups key and a separator
	groups, found = strings.CutPrefix(groups, fmt.Sprintf("%s%s=", TXTRecordKeysSeparator, TXTRecordGroupKey))
	if !found {
		return activeGroups, true
	}
	return strings.Split(groups, GroupSeparator), true
}

func compileTXTRecordTarget(activeGroups []string) string {
	var groups string
	if len(activeGroups) != 0 {
		groups = fmt.Sprintf("%s%s=%s", TXTRecordKeysSeparator, TXTRecordGroupKey, strings.Join(activeGroups, GroupSeparator))
	}
	version := fmt.Sprintf("version=%s", TXTRecordVersion)

	return fmt.Sprintf("\"%s%s\"", version, groups)
}
