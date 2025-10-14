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
