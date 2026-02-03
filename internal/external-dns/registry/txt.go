/*
Copyright 2017 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package registry

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"

	kuadrantPlan "github.com/kuadrant/dns-operator/internal/external-dns/plan"
)

const (
	recordTemplate              = "%{record_type}"
	affixSeparator              = "-"
	providerSpecificForceUpdate = "txt/force-update"
	txtVersionKey               = "version"

	txtFormatVersion         = "1"
	externalDNSMapperVersion = ""
)

var (
	LabelKeysToMerge = []string{
		endpoint.OwnerLabelKey,
	}

	legacyAffixMappers = map[string]func(string, string, string) NameMapper{
		externalDNSMapperVersion: NewExternalDNSAffixNameMapper,
	}
)

// TXTRegistry implements registry interface with ownership implemented via associated TXT records
type TXTRegistry struct {
	provider provider.Provider
	ownerID  string // refers to the owner id of the current instance
	mapper   NameMapper

	// cache the records in memory and update on an interval instead.
	recordsCache            []*endpoint.Endpoint
	recordsCacheRefreshTime time.Time
	cacheInterval           time.Duration

	// optional string to use to replace the asterisk in wildcard entries - without using this,
	// registry TXT records corresponding to wildcard records will be invalid (and rejected by most providers), due to
	// having a '*' appear (not as the first character) - see https://tools.ietf.org/html/rfc1034#section-4.3.3
	wildcardReplacement string

	managedRecordTypes []string
	excludeRecordTypes []string

	// encrypt text records
	txtEncryptEnabled bool
	txtEncryptAESKey  []byte

	logger logr.Logger
}

// NewTXTRegistry returns new TXTRegistry object
func NewTXTRegistry(ctx context.Context, provider provider.Provider, txtPrefix, txtSuffix, ownerID string, cacheInterval time.Duration, txtWildcardReplacement string, managedRecordTypes, excludeRecordTypes []string, txtEncryptEnabled bool, txtEncryptAESKey []byte) (*TXTRegistry, error) {
	logger := logr.FromContextOrDiscard(ctx).
		WithName("registry")
	logger.V(1).Info("initializing TXT registry", "ownerID", ownerID, "txtPrefix", txtPrefix, "txtSuffix", txtSuffix)

	if ownerID == "" {
		return nil, errors.New("owner id cannot be empty")
	}
	if len(txtEncryptAESKey) == 0 {
		txtEncryptAESKey = nil
	} else if len(txtEncryptAESKey) != 32 {
		return nil, errors.New("the AES Encryption key must have a length of 32 bytes")
	}
	if txtEncryptEnabled && txtEncryptAESKey == nil {
		return nil, errors.New("the AES Encryption key must be set when TXT record encryption is enabled")
	}

	if len(txtPrefix) > 0 && len(txtSuffix) > 0 {
		return nil, errors.New("txt-prefix and txt-suffix are mutual exclusive")
	}

	mapper := newKuadrantAffixMapper(legacyMapperTemplate{
		"": {
			prefix:              txtPrefix,
			suffix:              txtSuffix,
			wildcardReplacement: txtWildcardReplacement,
		},
	}, txtPrefix, txtWildcardReplacement)

	return &TXTRegistry{
		provider:            provider,
		ownerID:             ownerID,
		mapper:              mapper,
		cacheInterval:       cacheInterval,
		wildcardReplacement: txtWildcardReplacement,
		managedRecordTypes:  managedRecordTypes,
		excludeRecordTypes:  excludeRecordTypes,
		txtEncryptEnabled:   txtEncryptEnabled,
		txtEncryptAESKey:    txtEncryptAESKey,
		logger:              logger,
	}, nil
}

func getSupportedTypes() []string {
	return []string{endpoint.RecordTypeA, endpoint.RecordTypeAAAA, endpoint.RecordTypeCNAME, endpoint.RecordTypeNS}
}

func (im *TXTRegistry) GetDomainFilter() endpoint.DomainFilter {
	return im.provider.GetDomainFilter()
}

func (im *TXTRegistry) OwnerID() string {
	return im.ownerID
}

// Records returns the current records from the registry excluding TXT Records
// If TXT records are present, their metadata will be transferred into labels on the endpoint
func (im *TXTRegistry) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	// If we have the zones cached AND we have refreshed the cache since the
	// last given interval, then just use the cached results.
	if im.recordsCache != nil && time.Since(im.recordsCacheRefreshTime) < im.cacheInterval {
		im.logger.Info("Using cached records")
		return im.recordsCache, nil
	}

	records, err := im.provider.Records(ctx)
	if err != nil {
		return nil, err
	}

	endpoints := []*endpoint.Endpoint{}

	// this map will hold labels for all endpoints
	// for each endpoint it will be a map with ownedID as a key and labels as value
	labelMap := map[endpoint.EndpointKey]map[string]endpoint.Labels{}
	txtRecordsMap := make(map[endpoint.EndpointKey]struct{})

	for _, record := range records {

		if record.RecordType != endpoint.RecordTypeTXT {
			endpoints = append(endpoints, record)
			continue
		}

		var ownerID, version string
		labels := make(map[string]string)
		// convert targets into labels
		for _, target := range record.Targets {

			var labelsFromTarget endpoint.Labels
			ownerID, version, labelsFromTarget, err = NewLabelsFromString(target, im.txtEncryptAESKey)
			if errors.Is(err, endpoint.ErrInvalidHeritage) {
				break
			}
			// in case we have multiple targets join them into the same map
			// the latest value takes precedence
			for key, value := range labelsFromTarget {
				labels[key] = value
			}
		}
		// if we failed decoding targets just return this TXT record
		if errors.Is(err, endpoint.ErrInvalidHeritage) {
			// if no heritage is found or it is invalid
			// case when value of txt record cannot be identified
			// record will not be removed as it will have empty owner
			endpoints = append(endpoints, record)
			// reset err to be nil - if the next txt record has no targets, it should not inherit invalid heritage err
			err = nil
			continue
		}
		if err != nil {
			return nil, err
		}

		// convert TXT record name into the name of endpoint and recordType
		endpointName, recordType := im.mapper.ToEndpointName(record.DNSName, version)
		// compose endpoint key; this is an actual endpoint in the provider.

		key := endpoint.EndpointKey{
			DNSName:       endpointName,
			RecordType:    recordType,
			SetIdentifier: record.SetIdentifier,
		}

		if _, exists := labelMap[key]; !exists {
			labelMap[key] = map[string]endpoint.Labels{}
		}

		// store metadata for endpoint if we can get owner from the name
		if ownerID != "" {
			labelMap[key][ownerID] = labels
		} else {
			// check if there is an old format that gives us ownership info
			oldFormatOwners, exist := labels[endpoint.OwnerLabelKey]
			if exist {
				// remove owners themselves from labels
				delete(labels, endpoint.OwnerLabelKey)

				// for each owner from the old format, create an owner entry
				for _, owner := range kuadrantPlan.SplitLabels(oldFormatOwners) {
					// if we have only old labels - there will be nothing for this combination of
					// hostname and owner -
					// if we have old and new format - all information that was in the old format
					// was transferred into a new label in order to create it. So we just ignore old format
					if _, hasLabelsFromNewFormat := labelMap[key][owner]; !hasLabelsFromNewFormat {
						labelMap[key][owner] = labels
					}

				}
			}
		}
		// txtRecordsMap is just a list of all TXT records
		txtRecordsMap[record.Key()] = struct{}{}

	}

	// set metadata on endpoints from TXT records
	for _, ep := range endpoints {
		if ep.Labels == nil {
			ep.Labels = endpoint.NewLabels()
		}
		dnsNameSplit := strings.Split(ep.DNSName, ".")
		// If specified, replace a leading asterisk in the generated txt record name with some other string
		if im.wildcardReplacement != "" && dnsNameSplit[0] == "*" {
			dnsNameSplit[0] = im.wildcardReplacement
		}
		dnsName := strings.Join(dnsNameSplit, ".")
		key := endpoint.EndpointKey{
			DNSName:       dnsName,
			RecordType:    ep.RecordType,
			SetIdentifier: ep.SetIdentifier,
		}

		// AWS Alias records have "new" format encoded as type "cname"
		if isAlias, found := ep.GetProviderSpecificProperty("alias"); found && isAlias == "true" && ep.RecordType == endpoint.RecordTypeA {
			key.RecordType = endpoint.RecordTypeCNAME
		}

		// using the key of this endpoint check if we stored any metadata for it
		labelsForKey, labelsExist := labelMap[key]

		// if it is an old format we might not know record type
		if !labelsExist {
			key.RecordType = ""
			labelsForKey, labelsExist = labelMap[key]
		}

		// we recorded metadata
		if labelsExist {
			// endpoint naturally newer has any labels - we use TXT records to carry them
			// here endpoint is how we got it from the provider
			// depending on the implementation of the provider we might have labels already set
			// in this case any existing labels should be copied to each owner
			for _, ownerLabels := range labelsForKey {
				for endpointLabelKey, endpointLabelValue := range ep.Labels {
					// give precedence to labels from TXT record
					if _, exists := ownerLabels[endpointLabelKey]; !exists {
						ownerLabels[endpointLabelKey] = endpointLabelValue
					}
				}
			}

			// organize keys to merge
			labelsToMerge := make(map[string]string)
			for _, l := range LabelKeysToMerge {
				labelsToMerge[l] = ""
			}

			for owner, labels := range labelsForKey {
				// add owner
				labelsToMerge[endpoint.OwnerLabelKey] = kuadrantPlan.EnsureLabel(labelsToMerge[endpoint.OwnerLabelKey], owner)

				// if we encounter a label key that we want to merge - add this owner ID to the value
				for labelsKey, labelsValue := range labels {
					if _, exists := labelsToMerge[labelsKey]; exists {
						labelsToMerge[labelsKey] = kuadrantPlan.EnsureLabel(labelsToMerge[labelsKey], owner)
					} else {
						// if doesn't exist we are not merging this label but assuming value only of the current owner
						if owner == im.ownerID {
							ep.Labels[labelsKey] = labelsValue
						}
					}
				}
			}

			// transfer merged labels to the enpoint
			for labelsKey, labelsValue := range labelsToMerge {
				// if value is "" - no labels were found in a single owner.
				if labelsValue != "" {
					ep.Labels[labelsKey] = labelsValue
				}
			}
		}

		// Handle the migration of TXT records created before the new format (introduced in v0.12.0).
		// The migration is done for the TXT records owned by this instance only.
		if val, _ := ep.Labels[endpoint.OwnerLabelKey]; strings.Contains(val, im.ownerID) {
			if plan.IsManagedRecord(ep.RecordType, im.managedRecordTypes, im.excludeRecordTypes) {
				// Get desired TXT records and detect the missing ones
				desiredTXTs := im.generateTXTRecord(ep)
				for _, desiredTXT := range desiredTXTs {
					if _, exists := txtRecordsMap[desiredTXT.Key()]; !exists {
						ep.WithProviderSpecific(providerSpecificForceUpdate, "true")
					}
				}
			}
		}
	}

	// Update the cache.
	if im.cacheInterval > 0 {
		im.recordsCache = endpoints
		im.recordsCacheRefreshTime = time.Now()
	}

	return endpoints, nil
}

// generateTXTRecord generates TXT records.
func (im *TXTRegistry) generateTXTRecord(r *endpoint.Endpoint) []*endpoint.Endpoint {
	endpoints := make([]*endpoint.Endpoint, 0)
	recordType := r.RecordType
	// AWS Alias records are encoded as type "cname"
	if isAlias, found := r.GetProviderSpecificProperty("alias"); found && isAlias == "true" && recordType == endpoint.RecordTypeA {
		recordType = endpoint.RecordTypeCNAME
	}

	// version=1
	targets := make(endpoint.Labels)
	targets[txtVersionKey] = txtFormatVersion

	for key, value := range r.Labels {
		if key == endpoint.OwnerLabelKey {
			targets[key] = im.ownerID
			continue
		}
		targets[key] = value
	}

	txtNew := endpoint.NewEndpoint(im.mapper.ToTXTName(r.DNSName, im.OwnerID(), recordType), endpoint.RecordTypeTXT, targets.Serialize(true, im.txtEncryptEnabled, im.txtEncryptAESKey))
	if txtNew != nil {
		txtNew.WithSetIdentifier(r.SetIdentifier)
		txtNew.Labels[endpoint.OwnedRecordLabelKey] = r.DNSName
		txtNew.ProviderSpecific = r.ProviderSpecific
		endpoints = append(endpoints, txtNew)
	}

	return endpoints
}

// ApplyChanges updates dns provider with the changes
// for each created/deleted record it will also take into account TXT records for creation/deletion
func (im *TXTRegistry) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	filteredChanges := &plan.Changes{
		Create: changes.Create,
		//ToDo Ideally we would still be able to ensure ownership on update
		UpdateNew: changes.UpdateNew,
		UpdateOld: changes.UpdateOld,
		// make sure we don't delete endpoints that we don't own.
		// this should not be allowed by the plan, but better safe
		Delete: endpoint.FilterEndpointsByOwnerID(im.ownerID, changes.Delete),
	}

	// We do not receive TXT records here
	// Instead we need to generate them from endpoints and metadata on them

	for _, r := range filteredChanges.Create {
		if r.Labels == nil {
			r.Labels = endpoint.NewLabels()
		}
		r.Labels[endpoint.OwnerLabelKey] = im.ownerID

		// for create request just generate TXT record
		filteredChanges.Create = append(filteredChanges.Create, im.generateTXTRecord(r)...)

		if im.cacheInterval > 0 {
			im.addToCache(r)
		}
	}

	for _, r := range filteredChanges.Delete {
		// if we decide to facilitate migrations here, we delete old TXT records for ednpoint with
		// providerSpecific txt/force-update
		filteredChanges.Delete = append(filteredChanges.Delete, im.generateTXTRecord(r)...)

		if im.cacheInterval > 0 {
			im.removeFromCache(r)
		}
	}

	// for the update we should always have and update as a pair of old and new
	// some providers require old version in order to process an update correctly
	// if there will be no pair for the record - such an update will not be processed
	var updateOld, updateNew []*endpoint.Endpoint
	for _, updateOldRecord := range filteredChanges.UpdateOld {
		for _, updateNewRecord := range filteredChanges.UpdateNew {
			if updateOldRecord.Key() == updateNewRecord.Key() {
				// There are 3 reasons for an update:
				// Adding owner - we need to create a new TXT record
				// Removing owner - we need to delete TXT record
				// Updating other content - we need to update TXT record
				currentRecordIsOwned := strings.Contains(updateOldRecord.Labels[endpoint.OwnerLabelKey], im.ownerID)
				newRecordIsOwned := strings.Contains(updateNewRecord.Labels[endpoint.OwnerLabelKey], im.ownerID)

				// we want to add ownership - create
				if !currentRecordIsOwned && newRecordIsOwned {
					filteredChanges.Create = append(filteredChanges.Create, im.generateTXTRecord(updateNewRecord)...)
					break
				}

				// we want to remove ownership - delete
				if currentRecordIsOwned && !newRecordIsOwned {
					filteredChanges.Delete = append(filteredChanges.Delete, im.generateTXTRecord(updateNewRecord)...)
					break
				}

				// this is an update
				updateOld = append(updateOld, im.generateTXTRecord(updateOldRecord)...)
				updateNew = append(updateNew, im.generateTXTRecord(updateNewRecord)...)

				if im.cacheInterval > 0 {
					im.removeFromCache(updateOldRecord)
					im.addToCache(updateNewRecord)
				}
			}
		}
	}
	filteredChanges.UpdateOld = append(filteredChanges.UpdateOld, updateOld...)
	filteredChanges.UpdateNew = append(filteredChanges.UpdateNew, updateNew...)

	// when caching is enabled, disable the provider from using the cache
	if im.cacheInterval > 0 {
		ctx = context.WithValue(ctx, provider.RecordsContextKey, nil)
	}
	return im.provider.ApplyChanges(ctx, filteredChanges)
}

// AdjustEndpoints modifies the endpoints as needed by the specific provider
func (im *TXTRegistry) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return im.provider.AdjustEndpoints(endpoints)
}

func (im *TXTRegistry) addToCache(ep *endpoint.Endpoint) {
	if im.recordsCache != nil {
		im.recordsCache = append(im.recordsCache, ep)
	}
}

func (im *TXTRegistry) removeFromCache(ep *endpoint.Endpoint) {
	if im.recordsCache == nil || ep == nil {
		return
	}

	for i, e := range im.recordsCache {
		if e.DNSName == ep.DNSName && e.RecordType == ep.RecordType && e.SetIdentifier == ep.SetIdentifier && e.Targets.Same(ep.Targets) {
			// We found a match delete the endpoint from the cache.
			im.recordsCache = append(im.recordsCache[:i], im.recordsCache[i+1:]...)
			return
		}
	}
}

func NewLabelsFromString(labelText string, aesKey []byte) (owner, version string, labels endpoint.Labels, err error) {
	owner, version = "", ""

	labels, err = endpoint.NewLabelsFromString(labelText, aesKey)

	// extract owner and version
	for key, value := range labels {
		if key == endpoint.OwnerLabelKey {
			// if that's an old format we will have multiple owners here - we will deal with that later
			// we aren't sure who owns all the labels
			owner = value
		}
		if key == txtVersionKey {
			version = value
		}
	}

	// remove version
	delete(labels, txtVersionKey)

	return owner, version, labels, err
}
