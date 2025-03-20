package registry

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestDropPrefix(t *testing.T) {
	mapper := newaffixNameMapper("foo-", "", "")

	tests := []struct {
		txtName          string
		expectedHostname string
		expectedID       string
		expectedType     string
		version          string
	}{
		{
			"foo-11111111-cname-test.example.com",
			"test.example.com",
			"11111111",
			"CNAME",
			"1",
		},
		{
			"foo-a-test.example.com",
			"test.example.com",
			"",
			"A",
			"",
		},
		// this is not a format we support - id plus prefix
		{
			"foo-11111111-test.example.com",
			"11111111-test.example.com",
			"",
			"",
			"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.txtName, func(t *testing.T) {
			actualOutput, gotType, gotID := mapper.dropAffixExtractType(tc.txtName, tc.version)
			assert.Equal(t, tc.expectedHostname, actualOutput)
			assert.Equal(t, tc.expectedID, gotID)
			assert.Equal(t, tc.expectedType, gotType)
		})
	}
}

func TestDropSuffix(t *testing.T) {
	mapper := newaffixNameMapper("", "-foo", "")

	tests := []struct {
		txtName      string
		expectedHost string
		expectedID   string
		expectedType string
		version      string
	}{
		{
			"a-test-foo.example.com",
			"test.example.com",
			"",
			"A",
			"",
		},
		{
			"test--foo.example.com",
			"test-.example.com",
			"",
			"",
			"",
		},
	}

	for _, tc := range tests {
		t.Run(tc.txtName, func(t *testing.T) {
			r := strings.SplitN(tc.txtName, ".", 2)
			rClean, recordType, gotID := mapper.dropAffixExtractType(r[0], tc.version)
			actualOutput := rClean + "." + r[1]
			assert.Equal(t, tc.expectedHost, actualOutput)
			assert.Equal(t, tc.expectedID, gotID)
			assert.Equal(t, tc.expectedType, recordType)
		})
	}
}

func TestExtractRecordTypeDefaultPosition(t *testing.T) {
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
			actualName, actualType := extractRecordTypeDefaultPosition(tc.input)
			assert.Equal(t, tc.expectedName, actualName)
			assert.Equal(t, tc.expectedType, actualType)
		})
	}
}

func TestToTXTName(t *testing.T) {
	tests := []struct {
		name       string
		mapper     affixNameMapper
		domain     string
		txtDomain  string
		recordType string
		id         string
	}{
		{
			name:       "prefix",
			mapper:     newaffixNameMapper("foo", "", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "foo11111111-a-example.com",
			id:         "11111111",
		},
		{
			name:       "suffix",
			mapper:     newaffixNameMapper("", "foo", ""),
			domain:     "example.com",
			recordType: "AAAA",
			txtDomain:  "aaaa-example-11111111foo.com",
			id:         "11111111",
		},
		{
			name:       "prefix with dash",
			mapper:     newaffixNameMapper("foo-", "", ""),
			domain:     "example.com",
			recordType: "A",
			txtDomain:  "foo-11111111-a-example.com",
			id:         "11111111",
		},
		{
			name:       "suffix with dash",
			mapper:     newaffixNameMapper("", "-foo", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cname-example-11111111-foo.com",
			id:         "11111111",
		},
		{
			name:       "prefix with dot",
			mapper:     newaffixNameMapper("foo.", "", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "foo.11111111-cname-example.com",
			id:         "11111111",
		},
		{
			name:       "suffix with dot",
			mapper:     newaffixNameMapper("", ".foo", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cname-example-11111111.foo.com",
			id:         "11111111",
		},
		{
			name:       "prefix with multiple dots",
			mapper:     newaffixNameMapper("foo.bar.", "", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "foo.bar.11111111-cname-example.com",
			id:         "11111111",
		},
		{
			name:       "suffix with multiple dots",
			mapper:     newaffixNameMapper("", ".foo.bar.test", ""),
			domain:     "example.com",
			recordType: "CNAME",
			txtDomain:  "cname-example-11111111.foo.bar.test.com",
			id:         "11111111",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.txtDomain, tc.mapper.toTXTName(tc.domain, tc.id, tc.recordType))
		})
	}
}

func TestToEndpointsName(t *testing.T) {
	tests := []struct {
		name               string
		mapper             affixNameMapper
		txtName            string
		expectedDomain     string
		expectedRecordType string
		expectedID         string
		version            string
	}{
		// new "V3" records - id, type and affix. In codebase referred as V1
		{
			name:               "prefix",
			mapper:             newaffixNameMapper("foo", "", ""),
			txtName:            "foo11111111-a-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "A",
			expectedID:         "11111111",
			version:            "1",
		},
		{
			name:               "suffix",
			mapper:             newaffixNameMapper("", "foo", ""),
			txtName:            "cname-example-11111111foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
			expectedID:         "11111111",
			version:            "1",
		},
		{
			name:               "prefix with dash",
			mapper:             newaffixNameMapper("foo-", "", ""),
			txtName:            "foo-11111111-cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
			expectedID:         "11111111",
			version:            "1",
		},
		{
			name:               "suffix with dash",
			mapper:             newaffixNameMapper("", "-foo", ""),
			txtName:            "a-example-11111111-foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "A",
			expectedID:         "11111111",
			version:            "1",
		},
		{
			name:               "prefix with dot",
			mapper:             newaffixNameMapper("foo.", "", ""),
			txtName:            "foo.11111111-cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
			expectedID:         "11111111",
			version:            "1",
		},
		{
			name:               "suffix with dot",
			mapper:             newaffixNameMapper("", ".foo", ""),
			txtName:            "cname-example-11111111.foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
			expectedID:         "11111111",
			version:            "1",
		},
		// old "V2" records - type and affix. In codebase has no version associated. Calling them V2 here to simplify maintenance
		{
			name:               "prefix",
			mapper:             newaffixNameMapper("foo", "", ""),
			txtName:            "fooa-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "A",
		},
		{
			name:               "suffix",
			mapper:             newaffixNameMapper("", "foo", ""),
			txtName:            "cname-examplefoo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		{
			name:               "prefix with dash",
			mapper:             newaffixNameMapper("foo-", "", ""),
			txtName:            "foo-cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		{
			name:               "suffix with dash",
			mapper:             newaffixNameMapper("", "-foo", ""),
			txtName:            "a-example-foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "A",
		},
		{
			name:               "prefix with dot",
			mapper:             newaffixNameMapper("foo.", "", ""),
			txtName:            "foo.cname-example.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		{
			name:               "suffix with dot",
			mapper:             newaffixNameMapper("", ".foo", ""),
			txtName:            "cname-example.foo.com",
			expectedDomain:     "example.com",
			expectedRecordType: "CNAME",
		},
		// old "V1" records - affix. In codebase has no version associated. Calling them V1 here to simplify maintenance
		{
			name:           "prefix",
			mapper:         newaffixNameMapper("foo", "", ""),
			txtName:        "fooexample.com",
			expectedDomain: "example.com",
		},
		{
			name:           "suffix",
			mapper:         newaffixNameMapper("", "foo", ""),
			txtName:        "examplefoo.com",
			expectedDomain: "example.com",
		},
		{
			name:           "prefix with dash",
			mapper:         newaffixNameMapper("foo-", "", ""),
			txtName:        "foo-example.com",
			expectedDomain: "example.com",
		},
		{
			name:           "suffix with dash",
			mapper:         newaffixNameMapper("", "-foo", ""),
			txtName:        "example-foo.com",
			expectedDomain: "example.com",
		},
		{
			name:           "prefix with dot",
			mapper:         newaffixNameMapper("foo.", "", ""),
			txtName:        "foo.example.com",
			expectedDomain: "example.com",
		},
		{
			name:           "suffix with dot",
			mapper:         newaffixNameMapper("", ".foo", ""),
			txtName:        "example.foo.com",
			expectedDomain: "example.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			domain, recordType, id := tc.mapper.toEndpointName(tc.txtName, tc.version)
			assert.Equal(t, tc.expectedDomain, domain)
			assert.Equal(t, tc.expectedRecordType, recordType)
			assert.Equal(t, tc.expectedID, id)
		})
	}
}
