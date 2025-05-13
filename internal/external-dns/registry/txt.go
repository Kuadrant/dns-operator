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
)

const (
	recordTemplate              = "%{record_type}"
	affixSeparator              = "-"
	ownerIDLen                  = 8
	providerSpecificForceUpdate = "txt/force-update"
	nonceLabelKey               = "txt-encryption-nonce"
)

// TXTRegistry implements registry interface with ownership implemented via associated TXT records
type TXTRegistry struct {
	provider provider.Provider
	ownerID  string // refers to the owner id of the current instance
	mapper   nameMapper

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

	// retains a list of existing txt records
	txtRecordsMap map[endpoint.EndpointKey]struct{}

	// encrypt text records
	txtEncryptEnabled bool
	txtEncryptAESKey  []byte

	logger logr.Logger

	LabelsPacker *TXTLabelsPacker
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

	mapper := newaffixNameMapper(txtPrefix, txtSuffix, txtWildcardReplacement)

	return &TXTRegistry{
		provider:            provider,
		ownerID:             ownerID,
		mapper:              mapper,
		cacheInterval:       cacheInterval,
		wildcardReplacement: txtWildcardReplacement,
		managedRecordTypes:  managedRecordTypes,
		excludeRecordTypes:  excludeRecordTypes,
		txtRecordsMap:       make(map[endpoint.EndpointKey]struct{}),
		txtEncryptEnabled:   txtEncryptEnabled,
		txtEncryptAESKey:    txtEncryptAESKey,
		logger:              logger,
		LabelsPacker:        NewTXTLabelsPacker(),
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
// If TXT records were are present, their metadata will be transferred into labels on the endpoint
func (im *TXTRegistry) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	// If we have the zones cached AND we have refreshed the cache since the
	// last given interval, then just use the cached results.
	if im.recordsCache != nil && time.Since(im.recordsCacheRefreshTime) < im.cacheInterval {
		im.logger.V(1).Info("Using cached records")
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

	for _, record := range records {

		if record.RecordType != endpoint.RecordTypeTXT {
			endpoints = append(endpoints, record)
			continue
		}
		labels := make(map[string]string)
		// convert targets into labels
		for _, target := range record.Targets {
			// if empty target there is nothing to convert
			if target == "\"\"" {
				continue
			}
			var labelsFromTarget endpoint.Labels
			labelsFromTarget, err = endpoint.NewLabelsFromString(target, im.txtEncryptAESKey)
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

		// convert TXT record name into the name of endpoint, recordType of said endpoint and owner of the TXT record.
		endpointName, recordType, ownerID := im.mapper.toEndpointName(record.DNSName)
		// compose endpoint key; this is an actual endpoint in the provider.

		key := endpoint.EndpointKey{
			DNSName:       endpointName,
			RecordType:    recordType,
			SetIdentifier: record.SetIdentifier,
		}

		if _, exists := labelMap[key]; !exists {
			labelMap[key] = map[string]endpoint.Labels{}
		}
		// store metadata for endpoint
		labelMap[key][ownerID] = labels
		// txtRecordsMap is just a list of all TXT records
		im.txtRecordsMap[record.Key()] = struct{}{}

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

			// additionally, if we got an old TXT record, we would not know the owner of the labels
			// in this case, copy unknown labels to each owner (and create the owner if there is none)
			unknownOwnerLabels, exists := labelsForKey[""]
			// bother only if an old format had the current owner as one of the owners
			if exists && strings.Contains(unknownOwnerLabels[endpoint.OwnerLabelKey], im.ownerID) {
				delete(labelsForKey, "")
				// also delete owner=owner1 key/value from old format
				delete(unknownOwnerLabels, endpoint.OwnerLabelKey)
				// there is no other owners - create one
				if len(labelsForKey) == 0 {
					labelsForKey[im.ownerID] = unknownOwnerLabels
				} else {
					// there are other owners
					for _, ownerLabels := range labelsForKey {
						for unknownKey, unknownValue := range unknownOwnerLabels {
							// for each value in labels of unknown owner, check if such a key exists
							// in labels of the owner; copy the value only if it doesn't exist
							if _, ownerHasKey := ownerLabels[unknownKey]; !ownerHasKey {
								// do not create
								ownerLabels[unknownKey] = unknownValue
							}
						}
					}
				}

			}

			// pack labels into endpoint
			ep.Labels = im.LabelsPacker.PackLabels(labelsForKey)
		}

		// Handle the migration of TXT records created before the new format (introduced in v0.12.0).
		// The migration is done for the TXT records owned by this instance only.
		if _, ownerExists := ep.Labels[im.ownerID]; ownerExists {
			if plan.IsManagedRecord(ep.RecordType, im.managedRecordTypes, im.excludeRecordTypes) {
				// Get desired TXT records and detect the missing ones
				desiredTXTs := im.generateTXTRecord(ep)
				for _, desiredTXT := range desiredTXTs {
					if _, exists := im.txtRecordsMap[desiredTXT.Key()]; !exists {
						// this will indicate that this endpoint has old TXT associated with it
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

	targets := make([]string, 0)
	// if we have packed labels we need to unpack them and use only a set for the current owner
	ownedLabels := r.Labels
	if packed, _ := im.LabelsPacker.LabelsPacked(r.Labels); packed {
		// error will return false
		ownedLabels = im.LabelsPacker.UnpackLabels(r.Labels)[im.ownerID]
	}

	// the nonce label is a metadata of encryption and should be copied
	nonceLabel, nonceExists := ownedLabels[nonceLabelKey]
	for key, value := range ownedLabels {
		if !nonceExists {
			targets = append(targets, endpoint.Labels{key: value}.Serialize(true, im.txtEncryptEnabled, im.txtEncryptAESKey))
			continue
		}

		// add nonce to each target but the once itself
		if key == nonceLabelKey {
			continue
		}
		targets = append(targets, endpoint.Labels{
			key:           value,
			nonceLabelKey: nonceLabel,
		}.Serialize(true, im.txtEncryptEnabled, im.txtEncryptAESKey))
	}

	if len(targets) == 0 {
		targets = append(targets, "\"\"")
	}
	txtNew := endpoint.NewEndpoint(im.mapper.toTXTName(r.DNSName, im.OwnerID(), recordType), endpoint.RecordTypeTXT, targets...)
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
		Delete: im.FilterEndpointsByOwnerID(im.ownerID, changes.Delete),
	}

	// We do not receive TXT records here
	// Instead we need to generate them from endpoints and metadata on them

	for _, r := range filteredChanges.Create {
		if r.Labels == nil {
			r.Labels = endpoint.NewLabels()
		}

		// for create request just generate TXT record
		filteredChanges.Create = append(filteredChanges.Create, im.generateTXTRecord(r)...)

		if im.cacheInterval > 0 {
			im.addToCache(r)
		}
	}
	for _, r := range filteredChanges.Delete {
		// if we decide to facilitate migrations here, we delete old TXT records for ednpoint with
		// providerSpecific txt/force-update
		if _, err := im.LabelsPacker.LabelsPacked(r.Labels); err != nil {
			return err
		}
		filteredChanges.Delete = append(filteredChanges.Delete, im.generateTXTRecord(r)...)

		if im.cacheInterval > 0 {
			im.removeFromCache(r)
		}
	}

	for _, r := range filteredChanges.UpdateOld {
		if _, err := im.LabelsPacker.LabelsPacked(r.Labels); err != nil {
			return err
		}
		filteredChanges.Create, filteredChanges.UpdateOld = im.ensureTXTRecord(filteredChanges.Create, filteredChanges.UpdateOld, im.generateTXTRecord(r), false)
		// remove old version of record from cache
		if im.cacheInterval > 0 {
			im.removeFromCache(r)
		}
	}

	// make sure TXT records are consistently updated as well
	for _, r := range filteredChanges.UpdateNew {
		if packed, err := im.LabelsPacker.LabelsPacked(r.Labels); err != nil {
			return err
		} else if packed {
			r.Labels = im.LabelsPacker.UnpackLabels(r.Labels)[im.ownerID]
		}

		// if we have nil labels for this owner - we should remove ownership
		// this is done by deleting TXT record
		if r.Labels == nil {
			filteredChanges.Delete = append(filteredChanges.Delete, im.generateTXTRecord(r)...)
		} else {
			filteredChanges.Create, filteredChanges.UpdateNew = im.ensureTXTRecord(filteredChanges.Create, filteredChanges.UpdateNew, im.generateTXTRecord(r), true)
		}

		// add new version of record to cache
		if im.cacheInterval > 0 {
			im.addToCache(r)
		}
	}
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

// GetLabelsPacker implementing Registry interface
func (im *TXTRegistry) GetLabelsPacker() LabelsPacker {
	return im.LabelsPacker
}

// FilterEndpointsByOwnerID implementing Registry interface
func (im *TXTRegistry) FilterEndpointsByOwnerID(ownerID string, list []*endpoint.Endpoint) []*endpoint.Endpoint {
	filtered := []*endpoint.Endpoint{}
	for _, ep := range list {
		if _, ok := ep.Labels[ownerID]; ok {
			filtered = append(filtered, ep)
		}
	}
	return filtered
}

// ensureTXTRecord creates TXT record if id don't exist. If it exists - updates it.
func (im *TXTRegistry) ensureTXTRecord(createArray, updateArray, candidateTXTs []*endpoint.Endpoint, updateRecordsMap bool) (create []*endpoint.Endpoint, update []*endpoint.Endpoint) {
	for _, candidateTXT := range candidateTXTs {
		_, exists := im.txtRecordsMap[candidateTXT.Key()]
		if !exists {
			// make sure we are not creating twice
			// happens when creating record for the hostname that already exists with a new owner
			index := endpointIndex(createArray, candidateTXT)

			if index == -1 {
				createArray = append(createArray, candidateTXT)
				// if we are working with updateOld we should not update this map,
				// it will make controller think that record exists in the provider
				// this enables us to override create request with updateNew TXT record
				if updateRecordsMap {
					im.txtRecordsMap[candidateTXT.Key()] = struct{}{}
				}
			} else {
				createArray[index] = candidateTXT
			}
		} else {
			updateArray = append(updateArray, candidateTXT)
		}
	}
	return createArray, updateArray
}

// endpointIndex returns index of endpoint in a slice. If not found returns -1
func endpointIndex(list []*endpoint.Endpoint, ep *endpoint.Endpoint) int {
	for i, element := range list {
		if element.DNSName == ep.DNSName && element.SetIdentifier == ep.SetIdentifier && element.RecordType == ep.RecordType {
			return i
		}
	}
	return -1
}
