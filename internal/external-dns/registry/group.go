package registry

import (
	"context"
	"maps"
	"slices"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/registry"

	"github.com/kuadrant/dns-operator/types"
)

var _ registry.Registry = &GroupRegistry{}

// GroupRegistry wraps a registry implementation to provide group functionality.
type GroupRegistry struct {
	Registry registry.Registry
	Group    types.Group
}

func (g GroupRegistry) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	return g.Registry.Records(ctx)
}

func (g GroupRegistry) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	return g.Registry.ApplyChanges(ctx, changes)
}

func (g GroupRegistry) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	eps, err := g.Registry.AdjustEndpoints(endpoints)
	if err != nil {
		return eps, err
	}

	for _, ep := range eps {
		if g.Group.IsSet() {
			if ep.Labels == nil {
				ep.Labels = endpoint.NewLabels()
			}
			ep.Labels[types.GroupLabelKey] = g.Group.String()
			ep.Labels[types.TargetsLabelKey] = ep.Targets.String()
		} else {
			delete(ep.Labels, types.GroupLabelKey)
			delete(ep.Labels, types.TargetsLabelKey)
		}
	}

	return eps, nil
}

func (g GroupRegistry) GetDomainFilter() endpoint.DomainFilter {
	return g.Registry.GetDomainFilter()
}

func (g GroupRegistry) OwnerID() string {
	return g.Registry.OwnerID()
}

type RegistryOwner struct {
	OwnerID string
	Labels  map[string]string
}

type RegistryGroup struct {
	GroupID types.Group
	Owners  map[string]*RegistryOwner
}

// RegistryHost represents a compilation of all metadata stored in TXT records in a DNS zone
// for a particular rootHost. When the TXT registry stores ownership and metadata information,
// it creates TXT records alongside the actual DNS records (A, CNAME, etc.). This structure
// aggregates all such TXT record metadata for a single host.
//
// The metadata is organized hierarchically:
//   - Host: The root domain name (e.g., "api.example.com")
//   - Groups: TXT records that contain a group label are organized into RegistryGroup objects,
//     where each group contains multiple owners and their associated metadata
//   - UngroupedOwners: TXT records without a group label are stored here, indexed by owner ID
//
// The various IDs here are used as keys and also available as properties, this allows more economical
// searching for particular entries, but allows the structure to continue to contain this useful data
// once extracted from the structure.
//
// This compilation is useful for:
// - Understanding which owners (DNSRecord instances) have claimed a particular host
// - Retrieving all targets and metadata associated with a host across multiple owners
// - Supporting multi-cluster delegation where multiple clusters may publish to the same host
// - Determining group membership for advanced routing strategies (Geo, Weighted)
type RegistryHost struct {
	Host            string
	Groups          map[types.Group]*RegistryGroup
	UngroupedOwners map[string]*RegistryOwner
}

type RegistryMap struct {
	Hosts map[string]*RegistryHost
}

func (m *RegistryMap) GetHosts() []string {
	return slices.Collect(maps.Keys(m.Hosts))
}

func (h *RegistryHost) GetGroupIDs() types.Groups {
	return slices.Collect(maps.Keys(h.Groups))
}

func (h *RegistryHost) HasAnyGroup(groups types.Groups) bool {
	return slices.ContainsFunc(groups, h.HasGroup)
}

func (h *RegistryHost) HasGroup(group types.Group) bool {
	return h.GetGroupIDs().HasGroup(group)
}

func (h *RegistryHost) GetUngroupedTargets() []string {
	targets := map[string]struct{}{}
	for _, o := range h.UngroupedOwners {
		for _, t := range strings.Split(o.Labels["targets"], ",") {
			if t != "" {
				targets[t] = struct{}{}
			}
		}
	}
	return slices.Collect(maps.Keys(targets))
}

func (h *RegistryHost) GetGroupsTargets(groups types.Groups) []string {
	targets := map[string]struct{}{}
	for _, g := range groups {
		if group, ok := h.Groups[g]; ok {
			for _, t := range group.GetTargets() {
				if t != "" {
					targets[t] = struct{}{}
				}
			}
		}
	}
	return slices.Collect(maps.Keys(targets))
}

func (h *RegistryHost) GetOtherGroupsTargets(groups types.Groups) []string {
	targets := map[string]struct{}{}
	for _, g := range h.Groups {
		// we want any groups not provided in the argument
		if !groups.HasGroup(g.GroupID) {
			for _, t := range g.GetTargets() {
				if t != "" {
					targets[t] = struct{}{}
				}
			}
		}
	}
	return slices.Collect(maps.Keys(targets))
}

func (g *RegistryGroup) GetOwnerIDs() []string {
	return slices.Collect(maps.Keys(g.Owners))
}

func (g *RegistryGroup) GetTargets() []string {
	targets := map[string]struct{}{}
	for _, o := range g.Owners {
		for _, t := range strings.Split(o.Labels["targets"], ",") {
			if t != "" {
				targets[t] = struct{}{}
			}
		}
	}
	return slices.Collect(maps.Keys(targets))
}

func TxtRecordsToRegistryMap(endpoints []*endpoint.Endpoint, prefix, suffix, wildcardReplacement string, txtEncryptAESKey []byte) *RegistryMap {
	registryMap := &RegistryMap{
		Hosts: make(map[string]*RegistryHost),
	}

	nameMapper := newKuadrantAffixMapper(legacyMapperTemplate{
		"": {
			prefix:              prefix,
			suffix:              suffix,
			wildcardReplacement: wildcardReplacement,
		},
	}, prefix, wildcardReplacement)

	for _, ep := range endpoints {
		if ep.RecordType != endpoint.RecordTypeTXT {
			continue
		}
		labels := make(map[string]string)
		var ownerID string
		var version string
		var err error
		var hasValidHeritage bool
		for _, target := range ep.Targets {
			var labelsFromTarget endpoint.Labels
			ownerID, version, labelsFromTarget, err = NewLabelsFromString(target, txtEncryptAESKey)
			if err != nil {
				continue
			}
			hasValidHeritage = true
			maps.Copy(labels, labelsFromTarget)
		}

		// Skip if no valid heritage was found in any target
		if !hasValidHeritage {
			continue
		}

		// If ownerID wasn't extracted (no delimiter), get it from labels
		if _, ok := labels[endpoint.OwnerLabelKey]; ok && ownerID == "" {
			ownerID = labels[endpoint.OwnerLabelKey]
		} else if ownerID == "" {
			// couldn't find an owner ID for this record, skip it
			continue
		}

		// Convert TXT record name to actual endpoint name
		endpointName, _ := nameMapper.ToEndpointName(ep.DNSName, version)

		// Use endpoint name as the host key (without record type prefix)
		hostKey := endpointName

		if _, ok := registryMap.Hosts[hostKey]; !ok {
			registryMap.Hosts[hostKey] = &RegistryHost{
				Host:            endpointName,
				Groups:          make(map[types.Group]*RegistryGroup),
				UngroupedOwners: make(map[string]*RegistryOwner),
			}
		}
		if gID, ok := labels[types.GroupLabelKey]; ok {
			groupID := types.Group(gID)
			if _, ok := registryMap.Hosts[hostKey].Groups[groupID]; !ok {
				registryMap.Hosts[hostKey].Groups[groupID] = &RegistryGroup{
					GroupID: groupID,
					Owners:  make(map[string]*RegistryOwner),
				}
			}
			if _, ok := registryMap.Hosts[hostKey].Groups[groupID].Owners[ownerID]; !ok {
				registryMap.Hosts[hostKey].Groups[groupID].Owners[ownerID] = &RegistryOwner{
					OwnerID: ownerID,
				}
			}
			registryMap.Hosts[hostKey].Groups[groupID].Owners[ownerID].Labels = labels
		} else {
			if _, ok := registryMap.Hosts[hostKey].UngroupedOwners[ownerID]; !ok {
				registryMap.Hosts[hostKey].UngroupedOwners[ownerID] = &RegistryOwner{
					OwnerID: ownerID,
				}
			}
			registryMap.Hosts[hostKey].UngroupedOwners[ownerID].Labels = labels
		}
	}
	return registryMap
}
