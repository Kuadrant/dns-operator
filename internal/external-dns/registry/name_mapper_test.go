package registry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ExternalDNS only
func TestDropPrefix(t *testing.T) {
	mapper := NewExternalDNSAffixNameMapper("foo-", "", "").(externalDNSAffixNameMapper)

	tests := []struct {
		txtName          string
		expectedHostname string
		expectedType     string
	}{
		{
			"foo-a-test.example.com",
			"test.example.com",
			"A",
		},
		// this is not a format we support - id plus prefix
		{
			"foo-11111111-test.example.com",
			"11111111-test.example.com",
			"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.txtName, func(t *testing.T) {
			actualOutput, gotType := mapper.dropAffixExtractType(tc.txtName)
			assert.Equal(t, tc.expectedHostname, actualOutput)
			assert.Equal(t, tc.expectedType, gotType)
		})
	}
}

func TestDropSuffix(t *testing.T) {
	mapper := NewExternalDNSAffixNameMapper("", "-foo", "").(externalDNSAffixNameMapper)

	tests := []struct {
		txtName      string
		expectedHost string
		expectedType string
	}{
		{
			"a-test-foo.example.com",
			"test.example.com",
			"A",
		},
		{
			"test--foo.example.com",
			"test-.example.com",
			"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.txtName, func(t *testing.T) {
			r := strings.SplitN(tc.txtName, ".", 2)
			rClean, recordType := mapper.dropAffixExtractType(r[0])
			actualOutput := rClean + "." + r[1]
			assert.Equal(t, tc.expectedHost, actualOutput)
			assert.Equal(t, tc.expectedType, recordType)
		})
	}
}

func TestExtractRecordTypeDefaultPosition(t *testing.T) {
	mapper := NewExternalDNSAffixNameMapper("", "-foo", "").(externalDNSAffixNameMapper)

	tests := []struct {
		input        string
		expectedName string
		expectedType string
	}{
		{
			input:        "ns-zone.example.com",
			expectedName: "zone.example.com",
			expectedType: "NS",
		},
		{
			input:        "aaaa-zone.example.com",
			expectedName: "zone.example.com",
			expectedType: "AAAA",
		},
		{
			input:        "ptr-zone.example.com",
			expectedName: "ptr-zone.example.com",
			expectedType: "",
		},
		{
			input:        "zone.example.com",
			expectedName: "zone.example.com",
			expectedType: "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			actualName, actualType := mapper.extractRecordTypeDefaultPosition(tc.input)
			assert.Equal(t, tc.expectedName, actualName)
			assert.Equal(t, tc.expectedType, actualType)
		})
	}
}

func TestToTXTName(t *testing.T) {
	tests := []struct {
		name       string
		mapper     NameMapper
		domain     string
		txtDomain  string
		recordType string
		id         string
	}{
		{
			name:       "prefix with dash",
			mapper:     newKuadrantAffixMapper(legacyMapperTemplate{}, "foo-", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "foo-2tqs20a7-a-example.com",
			id:         "owner1",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.txtDomain, tc.mapper.ToTXTName(tc.domain, tc.id, tc.recordType))
		})
	}
}

func TestToEndpointsName(t *testing.T) {
	tests := []struct {
		name               string
		mapper             NameMapper
		txtName            string
		expectedDomain     string
		expectedRecordType string
		version            string
	}{
		// new "V3" records - id, type and affix. In codebase referred as V1
		{
			name:               "prefix with dash",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{}, "foo-", ""),
			txtName:            "foo-11111111-cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
			version:            "1",
		},
		// old "V2" records - type and affix. In codebase has no version associated. Calling them V2 here to simplify maintenance
		{
			name:               "prefix",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{"": {"foo", "", ""}}, "", ""),
			txtName:            "fooa-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "A",
		},
		{
			name:               "suffix",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{"": {"", "foo", ""}}, "", ""),
			txtName:            "cname-examplefoo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		{
			name:               "prefix with dash",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{"": {"foo-", "", ""}}, "", ""),
			txtName:            "foo-cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		{
			name:               "suffix with dash",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{"": {"", "-foo", ""}}, "", ""),
			txtName:            "a-example-foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "A",
		},
		{
			name:               "prefix with dot",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{"": {"foo.", "", ""}}, "", ""),
			txtName:            "foo.cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		{
			name:               "suffix with dot",
			mapper:             newKuadrantAffixMapper(legacyMapperTemplate{"": {"", ".foo", ""}}, "", ""),
			txtName:            "cname-example.foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		// old "V1" records - affix. In codebase has no version associated. Calling them V1 here to simplify maintenance
		{
			name:           "prefix",
			mapper:         newKuadrantAffixMapper(legacyMapperTemplate{"": {"foo", "", ""}}, "", ""),
			txtName:        "fooexample.com",
			expectedDomain: "example.com",
		},
		{
			name:           "suffix",
			mapper:         newKuadrantAffixMapper(legacyMapperTemplate{"": {"", "foo", ""}}, "", ""),
			txtName:        "examplefoo.com",
			expectedDomain: "example.com",
		},
		{
			name:           "prefix with dash",
			mapper:         newKuadrantAffixMapper(legacyMapperTemplate{"": {"foo-", "", ""}}, "", ""),
			txtName:        "foo-example.com",
			expectedDomain: "example.com",
		},
		{
			name:           "suffix with dash",
			mapper:         newKuadrantAffixMapper(legacyMapperTemplate{"": {"", "-foo", ""}}, "", ""),
			txtName:        "example-foo.com",
			expectedDomain: "example.com",
		},
		{
			name:           "prefix with dot",
			mapper:         newKuadrantAffixMapper(legacyMapperTemplate{"": {"foo.", "", ""}}, "", ""),
			txtName:        "foo.example.com",
			expectedDomain: "example.com",
		},
		{
			name:           "suffix with dot",
			mapper:         newKuadrantAffixMapper(legacyMapperTemplate{"": {"", ".foo", ""}}, "", ""),
			txtName:        "example.foo.com",
			expectedDomain: "example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			domain, recordType := tc.mapper.ToEndpointName(tc.txtName, tc.version)
			assert.Equal(t, tc.expectedDomain, domain)
			assert.Equal(t, tc.expectedRecordType, recordType)
		})
	}
}
