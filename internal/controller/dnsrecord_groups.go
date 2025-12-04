package controller

import (
	"context"
	"errors"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/types"
)

const (
	InactiveGroupRequeueTime = time.Second * 15
)

// TXTResolver is an interface for resolving TXT DNS records
type TXTResolver interface {
	LookupTXT(host string) ([]string, error)
}

// DefaultTXTResolver is the default implementation that uses net.LookupTXT
type DefaultTXTResolver struct{}

func (d *DefaultTXTResolver) LookupTXT(host string) ([]string, error) {
	return net.LookupTXT(host)
}

type GroupAdapter struct {
	DNSRecordAccessor
	activeGroups types.Groups
}

func newGroupAdapter(accessor DNSRecordAccessor, activeGroups types.Groups) *GroupAdapter {
	ga := &GroupAdapter{
		DNSRecordAccessor: accessor,
		activeGroups:      activeGroups,
	}

	return ga
}

func (s *GroupAdapter) IsActive() bool {
	//no active groups specified or
	return len(s.activeGroups) == 0 ||
		//this controllers group is active or
		s.activeGroups.HasGroup(s.GetGroup()) ||
		//this controller is ungrouped
		s.GetGroup() == ""
}

func (s *GroupAdapter) SetStatusConditions(hadChanges bool) {
	s.DNSRecordAccessor.SetStatusConditions(hadChanges)

	if s.GetGroup() != "" {
		if !s.IsActive() {
			s.SetStatusCondition(string(v1alpha1.ConditionTypeActive), metav1.ConditionFalse, string(v1alpha1.ConditionReasonNotInActiveGroup), "Group is not included in active groups")
		} else if len(s.activeGroups) != 0 {
			s.SetStatusCondition(string(v1alpha1.ConditionTypeActive), metav1.ConditionTrue, string(v1alpha1.ConditionReasonInActiveGroup), "Group is included in active groups")
		} else {
			s.SetStatusCondition(string(v1alpha1.ConditionTypeActive), metav1.ConditionTrue, string(v1alpha1.ConditionReasonNoActiveGroups), "Group is active as no active groups were found")
		}
	} else {
		s.ClearStatusCondition(string(v1alpha1.ConditionTypeActive))
	}
}

// Adding some group related functionality to the base reconciler below
func (r *BaseDNSRecordReconciler) getActiveGroups(ctx context.Context, dnsRecord DNSRecordAccessor) types.Groups {
	logger := log.FromContext(ctx).WithName("active-groups")
	activeGroups := types.Groups{}
	activeGroupsHost := activeGroupsTXTRecordName + "." + dnsRecord.GetZoneDomainName()

	values, err := r.TXTResolver.LookupTXT(activeGroupsHost)
	if err != nil {
		logger.Error(err, "error looking up active groups")
		return activeGroups
	}
	// found an answer, format it and return
	if len(values) > 0 {
		activeGroupsStr := strings.Join(values, "")
		pairs := strings.Split(activeGroupsStr, ";")
		for _, pairStr := range pairs {
			sections := strings.Split(pairStr, "=")
			if sections[0] == "groups" {
				for g := range strings.SplitSeq(sections[1], "&&") {
					group := types.Group(g)
					if len(g) > 0 && !activeGroups.HasGroup(group) {
						activeGroups = append(activeGroups, group)
					}
				}
			}

		}
	}

	logger.V(1).Info("got active groups", "groups", activeGroups)
	// no answers, return empty
	return activeGroups
}

// unpublishInactiveGroups: handle unpublishing inactive groups records from the zone
func (r *BaseDNSRecordReconciler) unpublishInactiveGroups(ctx context.Context, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider) error {
	logger := log.FromContext(ctx).WithName("active-groups").WithName("unpublish")
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}

	activeGroups := r.getActiveGroups(ctx, dnsRecord)

	// only process unpublish when there are active groups and we are reconciling a record from an active group
	if len(activeGroups) == 0 || dnsRecord.GetGroup() == "" || !dnsRecord.IsActive() {
		return nil
	}

	if prematurely, _ := recordReceivedPrematurely(dnsRecord); prematurely {
		logger.V(1).Info("Skipping DNSRecord - is still valid")
		return nil
	}

	//allZoneEndpoints = Records in the current dns provider zone
	allZoneEndpoints, err := dnsProvider.Records(ctx)
	if err != nil {
		return err
	}
	registryMap := externaldnsregistry.TxtRecordsToRegistryMap(allZoneEndpoints, TXTRegistryPrefix, TXTRegistrySuffix, TXTRegistryWildcardReplacement, []byte(TXTRegistryEncryptAESKey))
	changes := &plan.Changes{}

	// work out required changes to clean out inactive groups managed DNS Records (not including TXT records)
	for _, e := range allZoneEndpoints {
		//not a managed record type, skip it
		if !slice.ContainsString(managedDNSRecordTypes, e.RecordType) {
			continue
		}

		//no registry entries for this host at all, skip it
		if registryHost, ok := registryMap.Hosts[e.DNSName]; !ok {
			continue
		} else {
			if !registryHost.HasAnyGroup(activeGroups) && len(registryHost.UngroupedOwners) == 0 {
				// This host doesn't have owners in active groups nor any ungrouped owners, delete it
				changes.Delete = append(changes.Delete, e)
				continue
			}

			// active targets is the combination of all active group owners targets, plus all ungrouped owners targets
			activeTargets := append(registryHost.GetGroupsTargets(activeGroups), registryHost.GetUngroupedTargets()...)

			// remove all inactive targets from endpoint
			newTargets := []string{}
			for _, t := range e.Targets {
				if slice.ContainsString(activeTargets, t) {
					newTargets = append(newTargets, t)
				}
			}

			if len(newTargets) > 0 {
				if !slices.Equal(newTargets, e.Targets) {
					// some targets were only owned from inactive groups, modify the endpoint
					changes.UpdateOld = append(changes.UpdateOld, e.DeepCopy())
					e.Targets = newTargets
					changes.UpdateNew = append(changes.UpdateNew, e)
				}
			} else {
				// no targets left for this host, delete it
				changes.Delete = append(changes.Delete, e)
			}
		}
	}

	if changes.HasChanges() {
		// changes against provider directly, as using the registry here
		// would interfere with this controllers registry entries incorrectly.
		err = dnsProvider.ApplyChanges(ctx, changes)
		if err != nil {
			logger.Error(err, "error unpublishing inactive group records")
			return err
		}
	}

	// Clean out all TXT records that are registry entries for inactive groups,
	// this is done after the previous cleanup to ensure records are deleted before
	// the relevant registry entries (i.e. Azure cannot batch changes)
	changes = &plan.Changes{}
	for _, e := range allZoneEndpoints {
		//not a TXT record type, skip it
		if e.RecordType != endpoint.RecordTypeTXT {
			continue
		}
		labels := make(map[string]string)
		for _, target := range e.Targets {
			var labelsFromTarget endpoint.Labels
			_, _, labelsFromTarget, err = registry.NewLabelsFromString(target, []byte(TXTRegistryEncryptAESKey))
			if errors.Is(err, endpoint.ErrInvalidHeritage) {
				continue
			}
			maps.Copy(labels, labelsFromTarget)
		}
		// no group, or active group, do not delete
		if v, ok := labels[externaldnsregistry.GroupLabelKey]; !ok || activeGroups.HasGroup(types.Group(v)) {
			continue
		}
		changes.Delete = append(changes.Delete, e)
	}

	if changes.HasChanges() {
		err = dnsProvider.ApplyChanges(ctx, changes)
		return err
	}

	return nil
}
