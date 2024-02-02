// //go:build unit

package google

import (
	"context"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	dnsv1 "google.golang.org/api/dns/v1"

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
	"github.com/kuadrant/kuadrant-dns-operator/internal/provider"
)

func TestGoogleDNSProvider_toManagedZoneOutput(t *testing.T) {
	mockListCall := &MockResourceRecordSetsListCall{
		PagesFunc: func(ctx context.Context, f func(*dnsv1.ResourceRecordSetsListResponse) error) error {
			mockResponse := &dnsv1.ResourceRecordSetsListResponse{
				Rrsets: []*dnsv1.ResourceRecordSet{
					{
						Name: "TestRecordSet1",
					},
					{
						Name: "TestRecordSet2",
					},
				},
			}
			return f(mockResponse)
		},
	}
	mockClient := &MockResourceRecordSetsClient{
		ListFunc: func(project string, managedZone string) resourceRecordSetsListCallInterface {
			return mockListCall
		},
	}

	mockListCallErr := &MockResourceRecordSetsListCall{
		PagesFunc: func(ctx context.Context, f func(*dnsv1.ResourceRecordSetsListResponse) error) error {

			error := fmt.Errorf("status 400 ")
			return error
		},
	}
	mockClientErr := &MockResourceRecordSetsClient{
		ListFunc: func(project string, managedZone string) resourceRecordSetsListCallInterface {
			return mockListCallErr
		},
	}

	type fields struct {
		resourceRecordSetsClient resourceRecordSetsClientInterface
	}
	type args struct {
		mz *dnsv1.ManagedZone
	}
	tests := []struct {
		name    string
		fields  fields
		args    args
		want    provider.ManagedZoneOutput
		wantErr bool
	}{

		{
			name: "Successful test",
			fields: fields{
				resourceRecordSetsClient: mockClient,
			},
			args: args{
				&dnsv1.ManagedZone{
					Name: "testname",
					NameServers: []string{
						"nameserver1",
						"nameserver2",
					},
				},
			},
			want: provider.ManagedZoneOutput{
				ID: "testname",
				NameServers: []*string{
					aws.String("nameserver1"),
					aws.String("nameserver2"),
				},
				RecordCount: 2,
			},
			wantErr: false,
		},
		{
			name: "UnSuccessful test",
			fields: fields{
				resourceRecordSetsClient: mockClientErr,
			},
			args: args{
				&dnsv1.ManagedZone{
					Name: "testname",
					NameServers: []string{
						"nameserver1",
						"nameserver2",
					},
				},
			},
			want: provider.ManagedZoneOutput{
				ID: "testname",
				NameServers: []*string{
					aws.String("nameserver1"),
					aws.String("nameserver2"),
				},
				RecordCount: 0,
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := &GoogleDNSProvider{
				resourceRecordSetsClient: tt.fields.resourceRecordSetsClient,
			}
			got, err := g.toManagedZoneOutput(tt.args.mz)
			if (err != nil) != tt.wantErr {
				t.Errorf("GoogleDNSProvider.toManagedZoneOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("GoogleDNSProvider.toManagedZoneOutput() = %v, want %v", got, tt.want)
			}
		})
	}
}

func Test_toResourceRecordSets(t *testing.T) {
	type args struct {
		allEndpoints []*v1alpha1.Endpoint
	}
	tests := []struct {
		name string
		args args
		want []*dnsv1.ResourceRecordSet
	}{
		{
			name: "Successful test Geo & weight",
			args: args{
				allEndpoints: []*v1alpha1.Endpoint{
					{
						DNSName:    "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType: "A",
						RecordTTL:  60,
						Targets: v1alpha1.Targets{
							"0.0.0.0",
						},
						ProviderSpecific: v1alpha1.ProviderSpecific{},
						SetIdentifier:    "",
					},
					{
						DNSName:       "europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordTTL:     60,
						Targets: v1alpha1.Targets{
							"2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						},
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "weight",
								Value: "60",
							},
						},
					},
					{
						DNSName:       "lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "europe-west1",
						Targets: []string{
							"europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
						},
						RecordTTL: 300,
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "europe-west1",
							},
						},
					},
					{
						DNSName:    "unittest.google.hcpapps.net",
						RecordType: "CNAME",
						RecordTTL:  300,
						Targets: []string{
							"lb-4ej5le.unittest.google.hcpapps.net",
						},
						SetIdentifier: "",
					},
				},
			},
			want: []*dnsv1.ResourceRecordSet{
				{
					Name: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"0.0.0.0",
					},
					Ttl:  60,
					Type: "A",
				},
				{
					Name: "europe-west1.lb-4ej5le.unittest.google.hcpapps.net.",
					RoutingPolicy: &dnsv1.RRSetRoutingPolicy{
						Wrr: &dnsv1.RRSetRoutingPolicyWrrPolicy{
							Items: []*dnsv1.RRSetRoutingPolicyWrrPolicyWrrPolicyItem{
								{
									Rrdatas: []string{
										"2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
									},
									Weight: 60,
								},
							},
						},
					},
					Ttl:  60,
					Type: "CNAME",
				},

				{
					Name: "lb-4ej5le.unittest.google.hcpapps.net.",
					RoutingPolicy: &dnsv1.RRSetRoutingPolicy{
						Geo: &dnsv1.RRSetRoutingPolicyGeoPolicy{
							EnableFencing: false,
							Items: []*dnsv1.RRSetRoutingPolicyGeoPolicyGeoPolicyItem{
								{
									Location: "europe-west1",
									Rrdatas: []string{
										"europe-west1.lb-4ej5le.unittest.google.hcpapps.net.",
									},
								},
							},
						},
					},
					Ttl:  300,
					Type: "CNAME",
				},

				{
					Name: "unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"lb-4ej5le.unittest.google.hcpapps.net.",
					},
					Ttl:  300,
					Type: "CNAME",
				},
			},
		},
		{
			name: "Successful test no Geo & weight",
			args: args{
				allEndpoints: []*v1alpha1.Endpoint{
					{
						DNSName:    "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType: "A",
						RecordTTL:  60,
						Targets: v1alpha1.Targets{
							"0.0.0.0",
						},
						SetIdentifier: "",
					},
					{
						DNSName:       "default.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordTTL:     60,
						Targets: v1alpha1.Targets{
							"2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						},
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "weight",
								Value: "120",
							},
						},
					},
					{
						DNSName:       "lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "default",
						Targets: []string{
							"default.lb-4ej5le.unittest.google.hcpapps.net",
						},
						RecordTTL: 300,
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "*",
							},
						},
					},
					{
						DNSName:    "unittest.google.hcpapps.net",
						RecordType: "CNAME",
						RecordTTL:  300,
						Targets: []string{
							"lb-4ej5le.unittest.google.hcpapps.net",
						},
						SetIdentifier: "",
					},
				},
			},
			want: []*dnsv1.ResourceRecordSet{
				{
					Name: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"0.0.0.0",
					},
					Ttl:  60,
					Type: "A",
				},
				{
					Name: "default.lb-4ej5le.unittest.google.hcpapps.net.",
					RoutingPolicy: &dnsv1.RRSetRoutingPolicy{
						Wrr: &dnsv1.RRSetRoutingPolicyWrrPolicy{
							Items: []*dnsv1.RRSetRoutingPolicyWrrPolicyWrrPolicyItem{
								{
									Rrdatas: []string{
										"2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
									},
									Weight: 120,
								},
							},
						},
					},
					Ttl:  60,
					Type: "CNAME",
				},
				{
					Name: "lb-4ej5le.unittest.google.hcpapps.net.",
					RoutingPolicy: &dnsv1.RRSetRoutingPolicy{
						Geo: &dnsv1.RRSetRoutingPolicyGeoPolicy{
							EnableFencing: false,
							Items: []*dnsv1.RRSetRoutingPolicyGeoPolicyGeoPolicyItem{
								{
									Location: "europe-west1",
									Rrdatas: []string{
										"default.lb-4ej5le.unittest.google.hcpapps.net.",
									},
								},
							},
						},
					},
					Ttl:  300,
					Type: "CNAME",
				},
				{
					Name: "unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"lb-4ej5le.unittest.google.hcpapps.net.",
					},
					Ttl:  300,
					Type: "CNAME",
				},
			},
		},
		{
			name: "Successful test weight round robin with multiple targets",
			args: args{
				allEndpoints: []*v1alpha1.Endpoint{
					{
						DNSName:    "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType: "A",
						RecordTTL:  60,
						Targets: v1alpha1.Targets{
							"0.0.0.0",
						},
						SetIdentifier: "",
					},
					{
						DNSName:    "lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType: "A",
						RecordTTL:  60,
						Targets: v1alpha1.Targets{
							"0.0.0.1",
						},
						SetIdentifier: "",
					},
					{
						DNSName:       "default.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordTTL:     60,
						Targets: v1alpha1.Targets{
							"2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						},
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "weight",
								Value: "120",
							},
						},
					},
					{
						DNSName:       "default.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
						RecordTTL:     60,
						Targets: v1alpha1.Targets{
							"lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
						},
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "weight",
								Value: "120",
							},
						},
					},
					{
						DNSName:       "lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "default",
						Targets: []string{
							"default.lb-4ej5le.unittest.google.hcpapps.net",
						},
						RecordTTL: 300,
						ProviderSpecific: v1alpha1.ProviderSpecific{
							v1alpha1.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "*",
							},
						},
					},
					{
						DNSName:    "unittest.google.hcpapps.net",
						RecordType: "CNAME",
						RecordTTL:  300,
						Targets: []string{
							"lb-4ej5le.unittest.google.hcpapps.net",
						},
						SetIdentifier: "",
					},
				},
			},
			want: []*dnsv1.ResourceRecordSet{
				{
					Name: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"0.0.0.0",
					},
					Ttl:  60,
					Type: "A",
				},
				{
					Name: "lrnse3.lb-4ej5le.unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"0.0.0.1",
					},
					Ttl:  60,
					Type: "A",
				},
				{
					Name: "default.lb-4ej5le.unittest.google.hcpapps.net.",
					RoutingPolicy: &dnsv1.RRSetRoutingPolicy{
						Wrr: &dnsv1.RRSetRoutingPolicyWrrPolicy{
							Items: []*dnsv1.RRSetRoutingPolicyWrrPolicyWrrPolicyItem{
								{
									Rrdatas: []string{
										"2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
									},
									Weight: 120,
								},
								{
									Rrdatas: []string{
										"lrnse3.lb-4ej5le.unittest.google.hcpapps.net.",
									},
									Weight: 120,
								},
							},
						},
					},
					Ttl:  60,
					Type: "CNAME",
				},
				{
					Name: "lb-4ej5le.unittest.google.hcpapps.net.",
					RoutingPolicy: &dnsv1.RRSetRoutingPolicy{
						Geo: &dnsv1.RRSetRoutingPolicyGeoPolicy{
							EnableFencing: false,
							Items: []*dnsv1.RRSetRoutingPolicyGeoPolicyGeoPolicyItem{
								{
									Location: "europe-west1",
									Rrdatas: []string{
										"default.lb-4ej5le.unittest.google.hcpapps.net.",
									},
								},
							},
						},
					},
					Ttl:  300,
					Type: "CNAME",
				},
				{
					Name: "unittest.google.hcpapps.net.",
					Rrdatas: []string{
						"lb-4ej5le.unittest.google.hcpapps.net.",
					},
					Ttl:  300,
					Type: "CNAME",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := toResourceRecordSets(tt.args.allEndpoints)
			sorted(got)
			sorted(tt.want)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("toResourceRecordSets() = %+v, want %+v", got, tt.want)
			}

		})
	}
}
func sorted(rrset []*dnsv1.ResourceRecordSet) {
	sort.Slice(rrset, func(i, j int) bool {
		return rrset[i].Name < rrset[j].Name
	})
}

type MockResourceRecordSetsListCall struct {
	PagesFunc func(ctx context.Context, f func(*dnsv1.ResourceRecordSetsListResponse) error) error
}

func (m *MockResourceRecordSetsListCall) Pages(ctx context.Context, f func(*dnsv1.ResourceRecordSetsListResponse) error) error {
	return m.PagesFunc(ctx, f)

}

type MockResourceRecordSetsClient struct {
	ListFunc func(project string, managedZone string) resourceRecordSetsListCallInterface
}

func (m *MockResourceRecordSetsClient) List(project string, managedZone string) resourceRecordSetsListCallInterface {

	return m.ListFunc(project, managedZone)

}
