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
	ownerIDLen                  = 8
	providerSpecificForceUpdate = "txt/force-update"
	nonceLabelKey               = "txt-encryption-nonce"
	txtVersionKey               = "version"
	txtSeparator                = "="

	txtFormatVersion         = "1"
	externalDNSMapperVersion = ""
)

var (
	LabelKeysToMerge = []string{
		endpoint.OwnerLabelKey,
		kuadrantPlan.SoftDeleteKey,
		kuadrantPlan.StopSoftDeleteKey,
	}

	legacyAffixMappers = map[string]func(string, string, string) nameMapper{
		externalDNSMapperVersion: newExternalDNSAffixNameMapper,
	}
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
		txtRecordsMap:       make(map[endpoint.EndpointKey]struct{}),
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

		var ownerID, version string
		labels := make(map[string]string)
		// convert targets into labels
		for _, target := range record.Targets {

			var labelsFromTarget endpoint.Labels
			ownerID, version, labelsFromTarget, err = im.NewLabelsFromString(target, im.txtEncryptAESKey)
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
		endpointName, recordType := im.mapper.toEndpointName(record.DNSName, version)
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
					if _, exists := im.txtRecordsMap[desiredTXT.Key()]; !exists {
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

	txtNew := endpoint.NewEndpoint(im.mapper.toTXTName(r.DNSName, im.OwnerID(), recordType), endpoint.RecordTypeTXT, targets.Serialize(true, im.txtEncryptEnabled, im.txtEncryptAESKey))
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

	for _, r := range filteredChanges.UpdateOld {
		filteredChanges.Create, filteredChanges.UpdateOld = im.ensureTXTRecord(filteredChanges.Create, filteredChanges.UpdateOld, im.generateTXTRecord(r), false)
		// remove old version of record from cache
		if im.cacheInterval > 0 {
			im.removeFromCache(r)
		}
	}

	// make sure TXT records are consistently updated as well
	for _, r := range filteredChanges.UpdateNew {

		// if we don't own this endpoint anymore - we should remove ownership
		// this is done by deleting TXT record
		if val, _ := r.Labels[endpoint.OwnerLabelKey]; !strings.Contains(val, im.ownerID) {
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

func (im *TXTRegistry) NewLabelsFromString(labelText string, aesKey []byte) (owner, version string, labels endpoint.Labels, err error) {
	owner, version = "", ""

	labels, err = endpoint.NewLabelsFromString(labelText, aesKey)

	// extract owner and version
	for key, value := range labels {
		if key == endpoint.OwnerLabelKey && strings.Contains(value, kuadrantPlan.LabelDelimiter) {
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

/**
  nameMapper is the interface for mapping between the endpoint for the source
  and the endpoint for the TXT record.
*/

type nameMapper interface {
	toEndpointName(txtDNSName, version string) (endpointName, recordType string)
	toTXTName(string, string, string) string
}

type externalDNSAffixNameMapper struct {
	prefix              string
	suffix              string
	wildcardReplacement string
}

var _ nameMapper = externalDNSAffixNameMapper{}

func newExternalDNSAffixNameMapper(prefix, suffix, wildcardReplacement string) nameMapper {
	return externalDNSAffixNameMapper{prefix: strings.ToLower(prefix), suffix: strings.ToLower(suffix), wildcardReplacement: strings.ToLower(wildcardReplacement)}
}

// extractRecordTypeDefaultPosition extracts record type from the default position
// when not using '%{record_type}' in the prefix/suffix
func (pr externalDNSAffixNameMapper) extractRecordTypeDefaultPosition(name string) (string, string) {
	nameS := strings.Split(name, "-")
	for _, t := range getSupportedTypes() {
		if nameS[0] == strings.ToLower(t) {
			return strings.TrimPrefix(name, nameS[0]+"-"), t
		}
	}
	return name, ""
}

// dropAffixExtractType strips TXT record to find an endpoint name it manages
// it also returns the record type
func (pr externalDNSAffixNameMapper) dropAffixExtractType(name string) (baseName, recordType string) {
	prefix := pr.prefix
	suffix := pr.suffix

	if pr.recordTypeInAffix() {
		for _, t := range getSupportedTypes() {
			tLower := strings.ToLower(t)
			iPrefix := strings.ReplaceAll(prefix, recordTemplate, tLower)
			iSuffix := strings.ReplaceAll(suffix, recordTemplate, tLower)

			if pr.isPrefix() && strings.HasPrefix(name, iPrefix) {
				return strings.TrimPrefix(name, iPrefix), t
			}

			if pr.isSuffix() && strings.HasSuffix(name, iSuffix) {
				return strings.TrimSuffix(name, iSuffix), t
			}
		}

		// handle old TXT records
		prefix = pr.dropAffixTemplate(prefix)
		suffix = pr.dropAffixTemplate(suffix)
	}

	if pr.isPrefix() && strings.HasPrefix(name, prefix) {
		return pr.extractRecordTypeDefaultPosition(strings.TrimPrefix(name, prefix))
	}

	if pr.isSuffix() && strings.HasSuffix(name, suffix) {
		return pr.extractRecordTypeDefaultPosition(strings.TrimSuffix(name, suffix))
	}

	return "", ""
}

func (pr externalDNSAffixNameMapper) dropAffixTemplate(name string) string {
	return strings.ReplaceAll(name, recordTemplate, "")
}

func (pr externalDNSAffixNameMapper) isPrefix() bool {
	return len(pr.suffix) == 0
}

func (pr externalDNSAffixNameMapper) isSuffix() bool {
	return len(pr.prefix) == 0 && len(pr.suffix) > 0
}

func (pr externalDNSAffixNameMapper) toEndpointName(txtDNSName, _ string) (endpointName, recordType string) {
	lowerDNSName := strings.ToLower(txtDNSName)

	// drop prefix
	if pr.isPrefix() {
		endpointName, recordType = pr.dropAffixExtractType(lowerDNSName)
		return endpointName, recordType
	}

	// drop suffix
	if pr.isSuffix() {
		dc := strings.Count(pr.suffix, ".")
		DNSName := strings.SplitN(lowerDNSName, ".", 2+dc)
		domainWithSuffix := strings.Join(DNSName[:1+dc], ".")

		r, rType := pr.dropAffixExtractType(domainWithSuffix)
		return r + "." + DNSName[1+dc], rType
	}
	return "", ""
}

func (pr externalDNSAffixNameMapper) recordTypeInAffix() bool {
	if strings.Contains(pr.prefix, recordTemplate) {
		return true
	}
	if strings.Contains(pr.suffix, recordTemplate) {
		return true
	}
	return false
}

func (pr externalDNSAffixNameMapper) normalizeAffixTemplate(afix, recordType string) string {
	if strings.Contains(afix, recordTemplate) {
		return strings.ReplaceAll(afix, recordTemplate, recordType)
	}
	return afix
}

func (pr externalDNSAffixNameMapper) toTXTName(endpointDNSName, _, recordType string) string {
	DNSName := strings.SplitN(endpointDNSName, ".", 2)
	recordType = strings.ToLower(recordType)
	recordT := recordType + "-"

	prefix := pr.normalizeAffixTemplate(pr.prefix, recordType)
	suffix := pr.normalizeAffixTemplate(pr.suffix, recordType)

	// If specified, replace a leading asterisk in the generated txt record name with some other string
	if pr.wildcardReplacement != "" && DNSName[0] == "*" {
		DNSName[0] = pr.wildcardReplacement
	}

	if !pr.recordTypeInAffix() {
		DNSName[0] = recordT + DNSName[0]
	}

	if len(DNSName) < 2 {
		return prefix + DNSName[0] + suffix
	}

	return prefix + DNSName[0] + suffix + "." + DNSName[1]
}

type kuadrantAffixNameMapper struct {
	legacyMappers       map[string]nameMapper
	prefix              string
	wildcardReplacement string
}

type legacyMapperTemplate map[string]struct {
	prefix              string
	suffix              string
	wildcardReplacement string
}

var _ nameMapper = kuadrantAffixNameMapper{}

func newKuadrantAffixMapper(legacyMappersFor legacyMapperTemplate, prefix, wildcardReplacement string) nameMapper {
	affixMappers := make(map[string]nameMapper)

	for version, params := range legacyMappersFor {
		affixMappers[version] = legacyAffixMappers[version](params.prefix, params.suffix, params.wildcardReplacement)
	}
	return kuadrantAffixNameMapper{
		legacyMappers:       affixMappers,
		prefix:              prefix,
		wildcardReplacement: wildcardReplacement,
	}
}

func (pr kuadrantAffixNameMapper) toEndpointName(txtDNSName, version string) (endpointName, recordType string) {
	// legacy
	if version != txtFormatVersion {
		return pr.legacyMappers[version].toEndpointName(txtDNSName, version)
	}

	// ID-recordType-endpoint
	dnsNameSplit := strings.SplitN(strings.TrimPrefix(strings.ToLower(txtDNSName), strings.ToLower(pr.prefix)), affixSeparator, 3)

	// NPE guard
	if len(dnsNameSplit) != 3 {
		return "", ""
	}

	for _, rType := range getSupportedTypes() {
		if dnsNameSplit[1] == strings.ToLower(rType) {
			recordType = rType
			break
		}
	}

	return dnsNameSplit[2], recordType
}

func (pr kuadrantAffixNameMapper) toTXTName(endpointDNSName, id, recordType string) string {
	prefix := pr.prefix
	if !strings.HasSuffix(prefix, affixSeparator) {
		prefix = prefix + affixSeparator
	}

	dnsName := endpointDNSName

	if strings.HasPrefix(dnsName, "*") {
		dnsName = strings.Replace(dnsName, "*", pr.wildcardReplacement, 1)
	}

	return pr.prefix + id + affixSeparator + strings.ToLower(recordType) + affixSeparator + endpointDNSName
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
