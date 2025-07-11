package registry

import (
	"strings"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

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

	endpointName = dnsNameSplit[2]

	// undo wc replacement
	if strings.HasPrefix(endpointName, pr.wildcardReplacement) {
		strings.Replace(endpointName, pr.wildcardReplacement, "*", 1)
	}

	return endpointName, recordType
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

	return pr.prefix + hash.ToBase36HashLen(id, 8) + affixSeparator + strings.ToLower(recordType) + affixSeparator + dnsName
}
