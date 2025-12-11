package controller

import (
	"context"
	"errors"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	"github.com/go-logr/logr"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
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
	LookupTXT(ctx context.Context, host string, nameservers []string) ([]string, error)
}

// DefaultTXTResolver is the default implementation that uses net.LookupTXT
type DefaultTXTResolver struct{}

func (d *DefaultTXTResolver) LookupTXT(ctx context.Context, host string, nameservers []string) ([]string, error) {
	logger, err := logr.FromContext(ctx)
	if err != nil {
		return []string{}, nil
	}
	logger.Info("looking up txt record", "host", host, "nameservers", nameservers)
	// If nameservers are provided, try each one until we get an answer
	if len(nameservers) > 0 {
		for _, ns := range nameservers {
			// Parse the nameserver to handle cases where port is already specified
			nsAddr := ns
			if _, _, err := net.SplitHostPort(ns); err != nil {
				// No port specified, add default port 53
				nsAddr = net.JoinHostPort(ns, "53")
			}
			logger.V(1).Info("using nameserver", "nameserver", ns, "resolved", nsAddr)

			resolver := &net.Resolver{
				PreferGo: true,
				Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
					dialer := net.Dialer{
						Timeout: time.Second * 3,
					}
					// Always use our specified nameserver, ignoring the address parameter
					return dialer.DialContext(ctx, network, nsAddr)
				},
			}

			records, err := resolver.LookupTXT(ctx, host)

			if err == nil && len(records) > 0 {
				logger.V(1).Info("successfully resolved txt record", "nameserver", nsAddr, "records", records)
				return records, nil
			}
			logger.V(1).Info("failed to resolve txt record", "nameserver", nsAddr, "error", err)
		}
	}

	// Fall back to default net.LookupTXT if no custom nameservers resolved.
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
	//this controller is ungrouped
	return s.GetGroup() == "" ||
		//no active groups specified or
		len(s.activeGroups) == 0 ||
		//this controllers group is active or
		s.activeGroups.HasGroup(s.GetGroup())
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
func (r *BaseDNSRecordReconciler) getActiveGroups(ctx context.Context, c client.Client, dnsRecord DNSRecordAccessor) types.Groups {
	logger := log.FromContext(ctx).WithName("active-groups")
	activeGroups := types.Groups{}
	activeGroupsHost := activeGroupsTXTRecordName + "." + dnsRecord.GetZoneDomainName()

	nameservers := r.getNameserversFromProvider(ctx, c, dnsRecord)
	values, err := r.TXTResolver.LookupTXT(ctx, activeGroupsHost, nameservers)
	if err != nil {
		logger.Error(err, "error looking up active groups")
		return activeGroups
	}
	// found an answer, format it and return
	if len(values) > 0 {
		activeGroupsStr := strings.Join(values, "")
		pairs := strings.SplitSeq(activeGroupsStr, ";")
		for pairStr := range pairs {
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

// getNameserversFromProvider extracts nameservers from the provider secret
func (r *BaseDNSRecordReconciler) getNameserversFromProvider(ctx context.Context, c client.Client, dnsRecord DNSRecordAccessor) []string {
	var nameservers []string
	var providerSecret v1.Secret
	providerRef := dnsRecord.GetProviderRef()

	if providerRef.Name != "" {
		secretKey := client.ObjectKey{
			Name:      providerRef.Name,
			Namespace: dnsRecord.GetNamespace(),
		}

		if err := c.Get(ctx, secretKey, &providerSecret); err == nil {
			nameservers = r.extractNameserversFromSecret(&providerSecret)
		}
	} else {
		secretList := &v1.SecretList{}
		err := c.List(ctx, secretList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{
				v1alpha1.DefaultProviderSecretLabel: "true",
			}),
			Namespace: dnsRecord.GetNamespace(),
		})

		if err == nil && len(secretList.Items) == 1 {
			nameservers = r.extractNameserversFromSecret(&secretList.Items[0])
		}
	}
	return nameservers
}

// extractNameserversFromSecret extracts and parses the NAMESERVERS field from a secret
func (r *BaseDNSRecordReconciler) extractNameserversFromSecret(secret *v1.Secret) []string {
	var nameservers []string
	if nameserversData, ok := secret.Data["NAMESERVERS"]; ok && len(nameserversData) > 0 {
		nameserversStr := string(nameserversData)
		for ns := range strings.SplitSeq(nameserversStr, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				nameservers = append(nameservers, ns)
			}
		}
	}

	return nameservers
}

// unpublishInactiveGroups: handle unpublishing inactive groups records from the zone
func (r *BaseDNSRecordReconciler) unpublishInactiveGroups(ctx context.Context, c client.Client, dnsRecord DNSRecordAccessor, dnsProvider provider.Provider) error {
	logger := log.FromContext(ctx).WithName("active-groups").WithName("unpublish")
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}

	// if this record does not have a group, do not process unpublish
	if dnsRecord.GetGroup() == "" {
		return nil
	}

	activeGroups := r.getActiveGroups(ctx, c, dnsRecord)

	// only process unpublish when there are active groups and we are reconciling a record from an active group
	if len(activeGroups) == 0 || !dnsRecord.IsActive() {
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
	registryMap := externaldnsregistry.TxtRecordsToRegistryMap(allZoneEndpoints, txtRegistryPrefix, txtRegistrySuffix, txtRegistryWildcardReplacement, []byte(txtRegistryEncryptAESKey))
	changes := &plan.Changes{}

	// work out required changes to clean out inactive groups managed DNS Records (not including TXT records)
	for _, e := range allZoneEndpoints {
		//not a managed record type, skip it
		if !slices.Contains(managedDNSRecordTypes, e.RecordType) {
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
				if slices.Contains(activeTargets, t) {
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
			_, _, labelsFromTarget, err = registry.NewLabelsFromString(target, []byte(txtRegistryEncryptAESKey))
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
