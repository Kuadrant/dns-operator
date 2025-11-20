package controller

import (
	"context"
	"errors"
	"maps"
	"net"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/types"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

const (
	InactiveGroupRequeueTime = time.Second * 15
)

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
	return len(s.activeGroups) == 0 || s.activeGroups.HasGroup(s.GetGroup())
}

func (s *GroupAdapter) GetEndpoints() []*endpoint.Endpoint {
	if s.IsActive() {
		return s.DNSRecordAccessor.GetEndpoints()
	}
	return []*endpoint.Endpoint{}
}

func (s *GroupAdapter) SetStatusConditions(hadChanges bool) {
	s.DNSRecordAccessor.SetStatusConditions(hadChanges)

	s.SetStatusActiveGroups(s.activeGroups)

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
func (r *BaseDNSRecordReconciler) getActiveGroups(ctx context.Context, k8sClient client.Client, dnsRecord DNSRecordAccessor) types.Groups {
	logger := log.FromContext(ctx).WithName("active-groups")
	activeGroups := types.Groups{}
	values := []string{}
	nss := []string{}
	activeGroupsHost := activeGroupsTXTRecordName + "." + dnsRecord.GetZoneDomainName()

	// Look for custom NAMESERVERS value in provider secret
	secret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      dnsRecord.GetProviderRef().Name,
			Namespace: dnsRecord.GetNamespace(),
		},
	}
	if err := k8sClient.Get(ctx, client.ObjectKeyFromObject(secret), secret); err == nil {
		if string(secret.Data["NAMESERVERS"]) != "" {
			nss := strings.Split(strings.TrimSpace(string(secret.Data["NAMESERVERS"])), ",")
			logger.V(1).Info("got custom nameservers", "nameservers", nss)
		}
	}
	for _, ns := range nss {
		// bad NS passed in? Skip it
		if parsed, err := url.Parse(ns); err != nil || ns != parsed.Host {
			logger.Info("bad nameserver supplied", "nameserver", ns)
			continue
		} else if parsed.Port() == "" {
			// valid host, but no port specified: add default port
			logger.V(1).Info("nameserver specified with no port, adding default ':53'", "nameserver", ns)
			ns = ns + ":53"
		}

		resolver := &net.Resolver{
			Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
				d := net.Dialer{
					Timeout: time.Millisecond * time.Duration(10000),
				}
				return d.DialContext(ctx, network, ns)
			},
		}
		values, _ = resolver.LookupTXT(ctx, activeGroupsHost)

		// found an answer
		if len(values) != 0 {
			logger.Info("got active groups TXT record", "nameserver", ns, "values", values)
			break
		}

		logger.Info("custom nameserver could not answer active groups request", "nameserver", ns)
	}

	// either no custom nameservers defined or none had an answer, so try the internet
	if len(values) == 0 {
		values, _ = net.LookupTXT(activeGroupsHost)
		logger.Info("internet resolution attempted", "values", values)
	}

	// found an answer, format it and return
	if len(values) > 0 {
		activeGroupsStr := strings.Join(values, "")
		activeGroupsStr = strings.ReplaceAll(activeGroupsStr, `\`, ``)
		activeGroupsStr = strings.ReplaceAll(activeGroupsStr, `"`, ``)
		for _, g := range strings.Split(activeGroupsStr, ";") {
			group := types.Group(g)
			if len(g) > 0 && !activeGroups.HasGroup(group) {
				activeGroups = append(activeGroups, group)
			}
		}

		logger.Info("got active groups", "active groups", activeGroups)
		return activeGroups
	}

	// no answers, return empty
	logger.Info("No DNS resolution for active groups record", "hostname", activeGroupsHost)
	return activeGroups
}

// unpublishInactiveGroups: handle unpublishing inactive groups records from the zone
func (r *BaseDNSRecordReconciler) unpublishInactiveGroups(ctx context.Context, k8sClient client.Client, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider) error {
	logger := log.FromContext(ctx).WithName("active-groups")
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}

	if prematurely, _ := recordReceivedPrematurely(dnsRecord); prematurely {
		logger.V(1).Info("Skipping DNSRecord - is still valid")
		return nil
	}

	//allZoneEndpoints = Records in the current dns provider zone
	allZoneEndpoints, err := dnsProvider.Records(ctx)
	if err != nil {
		return err
	}

	activeGroups := r.getActiveGroups(ctx, k8sClient, dnsRecord)

	// only publish from controllers in active groups or ungrouped to avoid empty zones
	if len(activeGroups) == 0 || !activeGroups.HasGroup(r.Group) {
		return nil
	}

	registryMap := externaldnsregistry.TxtRecordsToRegistryMap(allZoneEndpoints, txtRegistryPrefix, txtRegistrySuffix, txtRegistryWildcardReplacement, []byte(txtRegistryEncryptAESKey))
	changes := &plan.Changes{}

	// work out required changes to clean out inactive groups managed DNS Records (not including TXT records)
	for _, e := range allZoneEndpoints {
		//not a managed record type, skip it
		if !slice.ContainsString(managedDNSRecordTypes, e.RecordType) {
			continue
		}
		//no registry entries for this host at all, skip it
		if registryHost, ok := registryMap.Hosts[e.DNSName]; !ok {
			logger.V(1).Info("no registry entries exist for host, skipping", "host", e.DNSName)
			continue
		} else {
			if !registryHost.HasAnyGroup(activeGroups) && len(registryHost.UngroupedOwners) == 0 {
				logger.V(1).Info("host is only owned by inactive groups, queued for deletion", "host", e.DNSName)
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
					logger.V(1).Info("host is owned by active (or ungrouped) and inactive groups, modifying targets", "host", e.DNSName, "old targets", e.Targets, "new targets", activeTargets)
					// some targets were only owned from inactive groups, modify the endpoint
					changes.UpdateOld = append(changes.UpdateOld, e.DeepCopy())
					e.Targets = newTargets
					changes.UpdateNew = append(changes.UpdateNew, e)
				}
			} else {
				// no targets left for this host, delete it
				logger.V(1).Info("host has no targets left, deleting", "host", e.DNSName, "old targets", e.Targets, "new targets", activeTargets)
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
			_, _, labelsFromTarget, err = registry.NewLabelsFromString(target, []byte(txtRegistryEncryptAESKey))
			if errors.Is(err, endpoint.ErrInvalidHeritage) {
				continue
			}
			maps.Copy(labels, labelsFromTarget)
		}
		// no group, or active group, do not delete
		if v, ok := labels[externaldnsregistry.GroupLabelKey]; !ok || activeGroups.HasGroup(types.Group(v)) {
			logger.V(1).Info("registry is for active group", "host", e.DNSName, "group", labels[externaldnsregistry.GroupLabelKey], "active groups", activeGroups)
			continue
		}
		logger.V(1).Info("deleting registry for inactive group", "host", e.DNSName, "group", labels[externaldnsregistry.GroupLabelKey], "active groups", activeGroups)
		changes.Delete = append(changes.Delete, e)
	}

	if changes.HasChanges() {
		err = dnsProvider.ApplyChanges(ctx, changes)
		return err
	}

	return nil
}
