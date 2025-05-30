package registry

import (
	"strings"

	"sigs.k8s.io/external-dns/endpoint"
)

/**
  nameMapper is the interface for mapping between the endpoint for the source
  and the endpoint for the TXT record.
*/

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
func extractRecordTypeDefaultPosition(name string) (string, string) {
	nameS := strings.Split(name, "-")
	for _, t := range getSupportedTypes() {
		if nameS[0] == strings.ToLower(t) {
			return strings.TrimPrefix(name, nameS[0]+"-"), t
		}
	}
	return name, ""
}

func (pr affixNameMapper) extractOwnerIDAndRecordType(name string) (baseName, recordType, ownerID string) {
	nameS := strings.Split(name, affixSeparator)
	if pr.isPrefix() {

		// could be
		// owner-type-hostname
		// type-hostname
		// hostname
		// ensure there is an owner
		if len(nameS[0]) == ownerIDLen && len(nameS) > 2 {
			ownerID = nameS[0]
			name = strings.Join(nameS[1:], affixSeparator)
		}
	}

	if pr.isSuffix() {

		// could be
		// type-hostname-id
		// type-hostname
		// hostname
		if len(nameS[len(nameS)-1]) == ownerIDLen && len(nameS) > 2 {
			ownerID = nameS[len(nameS)-1]
			name = strings.Join(nameS[:len(nameS)-1], affixSeparator)
		}
	}

	baseName, recordType = extractRecordTypeDefaultPosition(name)

	return baseName, recordType, ownerID
}

// dropAffixExtractType strips TXT record to find an endpoint name it manages
// it also returns the record type
func (pr affixNameMapper) dropAffixExtractType(name string) (baseName, recordType, ownerID string) {
	// potential values are:
	// prefixowner-recordtype-dnsname
	// prefixrecordtype-dnsname
	// prefixdnsname
	// recordtype-dnsname-ownersuffix
	// dnsname-ownersuffix
	// dnsnamesuffix

	if pr.isPrefix() && strings.HasPrefix(name, pr.prefix) {
		return pr.extractOwnerIDAndRecordType(strings.TrimPrefix(name, pr.prefix))
	}

	if pr.isSuffix() && strings.HasSuffix(name, pr.suffix) {
		return pr.extractOwnerIDAndRecordType(strings.TrimSuffix(name, pr.suffix))
	}

	return "", "", ""
}

func (pr affixNameMapper) isPrefix() bool {
	return len(pr.suffix) == 0
}

func (pr affixNameMapper) isSuffix() bool {
	return len(pr.prefix) == 0 && len(pr.suffix) > 0
}

func (pr affixNameMapper) toEndpointName(txtDNSName string) (endpointName, recordType, ownerID string) {
	lowerDNSName := strings.ToLower(txtDNSName)

	// drop prefix
	if pr.isPrefix() {
		return pr.dropAffixExtractType(lowerDNSName)
	}

	// drop suffix
	if pr.isSuffix() {
		dc := strings.Count(pr.suffix, ".")
		DNSName := strings.SplitN(lowerDNSName, ".", 2+dc)
		domainWithSuffix := strings.Join(DNSName[:1+dc], ".")

		r, rType, owner := pr.dropAffixExtractType(domainWithSuffix)
		return r + "." + DNSName[1+dc], rType, owner
	}
	return "", "", ""
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

func (pr affixNameMapper) toTXTName(endpointDNSName, id, recordType string) string {
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
