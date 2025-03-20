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
	"fmt"
	"strings"
	"time"

	"github.com/go-logr/logr"

	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
	"sigs.k8s.io/external-dns/provider"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

const (
	recordTemplate              = "%{record_type}"
	affixSeparator              = "-"
	setIDHashLen                = 16
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
// If TXT records was created previously to indicate ownership its corresponding value
// will be added to the endpoints Labels map
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

	labelMap := map[endpoint.EndpointKey]endpoint.Labels{}

	//fmt.Printf("\nrecords from provider\n\n")
	for _, record := range records {
		//fmt.Printf("\ndefault key (txtRecordsMap). Name: %s, type: %s, set id: %s\n", record.Key().DNSName, record.Key().RecordType, record.Key().SetIdentifier)

		if record.RecordType != endpoint.RecordTypeTXT {
			endpoints = append(endpoints, record)
			continue
		}
		labels := make(map[string]string)
		for _, target := range record.Targets {
			var labelsFromTarget endpoint.Labels
			labelsFromTarget, err = endpoint.NewLabelsFromString(target, im.txtEncryptAESKey)
			if errors.Is(err, endpoint.ErrInvalidHeritage) {
				break
			}
			for key, value := range labelsFromTarget {
				labels[key] = value
			}
		}
		if errors.Is(err, endpoint.ErrInvalidHeritage) {
			// if no heritage is found or it is invalid
			// case when value of txt record cannot be identified
			// record will not be removed as it will have empty owner
			//fmt.Printf("invalid heritage\n")
			endpoints = append(endpoints, record)
			continue
		}
		if err != nil {
			//fmt.Printf("returning error: %s\n ", err.Error())
			return nil, err
		}

		endpointName, recordType := im.mapper.toEndpointName(record.DNSName, hash.ToBase36HashLen(labels[endpoint.OwnerLabelKey], setIDHashLen))
		key := endpoint.EndpointKey{
			DNSName:       endpointName,
			RecordType:    recordType,
			SetIdentifier: record.SetIdentifier,
		}
		//fmt.Printf("custom key (labels map). Name: %s, type: %s, set id: %s\nowner: %s\n", key.DNSName, key.RecordType, key.SetIdentifier, labels[endpoint.OwnerLabelKey])
		//fmt.Printf("added to label map and records mapp\n")
		labelMap[key] = labels
		im.txtRecordsMap[record.Key()] = struct{}{}

	}

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

		// Handle both new and old registry format with the preference for the new one
		labels, labelsExist := labelMap[key]
		if !labelsExist {
			key.RecordType = ""
			labels, labelsExist = labelMap[key]
		}
		if labelsExist {
			for k, v := range labels {
				ep.Labels[k] = v
			}
		}

		// Handle the migration of TXT records created before the new format (introduced in v0.12.0).
		// The migration is done for the TXT records owned by this instance only.
		if len(im.txtRecordsMap) > 0 && ep.Labels[endpoint.OwnerLabelKey] == im.ownerID {
			if plan.IsManagedRecord(ep.RecordType, im.managedRecordTypes, im.excludeRecordTypes) {
				// Get desired TXT records and detect the missing ones
				desiredTXTs := im.generateTXTRecord(ep)
				//fmt.Printf("\ndesired txt records\n")
				for _, desiredTXT := range desiredTXTs {
					//fmt.Printf("record name: %s\n", desiredTXT.DNSName)
					//fmt.Printf("record id: %s\n\n", desiredTXT.SetIdentifier)

					//fmt.Printf("looking in txt recorsd map\n")
					if _, exists := im.txtRecordsMap[desiredTXT.Key()]; !exists {
						//fmt.Printf("not found\n")
						ep.WithProviderSpecific(providerSpecificForceUpdate, "true")
					} else {
						//fmt.Printf("found\n")
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

	//fmt.Printf("\n .Records() returns \n===================================\n")
	//for i, ep := range endpoints {
	//fmt.Printf("\n endpiont %d\n", i)
	//fmt.Printf("record name: %s\n", ep.DNSName)
	//fmt.Printf("record type: %s\n", ep.RecordType)
	//fmt.Printf("record id: %s\n", ep.SetIdentifier)
	//for _, providerSpecific := range ep.ProviderSpecific {
	//fmt.Printf("provider specific name: %s\n", providerSpecific.Name)
	//fmt.Printf("provider specific value: %s\n", providerSpecific.Value)
	//}
	//for key, value := range ep.Labels {
	//fmt.Printf("label key: %s\tvalue: %s\n", key, value)
	//}
	//}
	//fmt.Printf("===================================\n")

	return endpoints, nil
}

// generateTXTRecord generates TXT records.
// Once we decide to drop old format we need to drop toTXTName() and rename toNewTXTName
func (im *TXTRegistry) generateTXTRecord(r *endpoint.Endpoint) []*endpoint.Endpoint {
	endpoints := make([]*endpoint.Endpoint, 0)
	recordType := r.RecordType
	// AWS Alias records are encoded as type "cname"
	if isAlias, found := r.GetProviderSpecificProperty("alias"); found && isAlias == "true" && recordType == endpoint.RecordTypeA {
		recordType = endpoint.RecordTypeCNAME
	}

	targets := make([]string, 0)
	// the nonce label is a metadata of encryption and should be copied
	nonceLabel, nonceExists := r.Labels[nonceLabelKey]
	for key, value := range r.Labels {
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

	txtNew := endpoint.NewEndpoint(im.mapper.toNewTXTName(r.DNSName, hash.ToBase36HashLen(im.OwnerID(), setIDHashLen), recordType), endpoint.RecordTypeTXT, targets...)
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
		Delete:    endpoint.FilterEndpointsByOwnerID(im.ownerID, changes.Delete),
	}
	//fmt.Printf("\n=====================================\nApply changes\n")
	for _, r := range filteredChanges.Create {
		if r.Labels == nil {
			r.Labels = make(map[string]string)
		}
		r.Labels[endpoint.OwnerLabelKey] = im.ownerID

		//filteredChanges.Create = append(filteredChanges.Create, im.generateTXTRecord(r)...)

		candidateTXTs := im.generateTXTRecord(r)
		for _, candidateTXT := range candidateTXTs {
			fmt.Printf("create\n")
			//_, exists := im.txtRecordsMap[candidateTXT.Key()]
			//if !exists {
			//	fmt.Printf("generated txt does not exists: creating new\n")
			//	filteredChanges.Create = append(filteredChanges.Create, candidateTXT)
			//	im.txtRecordsMap[candidateTXT.Key()] = struct{}{}
			//}
			filteredChanges.Create = append(filteredChanges.Create, candidateTXT)
			im.txtRecordsMap[candidateTXT.Key()] = struct{}{}
		}

		if im.cacheInterval > 0 {
			im.addToCache(r)
		}
	}
	//fmt.Printf("\nCreate records:\n")
	//for i, change := range filteredChanges.Create {
	//fmt.Printf("\nrecord %d\n", i)
	//fmt.Printf("record name: %s\n", change.DNSName)
	//fmt.Printf("record type: %s\n", change.RecordType)
	//for _, target := range change.Targets {
	//fmt.Printf("target: %s\n", target)
	//}
	//fmt.Printf("record id: %s\n", change.SetIdentifier)
	//for _, providerSpecific := range change.ProviderSpecific {
	//	fmt.Printf("provider specific name: %s\n", providerSpecific.Name)
	//	fmt.Printf("provider specific value: %s\n", providerSpecific.Value)
	//}
	//for key, value := range change.Labels {
	//	fmt.Printf("label key: %s\tvalue: %s\n", key, value)
	//}
	//}

	for _, r := range filteredChanges.Delete {
		// when we delete TXT records for which value has changed (due to new label) this would still work because
		// !!! TXT record value is uniquely generated from the Labels of the endpoint. Hence old TXT record can be uniquely reconstructed
		// !!! After migration to the new TXT registry format we can drop records in old format here!!!
		filteredChanges.Delete = append(filteredChanges.Delete, im.generateTXTRecord(r)...)

		if im.cacheInterval > 0 {
			im.removeFromCache(r)
		}
	}

	//fmt.Printf("\nDelete records:\n")
	//for i, change := range filteredChanges.Delete {
	//	fmt.Printf("\nrecord %d\n", i)
	//	fmt.Printf("record name: %s\n", change.DNSName)
	//	fmt.Printf("record type: %s\n", change.RecordType)
	//	for _, target := range change.Targets {
	//		fmt.Printf("target: %s\n", target)
	//	}
	//	fmt.Printf("record id: %s\n", change.SetIdentifier)
	//	for _, providerSpecific := range change.ProviderSpecific {
	//		fmt.Printf("provider specific name: %s\n", providerSpecific.Name)
	//		fmt.Printf("provider specific value: %s\n", providerSpecific.Value)
	//	}
	//	for key, value := range change.Labels {
	//		fmt.Printf("label key: %s\tvalue: %s\n", key, value)
	//	}
	//}

	// make sure TXT records are consistently updated as well
	for _, r := range filteredChanges.UpdateOld {
		// when we updateOld TXT records for which value has changed (due to new label) this would still work because
		// !!! TXT record value is uniquely generated from the Labels of the endpoint. Hence old TXT record can be uniquely reconstructed
		candidateTXTs := im.generateTXTRecord(r)
		for _, candidateTXT := range candidateTXTs {
			fmt.Printf("update old\n")
			_, exists := im.txtRecordsMap[candidateTXT.Key()]
			if exists {
				fmt.Printf("generated txt does exists: updating old\n")
				filteredChanges.UpdateOld = append(filteredChanges.UpdateOld, candidateTXT)
			}
		}
		// remove old version of record from cache
		if im.cacheInterval > 0 {
			im.removeFromCache(r)
		}
	}

	//fmt.Printf("\nUpdate old records:\n")
	//for i, change := range filteredChanges.UpdateOld {
	//	fmt.Printf("\nrecord %d\n", i)
	//	fmt.Printf("record name: %s\n", change.DNSName)
	//	fmt.Printf("record type: %s\n", change.RecordType)
	//	for _, target := range change.Targets {
	//		fmt.Printf("target: %s\n", target)
	//	}
	//	fmt.Printf("record id: %s\n", change.SetIdentifier)
	//	for _, providerSpecific := range change.ProviderSpecific {
	//		fmt.Printf("provider specific name: %s\n", providerSpecific.Name)
	//		fmt.Printf("provider specific value: %s\n", providerSpecific.Value)
	//	}
	//	for key, value := range change.Labels {
	//		fmt.Printf("label key: %s\tvalue: %s\n", key, value)
	//	}
	//}

	// make sure TXT records are consistently updated as well
	for _, r := range filteredChanges.UpdateNew {

		candidateTXTs := im.generateTXTRecord(r)
		for _, candidateTXT := range candidateTXTs {
			fmt.Printf("update new\n")
			_, exists := im.txtRecordsMap[candidateTXT.Key()]
			if !exists {
				// make sure we are not creating twice
				// happens when creating record for the hostname that already exists with a new owner
				index := endpointIndex(filteredChanges.Create, candidateTXT)

				fmt.Printf("generated txt does exists\n")
				if index == -1 {
					fmt.Printf("not found in create - appending\n")
					filteredChanges.Create = append(filteredChanges.Create, candidateTXT)
					im.txtRecordsMap[candidateTXT.Key()] = struct{}{}
				} else {
					fmt.Printf("found in create - replacing\n")
					filteredChanges.Create[index] = candidateTXT
				}

			} else {
				fmt.Printf("generated txt does exists: updating new\n")
				filteredChanges.UpdateNew = append(filteredChanges.UpdateNew, candidateTXT)
			}
		}
		// add new version of record to cache
		if im.cacheInterval > 0 {
			im.addToCache(r)
		}
	}

	//fmt.Printf("\nUpdate new records:\n")
	//for i, change := range filteredChanges.UpdateNew {
	//	fmt.Printf("\nrecord %d\n", i)
	//	fmt.Printf("record name: %s\n", change.DNSName)
	//	fmt.Printf("record type: %s\n", change.RecordType)
	//	for _, target := range change.Targets {
	//		fmt.Printf("target: %s\n", target)
	//	}
	//	fmt.Printf("record id: %s\n", change.SetIdentifier)
	//	for _, providerSpecific := range change.ProviderSpecific {
	//		fmt.Printf("provider specific name: %s\n", providerSpecific.Name)
	//		fmt.Printf("provider specific value: %s\n", providerSpecific.Value)
	//	}
	//	for key, value := range change.Labels {
	//		fmt.Printf("label key: %s\tvalue: %s\n", key, value)
	//	}
	//}

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

// endpointIndex returns index of endpoint in a slice. If not found returns -1
func endpointIndex(list []*endpoint.Endpoint, ep *endpoint.Endpoint) int {
	for i, element := range list {
		if element.DNSName == ep.DNSName {
			return i
		}
	}
	return -1
}

/**
  nameMapper is the interface for mapping between the endpoint for the source
  and the endpoint for the TXT record.
*/

type nameMapper interface {
	toEndpointName(string, string) (endpointName string, recordType string)
	toTXTName(string) string
	toNewTXTName(string, string, string) string
	recordTypeInAffix() bool
}

type affixNameMapper struct {
	prefix              string
	suffix              string
	wildcardReplacement string
}

var _ nameMapper = affixNameMapper{}

func newaffixNameMapper(prefix, suffix, wildcardReplacement string) affixNameMapper {
	return affixNameMapper{prefix: strings.ToLower(prefix), suffix: strings.ToLower(suffix), wildcardReplacement: strings.ToLower(wildcardReplacement)}
}

// extractRecordTypeDefaultPosition extracts record type from the default position
// when not using '%{record_type}' in the prefix/suffix
func extractRecordTypeDefaultPosition(name string) (baseName, recordType string) {
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
func (pr affixNameMapper) dropAffixExtractType(name, id string) (baseName, recordType string) {
	prefix := pr.prefix + id + affixSeparator
	suffix := affixSeparator + id + pr.suffix

	//fmt.Printf("\nexpecting prefix: %s\n", prefix)
	//fmt.Printf("expecting suffix: %s\n", suffix)

	// if we are dealing with old V1 or V2 records, there is no id in affix
	if !strings.Contains(name, id) {
		prefix = pr.prefix
		suffix = pr.suffix
		//fmt.Printf("ID not detected, using old affix\n")
		//fmt.Printf("expecting prefix: %s\n", prefix)
		//fmt.Printf("expecting suffix: %s\n", suffix)
	}

	//fmt.Printf("name: %s\n", name)
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

		// handle old (V1) TXT records
		prefix = pr.dropAffixTemplate(prefix)
		suffix = pr.dropAffixTemplate(suffix)
	}

	if pr.isPrefix() && strings.HasPrefix(name, prefix) {
		return extractRecordTypeDefaultPosition(strings.TrimPrefix(name, prefix))
	}

	if pr.isSuffix() && strings.HasSuffix(name, suffix) {
		return extractRecordTypeDefaultPosition(strings.TrimSuffix(name, suffix))
	}

	return "", ""
}

func (pr affixNameMapper) dropAffixTemplate(name string) string {
	return strings.ReplaceAll(name, recordTemplate, "")
}

func (pr affixNameMapper) isPrefix() bool {
	return len(pr.suffix) == 0
}

func (pr affixNameMapper) isSuffix() bool {
	return len(pr.prefix) == 0 && len(pr.suffix) > 0
}

func (pr affixNameMapper) toEndpointName(txtDNSName, id string) (endpointName string, recordType string) {
	lowerDNSName := strings.ToLower(txtDNSName)
	lowerID := strings.ToLower(id)

	// drop prefix
	if pr.isPrefix() {
		return pr.dropAffixExtractType(lowerDNSName, lowerID)
	}

	// drop suffix
	if pr.isSuffix() {
		dc := strings.Count(pr.suffix, ".")
		DNSName := strings.SplitN(lowerDNSName, ".", 2+dc)
		domainWithSuffix := strings.Join(DNSName[:1+dc], ".")

		r, rType := pr.dropAffixExtractType(domainWithSuffix, lowerID)
		return r + "." + DNSName[1+dc], rType
	}
	return "", ""
}

func (pr affixNameMapper) toTXTName(endpointDNSName string) string {
	DNSName := strings.SplitN(endpointDNSName, ".", 2)

	prefix := pr.dropAffixTemplate(pr.prefix)
	suffix := pr.dropAffixTemplate(pr.suffix)
	// If specified, replace a leading asterisk in the generated txt record name with some other string
	if pr.wildcardReplacement != "" && DNSName[0] == "*" {
		DNSName[0] = pr.wildcardReplacement
	}

	if len(DNSName) < 2 {
		return prefix + DNSName[0] + suffix
	}
	return prefix + DNSName[0] + suffix + "." + DNSName[1]
}

func (pr affixNameMapper) recordTypeInAffix() bool {
	if strings.Contains(pr.prefix, recordTemplate) {
		return true
	}
	if strings.Contains(pr.suffix, recordTemplate) {
		return true
	}
	return false
}

func (pr affixNameMapper) normalizeAffixTemplate(afix, recordType string) string {
	if strings.Contains(afix, recordTemplate) {
		return strings.ReplaceAll(afix, recordTemplate, recordType)
	}
	return afix
}

func (pr affixNameMapper) toNewTXTName(endpointDNSName, id, recordType string) string {
	DNSName := strings.SplitN(endpointDNSName, ".", 2)
	recordType = strings.ToLower(recordType)
	id = strings.ToLower(id)
	recordT := recordType + affixSeparator

	prefix := pr.normalizeAffixTemplate(pr.prefix, recordType)
	suffix := pr.normalizeAffixTemplate(pr.suffix, recordType)

	if pr.isPrefix() {
		prefix = prefix + id + affixSeparator
	} else {
		suffix = affixSeparator + id + suffix
	}

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
