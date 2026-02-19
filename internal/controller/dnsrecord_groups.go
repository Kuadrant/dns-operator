// Package controller implements DNS failover groups functionality for the DNS operator.
//
// # DNS Failover Groups Overview
//
// DNS Groups enable active-passive failover for DNS records across multiple clusters.
// Each cluster can be assigned to a named group (e.g., "primary", "secondary"), and
// only the records from the currently "active" groups are published to DNS.
//
// # How It Works
//
//  1. Group Assignment: Each DNSRecord controller is started with a group identifier
//     (via --group flag or GROUP environment variable). Records managed by that
//     controller inherit the group assignment.
//
//  2. Active Groups Declaration: The set of currently active groups is stored as a
//     special TXT record in DNS at: kuadrant-active-groups.<zone-domain-name>
//     Format: "groups=group1&&group2;version=1"
//
// 3. Group Filtering: Before publishing, each controller:
//
//   - Queries the active groups TXT record
//
//   - Compares its own group against the active groups list
//
//   - Only publishes if its group is active OR if it's ungrouped
//
//     4. Inactive Group Cleanup: When a group becomes active, it unpublishes DNS
//     records from inactive groups to ensure clean failover without stale data.
//
// # Example Scenario
//
// Setup:
//   - Cluster A (group="us-east") has DNSRecord pointing to 1.2.3.4
//   - Cluster B (group="us-west") has DNSRecord pointing to 5.6.7.8
//   - Cluster C (ungrouped) has DNSRecord pointing to 9.9.9.9
//
// When active groups = ["us-east"]:
//   - Published targets: 1.2.3.4, 9.9.9.9
//   - Cluster B sees it's inactive and skips publishing
//
// When active groups switch to ["us-west"]:
//   - Cluster B becomes active and publishes 5.6.7.8
//   - Cluster A sees it's inactive and stops publishing
//   - Cluster B also unpublishes stale 1.2.3.4 records
//   - Published targets: 5.6.7.8, 9.9.9.9
//
// # Ungrouped Records
//
// Records without a group assignment (group="") are always considered active
// and published alongside whichever groups are currently active. This allows
// for baseline traffic routing or shared infrastructure records.
//
// # Key Components
//
// - TXTResolver: Interface for looking up the active groups TXT record from DNS
// - GroupAdapter: Wraps DNSRecordAccessor to add group-aware IsActive() behavior
// - GroupAdapter.UnpublishInactiveGroups(): Cleans up DNS records from inactive groups
// - getActiveGroups(): Queries DNS for the current active groups list
package controller

import (
	"context"
	"maps"
	"net"
	"slices"
	"strings"
	"time"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externaldnsplan "github.com/kuadrant/dns-operator/internal/external-dns/plan"
	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
	externaldnsregistry "github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/types"
)

var (
	// InactiveGroupRequeueTime determines how frequently inactive group records
	// are reconciled to check if they've become active
	InactiveGroupRequeueTime = time.Second * 15
)

const (
	ActiveGroupsTXTRecordName = "kuadrant-active-groups"
	TXTRecordKeysSeparator    = ";"
	TXTRecordGroupKey         = "groups"
)

// TXTResolver is an interface for resolving TXT DNS records.
// This abstraction allows for mocking in tests and custom resolution logic.
type TXTResolver interface {
	// LookupTXT queries the specified host for TXT records.
	// If nameservers are provided, queries are directed to those servers.
	// If nameservers is empty or all fail, falls back to default DNS resolution.
	LookupTXT(ctx context.Context, host string, nameservers []string) ([]string, error)
}

// DefaultTXTResolver is the default implementation that uses net.LookupTXT.
// It supports custom nameserver configuration for querying specific DNS servers.
type DefaultTXTResolver struct{}

// LookupTXT queries TXT records for the given host using custom nameservers if provided.
// This is used to query the active groups TXT record, which may be hosted on specific
// DNS servers (e.g., CoreDNS instances in local development).
//
// Nameserver Resolution Strategy:
//  1. Try each provided nameserver in sequence until one returns results
//  2. Automatically adds port 53 if not specified in the nameserver address
//  3. Falls back to system DNS resolver if all custom nameservers fail
//  4. Uses a 3-second timeout per nameserver to avoid hanging on unreachable servers
func (d *DefaultTXTResolver) LookupTXT(ctx context.Context, host string, nameservers []string) ([]string, error) {
	logger := log.FromContext(ctx)

	logger.Info("looking up txt record", "host", host, "nameservers", nameservers)
	// If nameservers are provided, try each one until we get an answer
	for _, ns := range nameservers {
		// Parse the nameserver to handle cases where port is already specified
		nsAddr := ns
		if _, _, err := net.SplitHostPort(ns); err != nil {
			// No port specified, add default port 53
			nsAddr = net.JoinHostPort(ns, "53")
		}
		logger.Info("using nameserver", "nameserver", ns, "resolved", nsAddr)

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
			logger.Info("successfully resolved txt record", "nameserver", nsAddr, "records", records)
			return records, nil
		}
		logger.Info("failed to resolve txt record", "nameserver", nsAddr, "error", err)
	}

	// Fall back to default net.LookupTXT if no custom nameservers resolved.
	return net.LookupTXT(host)
}

// GroupAdapter wraps a DNSRecordAccessor to provide group-aware behavior.
// It determines whether a record should be published based on whether its
// group is in the list of currently active groups.
type GroupAdapter struct {
	DNSRecordAccessor
	activeGroups types.Groups
}

// newGroupAdapter creates a new GroupAdapter wrapping the given DNSRecordAccessor.
// The activeGroups parameter should be the current list of active groups obtained
// from the active groups TXT record in DNS.
func newGroupAdapter(accessor DNSRecordAccessor, activeGroups types.Groups) *GroupAdapter {
	ga := &GroupAdapter{
		DNSRecordAccessor: accessor,
		activeGroups:      activeGroups,
	}

	return ga
}

// IsActive determines if this record should be published based on group membership.
// Returns true if:
//   - The record is ungrouped (group=""), OR
//   - The record's group is in the active groups list
//
// Returns false if:
//   - The record has a group assignment that is NOT in the active groups list
func (s *GroupAdapter) IsActive() bool {
	//this controller is ungrouped - always active
	if s.GetGroup() == "" {
		return true
	}
	//this controllers group is active
	return s.activeGroups.HasGroup(s.GetGroup())
}

func (s *GroupAdapter) GetActiveGroups() types.Groups {
	return s.activeGroups
}

// SetStatusConditions updates the DNSRecord status conditions including the Active condition.
// The Active condition indicates whether this record's group is currently active:
//   - ConditionTrue: Record is in an active group and will be published
//   - ConditionFalse: Record is in an inactive group and will not be published
//   - Condition removed: Record is ungrouped (always active, no condition needed)
func (s *GroupAdapter) SetStatusConditions(hadChanges bool) {
	s.DNSRecordAccessor.SetStatusConditions(hadChanges)

	if s.GetGroup() == "" {
		s.ClearStatusCondition(string(v1alpha1.ConditionTypeActive))
		return
	}

	if s.IsActive() {
		s.SetStatusCondition(string(v1alpha1.ConditionTypeActive), metav1.ConditionTrue, string(v1alpha1.ConditionReasonInActiveGroup), "Group is included in active groups")
	} else {
		s.SetStatusCondition(string(v1alpha1.ConditionTypeActive), metav1.ConditionFalse, string(v1alpha1.ConditionReasonNotInActiveGroup), "Group is not included in active groups")
		s.SetStatusCondition(string(v1alpha1.ConditionTypeReady), metav1.ConditionFalse, string(v1alpha1.ConditionReasonInInactiveGroup), "No further actions to take while in inactive group")
	}
}

// FinalizeReconciliation is called after apply changes or when a record is found to be inactive.
// For records without a group, this is a no-op.
// For inactive records, it ensures TXT registry entries have accurate group values (gh-637).
// For active records, it unpublishes DNS records from inactive groups.
func (s *GroupAdapter) FinalizeReconciliation(ctx context.Context, dnsProvider provider.Provider) error {
	// If this record does not have a group, do not process
	if s.GetGroup() == "" {
		return nil
	}

	logger := log.FromContext(ctx).WithName("active-groups").WithName("finalize-reconciliation")

	if !s.IsActive() {
		// For inactive records, ensure TXT registry entries have accurate group values (gh-637)
		// We need to update existing TXT records without creating new DNS records
		logger.Info("Record is inactive, updating TXT registry entries with group values")
		return s.updateInactiveGroupTXTRecords(ctx, dnsProvider)
	}

	// For active records, unpublish DNS records from inactive groups
	logger.Info("Record is active, unpublishing inactive groups")
	return s.UnpublishInactiveGroups(ctx, dnsProvider)
}

// updateInactiveGroupTXTRecords updates TXT registry entries for this inactive record
// to ensure they have accurate group values. This allows active groups to correctly
// identify and clean up records from inactive groups (gh-637).
func (s *GroupAdapter) updateInactiveGroupTXTRecords(ctx context.Context, dnsProvider provider.Provider) error {
	logger := log.FromContext(ctx).WithName("update-txt-records")

	// Get all endpoints in the zone
	allZoneEndpoints, err := dnsProvider.Records(ctx)
	if err != nil {
		return err
	}

	changes := &plan.Changes{}

	// Find TXT records that belong to this owner and update them with the group label
	for _, e := range allZoneEndpoints {
		// Only process TXT records
		if e.RecordType != endpoint.RecordTypeTXT {
			continue
		}

		// Check if this TXT record belongs to this owner
		var isOwner bool
		updatedTargets := []string{}
		for _, target := range e.Targets {
			ownerID, _, labels, err := registry.NewLabelsFromString(target, []byte(txtRegistryEncryptAESKey))
			if err != nil {
				logger.Info("failed to parse labels from TXT target", "target", target, "error", err)
				updatedTargets = append(updatedTargets, target)
				continue
			}

			// Check if this TXT record belongs to this owner
			if ownerID == s.GetOwnerID() {
				isOwner = true
				// Update the group label if not already set correctly
				currentGroup := labels[types.GroupLabelKey]
				expectedGroup := string(s.GetGroup())
				if currentGroup != expectedGroup {
					labels[types.GroupLabelKey] = expectedGroup
					// Rebuild the TXT record with the updated labels by serializing
					serialized := labels.Serialize(true, txtRegistryEncryptEnabled, []byte(txtRegistryEncryptAESKey))
					updatedTargets = append(updatedTargets, serialized)
				} else {
					updatedTargets = append(updatedTargets, target)
				}
			} else {
				updatedTargets = append(updatedTargets, target)
			}
		}

		// If this TXT record belongs to this owner and we updated the targets, add it to changes
		if isOwner && !slices.Equal(e.Targets, updatedTargets) {
			changes.UpdateOld = append(changes.UpdateOld, e.DeepCopy())
			e.Targets = updatedTargets
			changes.UpdateNew = append(changes.UpdateNew, e)
		}
	}

	if changes.HasChanges() {
		logger.Info("Updating TXT records with group labels", "updates", len(changes.UpdateNew))
		return dnsProvider.ApplyChanges(ctx, changes)
	}

	return nil
}

// UnpublishInactiveGroups removes DNS records from inactive groups in the DNS provider zone.
//
// This function is called by active group controllers after they successfully publish
// their own records. It ensures that when a group becomes active, any leftover DNS
// records from previously active groups are cleaned up to prevent stale routing.
//
// How it works:
//
//  1. Retrieves all DNS records currently in the provider zone
//  2. Reads the TXT registry entries to determine which group owns each target
//  3. For each DNS record:
//     - If ALL owners are from inactive groups: DELETE the entire record
//     - If SOME owners are from inactive groups: UPDATE to remove only inactive targets
//     - If the record has active group or ungrouped owners: KEEP unchanged
//  4. After DNS record cleanup, deletes the TXT registry records for inactive groups
//
// Example scenario:
//
//	Zone has: foo.example.com -> [1.1.1.1 (group1), 2.2.2.2 (group2), 3.3.3.3 (ungrouped)]
//	Active groups: [group2]
//	Result: foo.example.com -> [2.2.2.2 (group2), 3.3.3.3 (ungrouped)]
//	The 1.1.1.1 target and its TXT registry entry are removed.
//
// Important notes:
//   - Only runs when the current controller's group is active
//   - Does not run for ungrouped or authoritative records
//   - Applies changes directly to the provider (bypassing the registry to avoid conflicts)
//   - TXT record cleanup happens AFTER DNS record cleanup (required for Azure)
func (s *GroupAdapter) UnpublishInactiveGroups(ctx context.Context, dnsProvider provider.Provider) error {
	logger := log.FromContext(ctx).WithName("active-groups").WithName("unpublish")
	managedDNSRecordTypes := []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}

	// if this record does not have a group, do not process unpublish
	if s.GetGroup() == "" {
		return nil
	}

	activeGroups := s.GetActiveGroups()

	// only process unpublish when we are reconciling a record from an active group
	if !s.IsActive() || s.GetGroup() == "" {
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
	for _, endpoint := range allZoneEndpoints {
		//not a managed record type, skip it
		if !slices.Contains(managedDNSRecordTypes, endpoint.RecordType) {
			continue
		}

		//no registry entries for this host at all, skip it
		if _, ok := registryMap.Hosts[endpoint.DNSName]; !ok {
			continue
		}

		registryHost := registryMap.Hosts[endpoint.DNSName]
		if !registryHost.HasAnyGroup(activeGroups) && len(registryHost.UngroupedOwners) == 0 {
			// This host doesn't have owners in active groups nor any ungrouped owners, delete it
			changes.Delete = append(changes.Delete, endpoint)
			continue
		}

		// inactive targets is the targets of all inactive groups
		inactiveTargets := registryHost.GetOtherGroupsTargets(activeGroups)

		// remove all inactive targets from endpoint
		newTargets := []string{}
		for _, t := range endpoint.Targets {
			if !slices.Contains(inactiveTargets, t) {
				newTargets = append(newTargets, t)
			}
		}

		if len(newTargets) > 0 {
			if !slices.Equal(newTargets, endpoint.Targets) {
				// some targets were only owned from inactive groups, modify the endpoint
				changes.UpdateOld = append(changes.UpdateOld, endpoint.DeepCopy())
				endpoint.Targets = newTargets
				changes.UpdateNew = append(changes.UpdateNew, endpoint)
			}
		} else {
			// no targets left for this host, delete it
			changes.Delete = append(changes.Delete, endpoint)
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
			if err != nil {
				logger.V(1).Info("failed to parse labels from TXT target", "target", target, "error", err)
				continue
			}
			maps.Copy(labels, labelsFromTarget)
		}
		// no group, or active group, do not delete
		if v, ok := labels[types.GroupLabelKey]; !ok || activeGroups.HasGroup(types.Group(v)) {
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

// getActiveGroups queries DNS for the current list of active groups.
//
// It looks up a special TXT record at: kuadrant-active-groups.<zone-domain-name>
// The TXT record contains semicolon-separated key=value pairs, where the "groups"
// key contains double-ampersand separated group names.
//
// Example TXT record format: "groups=us-east&&us-west;version=1"
//
// This function:
//  1. Constructs the active groups hostname from the zone domain
//  2. Retrieves custom nameservers from the provider secret (if configured)
//  3. Queries those nameservers (or defaults) for the TXT record
//  4. Parses the "groups=..." entry and returns the list of active groups
//
// Returns an empty Groups list if:
//   - The TXT record doesn't exist
//   - The TXT record is malformed
//   - DNS lookup fails
//   - No "groups" key is found in the record
func (r *BaseDNSRecordReconciler) getActiveGroups(ctx context.Context, c client.Client, dnsRecord DNSRecordAccessor) types.Groups {
	logger := log.FromContext(ctx).WithName("active-groups")
	activeGroups := types.Groups{}
	activeGroupsHost := ActiveGroupsTXTRecordName + "." + dnsRecord.GetZoneDomainName()

	nameservers, err := r.getNameserversFromProvider(ctx, c, dnsRecord)
	if err != nil {
		logger.Error(err, "error getting custom nameservers from provider")
		return activeGroups
	}
	values, err := r.TXTResolver.LookupTXT(ctx, activeGroupsHost, nameservers)
	if err != nil {
		logger.Error(err, "error looking up active groups")
		return activeGroups
	}
	// Parse the TXT record to extract active groups
	// Expected format: "groups=group1&&group2&&group3;version=1;other=value"
	// We're looking for the "groups" key and splitting on "&&" to get individual group names
	if len(values) > 0 {
		activeGroupsStr := strings.Join(values, "")
		for _, pairStr := range strings.Split(activeGroupsStr, TXTRecordKeysSeparator) {
			sections := strings.Split(pairStr, "=")
			if len(sections) != 2 {
				logger.Info("found badly formed data in the active groups TXT record, skipping", "pairStr", pairStr)
				continue
			}
			if sections[0] == TXTRecordGroupKey {
				for g := range strings.SplitSeq(sections[1], externaldnsplan.LabelDelimiter) {
					group := types.Group(g)
					if len(g) > 0 && !activeGroups.HasGroup(group) {
						activeGroups = append(activeGroups, group)
					}
				}
			}

		}
	}

	logger.Info("got active groups", "groups", activeGroups)
	// no answers, return empty
	return activeGroups
}

// getNameserversFromProvider extracts custom nameserver addresses from the provider secret.
//
// Provider secrets can optionally include a NAMESERVERS field containing a comma-separated
// list of DNS server addresses to use for active groups lookups. This is useful when:
//   - Using CoreDNS or other custom DNS servers in development
//   - DNS providers host the active groups record on specific nameservers
//   - You need to bypass public DNS for the active groups query
//
// The function looks for the NAMESERVERS data key in either:
//  1. The provider secret referenced by dnsRecord.GetProviderRef(), OR
//  2. The default provider secret (labeled kuadrant.io/default-provider=true)
//
// Returns a list of nameserver addresses (e.g., ["10.96.0.10:53", "10.96.0.11"])
// Returns an empty list if no NAMESERVERS field is found or the secret doesn't exist.
func (r *BaseDNSRecordReconciler) getNameserversFromProvider(ctx context.Context, c client.Client, dnsRecord DNSRecordAccessor) ([]string, error) {
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
		} else {
			return nameservers, err
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
	return nameservers, nil
}

// extractNameserversFromSecret extracts and parses the NAMESERVERS field from a secret.
// The NAMESERVERS field should contain a comma-separated list of DNS server addresses.
// Each address is trimmed of whitespace and empty entries are ignored.
//
// Example secret data:
//
//	NAMESERVERS: "10.96.0.10:53, 10.96.0.11:53"
//	NAMESERVERS: "coredns.kube-system.svc.cluster.local"
func (r *BaseDNSRecordReconciler) extractNameserversFromSecret(secret *v1.Secret) []string {
	var nameservers []string
	if secret == nil {
		return nameservers
	}
	if nameserversData, ok := secret.Data["NAMESERVERS"]; ok && len(nameserversData) > 0 {
		nameserversStr := string(nameserversData)
		for _, ns := range strings.Split(nameserversStr, ",") {
			ns = strings.TrimSpace(ns)
			if ns != "" {
				nameservers = append(nameservers, ns)
			}
		}
	}

	return nameservers
}
