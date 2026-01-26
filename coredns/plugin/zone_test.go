package kuadrant

import (
	"fmt"
	"testing"

	"github.com/coredns/coredns/plugin/file"
	"github.com/miekg/dns"
	"github.com/stretchr/testify/assert"

	"k8s.io/utils/ptr"
	"sigs.k8s.io/external-dns/endpoint"
)

func TestZone_InsertEndpoint(t *testing.T) {
	type args struct {
		ep *endpoint.Endpoint
	}
	tests := []struct {
		name           string
		args           args
		expectedRRData map[string]rrData
	}{
		{
			name: "insert A record",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "a.example.com",
					Targets:    []string{"1.1.1.1"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  60,
				},
			},
			expectedRRData: map[string]rrData{
				"a.example.com.\t60\tIN\tA\t1.1.1.1": {},
			},
		},
		{
			name: "insert CNAME record",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "cname.example.com",
					Targets:    []string{"t.example.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  60,
				},
			},
			expectedRRData: map[string]rrData{
				"cname.example.com.\t60\tIN\tCNAME\tt.example.com.": {},
			},
		},
		{
			name: "insert NS record",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "ns.example.com",
					Targets:    []string{"ns1.example.com"},
					RecordType: endpoint.RecordTypeNS,
					RecordTTL:  60,
				},
			},
			expectedRRData: map[string]rrData{
				"ns.example.com.\t60\tIN\tNS\tns1.example.com.": {},
			},
		},
		{
			name: "insert AAAA record",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "aaaa.example.com",
					Targets:    []string{"2001:db8::68"},
					RecordType: endpoint.RecordTypeAAAA,
					RecordTTL:  60,
				},
			},
			expectedRRData: map[string]rrData{
				"aaaa.example.com.\t60\tIN\tAAAA\t2001:db8::68": {},
			},
		},
		{
			name: "insert TXT record",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "txt.example.com",
					Targets:    []string{"foo=bar"},
					RecordType: endpoint.RecordTypeTXT,
					RecordTTL:  60,
				},
			},
			expectedRRData: map[string]rrData{
				"txt.example.com.\t60\tIN\tTXT\t\"foo=bar\"": {},
			},
		},
		{
			name: "insert A record with multiple targets",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "a.example.com",
					Targets:    []string{"1.1.1.1", "2.2.2.2"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  60,
				},
			},
			expectedRRData: map[string]rrData{
				"a.example.com.\t60\tIN\tA\t1.1.1.1": {},
				"a.example.com.\t60\tIN\tA\t2.2.2.2": {},
			},
		},
		{
			name: "insert A record with geo",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "a.example.com",
					Targets:    []string{"1.1.1.1"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  60,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "geo-code",
							Value: "GEO-EU",
						},
					},
				},
			},
			expectedRRData: map[string]rrData{
				"a.example.com.\t60\tIN\tA\t1.1.1.1": {
					geo: ptr.To("GEO-EU"),
				},
			},
		},
		{
			name: "insert A record with weight",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "a.example.com",
					Targets:    []string{"1.1.1.1"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  60,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "weight",
							Value: "100",
						},
					},
				},
			},
			expectedRRData: map[string]rrData{
				"a.example.com.\t60\tIN\tA\t1.1.1.1": {
					weight: ptr.To(int64(100)),
				},
			},
		},
		{
			name: "insert A record with weight and geo (prefer weight)",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "a.example.com",
					Targets:    []string{"1.1.1.1"},
					RecordType: endpoint.RecordTypeA,
					RecordTTL:  60,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "weight",
							Value: "100",
						},
						{
							Name:  "geo-code",
							Value: "GEO-EU",
						},
					},
				},
			},
			expectedRRData: map[string]rrData{
				"a.example.com.\t60\tIN\tA\t1.1.1.1": {
					weight: ptr.To(int64(100)),
				},
			},
		},
		{
			name: "insert CNAME record with geo",
			args: args{
				ep: &endpoint.Endpoint{
					DNSName:    "cname.example.com",
					Targets:    []string{"t.example.com"},
					RecordType: endpoint.RecordTypeCNAME,
					RecordTTL:  60,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "geo-code",
							Value: "GEO-EU",
						},
					},
				},
			},
			expectedRRData: map[string]rrData{
				"cname.example.com.\t60\tIN\tCNAME\tt.example.com.": {
					geo: ptr.To("GEO-EU"),
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			z := &Zone{
				origin:  dns.Fqdn("example.com"),
				origLen: dns.CountLabel(dns.Fqdn("example.com")),
				Zone:    file.NewZone("example.com", ""),
				rrData:  map[string]rrData{},
			}
			assert.NoError(t, z.InsertEndpoint(tt.args.ep), fmt.Sprintf("InsertEndpoint(%v)", tt.args.ep))
			assert.Equal(t, tt.expectedRRData, z.rrData)
		})
	}
}

func TestConvertEmailToMailbox(t *testing.T) {
	tests := []struct {
		name     string
		email    string
		expected string
	}{
		{
			name:     "standard email",
			email:    "admin@example.com",
			expected: "admin.example.com.",
		},
		{
			name:     "email with spaces",
			email:    "  admin@example.com  ",
			expected: "admin.example.com.",
		},
		{
			name:     "subdomain email",
			email:    "hostmaster@sub.example.com",
			expected: "hostmaster.sub.example.com.",
		},
		{
			name:     "email with dot in local part (RFC 1035/2142 - dots must be escaped)",
			email:    "dns.admin@example.com",
			expected: "dns\\.admin.example.com.",
		},
		{
			name:     "email with multiple dots in local part",
			email:    "my.dns.admin@example.com",
			expected: "my\\.dns\\.admin.example.com.",
		},
		{
			name:     "empty email (invalid)",
			email:    "",
			expected: "",
		},
		{
			name:     "email missing @ symbol (invalid)",
			email:    "adminexample.com",
			expected: "",
		},
		{
			name:     "email with empty local part (invalid)",
			email:    "@example.com",
			expected: "",
		},
		{
			name:     "email with empty domain (invalid)",
			email:    "admin@",
			expected: "",
		},
		{
			name:     "email with spaces in local part (invalid after trim)",
			email:    "  @example.com",
			expected: "",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := convertEmailToMailbox(tt.email)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestNewZone_CustomRNAME(t *testing.T) {
	tests := []struct {
		name         string
		zoneName     string
		rname        string
		expectedMbox string
	}{
		{
			name:         "default rname (empty string)",
			zoneName:     "example.com",
			rname:        "",
			expectedMbox: "hostmaster.example.com.",
		},
		{
			name:         "custom rname",
			zoneName:     "example.com",
			rname:        "admin@example.com",
			expectedMbox: "admin.example.com.",
		},
		{
			name:         "custom rname with subdomain",
			zoneName:     "sub.example.com",
			rname:        "dns@sub.example.com",
			expectedMbox: "dns.sub.example.com.",
		},
		{
			name:         "custom rname with dot in local part (RFC 1035/2142)",
			zoneName:     "example.com",
			rname:        "dns.admin@example.com",
			expectedMbox: "dns\\.admin.example.com.",
		},
		{
			name:         "invalid rname falls back to default",
			zoneName:     "example.com",
			rname:        "invalid-email-no-at",
			expectedMbox: "hostmaster.example.com.",
		},
		{
			name:         "invalid rname with empty domain falls back to default",
			zoneName:     "example.com",
			rname:        "admin@",
			expectedMbox: "hostmaster.example.com.",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			zone := NewZone(tt.zoneName, tt.rname)
			assert.NotNil(t, zone)

			// Find the SOA record in the zone
			var soaRecord *dns.SOA
			zone.RLock()
			apex := zone.Apex
			zone.RUnlock()

			if apex.SOA != nil {
				soaRecord = apex.SOA
			}

			assert.NotNil(t, soaRecord, "SOA record should exist")
			if soaRecord != nil {
				assert.Equal(t, tt.expectedMbox, soaRecord.Mbox, "SOA Mbox should match expected value")
			}
		})
	}
}

func TestZone_RefreshPreservesRNAME(t *testing.T) {
	// Create a zone with custom RNAME
	originalRname := "dns.admin@example.com"
	zone1 := NewZone("example.com", originalRname)
	assert.NotNil(t, zone1)

	// Verify the zone has the rname field set
	assert.Equal(t, originalRname, zone1.rname)

	// Get the original SOA record
	zone1.RLock()
	originalSOA := zone1.Apex.SOA
	zone1.RUnlock()
	assert.NotNil(t, originalSOA)
	assert.Equal(t, "dns\\.admin.example.com.", originalSOA.Mbox)

	// Simulate zone refresh by creating a new zone with the preserved rname
	zone2 := NewZone("example.com", zone1.rname)
	assert.NotNil(t, zone2)

	// Verify the refreshed zone has the same rname
	assert.Equal(t, originalRname, zone2.rname)

	// Get the new SOA record
	zone2.RLock()
	newSOA := zone2.Apex.SOA
	zone2.RUnlock()
	assert.NotNil(t, newSOA)

	// Verify the SOA RNAME is preserved
	assert.Equal(t, originalSOA.Mbox, newSOA.Mbox, "SOA RNAME should be preserved after zone refresh")
	assert.Equal(t, "dns\\.admin.example.com.", newSOA.Mbox)
}
