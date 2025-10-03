package registry

import (
	"strings"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

/**
  nameMapper is the interface for mapping between the endpoint for the source
  and the endpoint for the TXT record.
*/

type NameMapper interface {
	ToEndpointName(txtDNSName, version string) (endpointName, recordType string)
	ToTXTName(string, string, string) string
}

type kuadrantAffixNameMapper struct {
	legacyMappers       map[string]NameMapper
	prefix              string
	wildcardReplacement string
}

type legacyMapperTemplate map[string]struct {
	prefix              string
	suffix              string
	wildcardReplacement string
}

var _ NameMapper = kuadrantAffixNameMapper{}

func newKuadrantAffixMapper(legacyMappersFor legacyMapperTemplate, prefix, wildcardReplacement string) NameMapper {
	affixMappers := make(map[string]NameMapper)

	for version, params := range legacyMappersFor {
		affixMappers[version] = legacyAffixMappers[version](params.prefix, params.suffix, params.wildcardReplacement)
	}
	return kuadrantAffixNameMapper{
		legacyMappers:       affixMappers,
		prefix:              prefix,
		wildcardReplacement: wildcardReplacement,
	}
}

func (pr kuadrantAffixNameMapper) ToEndpointName(txtDNSName, version string) (endpointName, recordType string) {
	// legacy
	if version != txtFormatVersion {
		return pr.legacyMappers[version].ToEndpointName(txtDNSName, version)
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

func (pr kuadrantAffixNameMapper) ToTXTName(endpointDNSName, id, recordType string) string {
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
