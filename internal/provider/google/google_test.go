//go:build unit

package google

import (
	"context"
	"fmt"
	"sort"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/google/go-cmp/cmp"
	dnsv1 "google.golang.org/api/dns/v1"

	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/internal/provider"
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
			got, err := g.toManagedZoneOutput(context.Background(), tt.args.mz)
			if (err != nil) != tt.wantErr {
				t.Errorf("GoogleDNSProvider.toManagedZoneOutput() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("GoogleDNSProvider.toManagedZoneOutput() (-want +got):\n%s", diff)
			}
		})
	}
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

func sorted(endpoints []*externaldnsendpoint.Endpoint) {
	sort.Slice(endpoints, func(i, j int) bool {
		return endpoints[i].DNSName < endpoints[j].DNSName
	})
}

func Test_endpointsFromResourceRecordSets(t *testing.T) {
	type args struct {
		resourceRecordSets []*dnsv1.ResourceRecordSet
	}
	tests := []struct {
		name string
		args args
		want []*externaldnsendpoint.Endpoint
	}{
		{
			name: "test CNAME with geo and multiple targets",
			args: args{
				resourceRecordSets: []*dnsv1.ResourceRecordSet{
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
									{
										Location: "us-east1",
										Rrdatas: []string{
											"us-east1.lb-4ej5le.unittest.google.hcpapps.net.",
										},
									},
								},
							},
						},
						Ttl:  300,
						Type: "CNAME",
					},
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "lb-4ej5le.unittest.google.hcpapps.net",
					RecordType: "CNAME",
					RecordTTL:  300,
					Labels:     externaldnsendpoint.Labels{},
					Targets: externaldnsendpoint.Targets{
						"europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
						"us-east1.lb-4ej5le.unittest.google.hcpapps.net",
					},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "geo",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "europe-west1",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "us-east1.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "us-east1",
						},
					},
				},
			},
		},
		{
			name: "test CNAME with weights and multiple targets",
			args: args{
				resourceRecordSets: []*dnsv1.ResourceRecordSet{
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
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "default.lb-4ej5le.unittest.google.hcpapps.net",
					RecordType: "CNAME",
					RecordTTL:  60,
					Labels:     externaldnsendpoint.Labels{},
					Targets: externaldnsendpoint.Targets{
						"2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						"lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
					},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "weighted",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "120",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "120",
						},
					},
				},
			},
		},
		{
			name: "test A record",
			args: args{
				resourceRecordSets: []*dnsv1.ResourceRecordSet{
					{
						Name: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net.",
						Rrdatas: []string{
							"0.0.0.0",
						},
						Ttl:  60,
						Type: "A",
					},
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
					RecordType: "A",
					RecordTTL:  60,
					Labels:     externaldnsendpoint.Labels{},
					Targets: externaldnsendpoint.Targets{
						"0.0.0.0",
					},
					SetIdentifier: "",
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointsFromResourceRecordSets(tt.args.resourceRecordSets)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("endpointsFromResourceRecordSets (-want +got):\n%s", diff)
			}

		})
	}
}

func Test_endpointsToGoogleFormat(t *testing.T) {
	type args struct {
		endpoints []*externaldnsendpoint.Endpoint
	}
	tests := []struct {
		name string
		args args
		want []*externaldnsendpoint.Endpoint
	}{
		{
			name: "multiple weighted records",
			args: args{
				endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName:       "default.lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						RecordTTL:     60,
						Targets: externaldnsendpoint.Targets{
							"2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						},
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
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
						Targets: externaldnsendpoint.Targets{
							"lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
						},
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "weight",
								Value: "120",
							},
						},
					},
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "default.lb-4ej5le.unittest.google.hcpapps.net",
					RecordType: "CNAME",
					RecordTTL:  60,
					Targets: externaldnsendpoint.Targets{
						"2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
						"lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
					},
					Labels: externaldnsendpoint.Labels{},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "weighted",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "2c71gf.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "120",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "lrnse3.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "120",
						},
					},
				},
			},
		},
		{
			name: "multiple geo targets",
			args: args{
				endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName:       "lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "europe-west1",
						Targets: []string{
							"europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
						},
						RecordTTL: 300,
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "europe-west1",
							},
						},
					},
					{
						DNSName:       "lb-4ej5le.unittest.google.hcpapps.net",
						RecordType:    "CNAME",
						SetIdentifier: "us-east1",
						Targets: []string{
							"us-east1.lb-4ej5le.unittest.google.hcpapps.net",
						},
						RecordTTL: 300,
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "us-east1",
							},
						},
					},
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "lb-4ej5le.unittest.google.hcpapps.net",
					RecordType: "CNAME",
					RecordTTL:  300,
					Targets: externaldnsendpoint.Targets{
						"europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
						"us-east1.lb-4ej5le.unittest.google.hcpapps.net",
					},
					Labels: externaldnsendpoint.Labels{},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "geo",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "europe-west1.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "europe-west1",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "us-east1.lb-4ej5le.unittest.google.hcpapps.net",
							Value: "us-east1",
						},
					},
				},
			},
		},
		{
			name: "multiple geo and weighted targets",
			args: args{
				endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName:    "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "A",
						Targets: []string{
							"172.18.200.1",
						},
						RecordTTL: 60,
					},
					{
						DNSName:    "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "A",
						Targets: []string{
							"172.18.200.2",
						},
						RecordTTL: 60,
					},
					{
						DNSName:    "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "A",
						Targets: []string{
							"172.18.200.3",
						},
						RecordTTL: 60,
					},
					{
						DNSName:    "loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL: 300,
					},
					{
						DNSName:    "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL:     60,
						SetIdentifier: "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "weight",
								Value: "200",
							},
						},
					},
					{
						DNSName:    "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL:     60,
						SetIdentifier: "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "weight",
								Value: "100",
							},
						},
					},
					{
						DNSName:    "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL:     60,
						SetIdentifier: "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "weight",
								Value: "100",
							},
						},
					},
					{
						DNSName:    "lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL:     300,
						SetIdentifier: "europe-west1",
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "europe-west1",
							},
						},
					},
					{
						DNSName:    "lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL:     300,
						SetIdentifier: "us-east1",
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "us-east1",
							},
						},
					},
					{
						DNSName:    "lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL:     300,
						SetIdentifier: "default",
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "geo-code",
								Value: "*",
							},
						},
					},
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "A",
					Targets: []string{
						"172.18.200.1",
					},
					RecordTTL: 60,
				},
				{
					DNSName:    "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "A",
					Targets: []string{
						"172.18.200.2",
					},
					RecordTTL: 60,
				},
				{
					DNSName:    "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "A",
					Targets: []string{
						"172.18.200.3",
					},
					RecordTTL: 60,
				},
				{
					DNSName:    "loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 300,
				},
				{
					DNSName:    "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						"cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 60,
					Labels:    externaldnsendpoint.Labels{},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "weighted",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "200",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "100",
						},
					},
				},
				{
					DNSName:    "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 60,
					Labels:    externaldnsendpoint.Labels{},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "weighted",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "100",
						},
					},
				},
				{
					DNSName:    "lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						"us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 300,
					Labels:    externaldnsendpoint.Labels{},
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "geo",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "europe-west1",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "us-east1",
						},
					},
				},
			},
		},
		{
			name: "multiple geo and weighted targets already in google format",
			args: args{
				endpoints: []*externaldnsendpoint.Endpoint{
					{
						DNSName:    "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "A",
						Targets: []string{
							"172.18.200.1",
						},
						RecordTTL: 60,
					},
					{
						DNSName:    "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "A",
						Targets: []string{
							"172.18.200.2",
						},
						RecordTTL: 60,
					},
					{
						DNSName:    "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "A",
						Targets: []string{
							"172.18.200.3",
						},
						RecordTTL: 60,
					},
					{
						DNSName:    "loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL: 300,
					},
					{
						DNSName:    "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							"cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL: 60,
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "routingpolicy",
								Value: "weighted",
							},
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
								Value: "200",
							},
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
								Value: "100",
							},
						},
					},
					{
						DNSName:    "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL: 60,
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "routingpolicy",
								Value: "weighted",
							},
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
								Value: "100",
							},
						},
					},
					{
						DNSName:    "lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						RecordType: "CNAME",
						Targets: []string{
							"europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							"us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						},
						RecordTTL: 300,
						ProviderSpecific: externaldnsendpoint.ProviderSpecific{
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "routingpolicy",
								Value: "geo",
							},
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
								Value: "europe-west1",
							},
							externaldnsendpoint.ProviderSpecificProperty{
								Name:  "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
								Value: "us-east1",
							},
						},
					},
				},
			},
			want: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "A",
					Targets: []string{
						"172.18.200.1",
					},
					RecordTTL: 60,
				},
				{
					DNSName:    "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "A",
					Targets: []string{
						"172.18.200.2",
					},
					RecordTTL: 60,
				},
				{
					DNSName:    "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "A",
					Targets: []string{
						"172.18.200.3",
					},
					RecordTTL: 60,
				},
				{
					DNSName:    "loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 300,
				},
				{
					DNSName:    "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						"cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 60,
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "weighted",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "cluster1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "200",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "cluster2.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "100",
						},
					},
				},
				{
					DNSName:    "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 60,
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "weighted",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "cluster3.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "100",
						},
					},
				},
				{
					DNSName:    "lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					RecordType: "CNAME",
					Targets: []string{
						"europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
						"us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
					},
					RecordTTL: 300,
					ProviderSpecific: externaldnsendpoint.ProviderSpecific{
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "routingpolicy",
							Value: "geo",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "europe-west1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "europe-west1",
						},
						externaldnsendpoint.ProviderSpecificProperty{
							Name:  "us-east1.lb-gw1-ns1.loadbalanced.mn.google.hcpapps.net",
							Value: "us-east1",
						},
					},
				},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := endpointsToGoogleFormat(tt.args.endpoints)
			sorted(got)
			sorted(tt.want)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("endpointsToGoogleFormat (-want +got):\n%s", diff)
			}
		})
	}
}
