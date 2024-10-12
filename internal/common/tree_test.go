package common

import (
	"testing"

	. "github.com/onsi/gomega"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func Test_MakeTreeFromDNSRecord(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name      string
		DNSRecord *v1alpha1.DNSRecord
		Verify    func(tree *DNSTreeNode)
	}{
		{
			Name: "geo to tree",
			DNSRecord: &v1alpha1.DNSRecord{
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "app.testdomain.com",
					Endpoints: []*endpoint.Endpoint{
						{
							DNSName:    "app.testdomain.com",
							RecordTTL:  300,
							RecordType: "CNAME",
							Targets: []string{
								"klb.testdomain.com",
							},
						},
						{
							DNSName:    "ip1.testdomain.com",
							RecordTTL:  60,
							RecordType: "A",
							Targets: []string{
								"172.32.200.1",
							},
						},
						{
							DNSName:    "ip2.testdomain.com",
							RecordTTL:  60,
							RecordType: "A",
							Targets: []string{
								"172.32.200.2",
							},
						},
						{
							DNSName:    "eu.klb.testdomain.com",
							RecordTTL:  60,
							RecordType: "CNAME",
							Targets: []string{
								"ip2.testdomain.com",
							},
						},
						{
							DNSName:    "us.klb.testdomain.com",
							RecordTTL:  60,
							RecordType: "CNAME",
							Targets: []string{
								"ip1.testdomain.com",
							},
						},
						{
							DNSName: "klb.testdomain.com",
							ProviderSpecific: []endpoint.ProviderSpecificProperty{
								{
									Name:  "geo-code",
									Value: "*",
								},
							},
							RecordTTL:     300,
							RecordType:    "CNAME",
							SetIdentifier: "",
							Targets: []string{
								"eu.klb.testdomain.com",
							},
						},
						{
							DNSName: "klb.testdomain.com",
							ProviderSpecific: []endpoint.ProviderSpecificProperty{
								{
									Name:  "geo-code",
									Value: "GEO-NA",
								},
							},
							RecordTTL:     300,
							RecordType:    "CNAME",
							SetIdentifier: "",
							Targets: []string{
								"us.klb.testdomain.com",
							},
						},
						{
							DNSName: "klb.testdomain.com",
							ProviderSpecific: []endpoint.ProviderSpecificProperty{
								{
									Name:  "geo-code",
									Value: "GEO-EU",
								},
							},
							RecordTTL:     300,
							RecordType:    "CNAME",
							SetIdentifier: "",
							Targets: []string{
								"eu.klb.testdomain.com",
							},
						},
					},
				},
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("app.testdomain.com"))
				Expect(len(tree.Children)).To(Equal(1))

				child := tree.Children[0]
				Expect(child.Name).To(Equal("klb.testdomain.com"))
				Expect(len(child.Children)).To(Equal(2))

				Expect(len(child.DataSets)).To(Equal(3))
				Expect(child.DataSets[0]).To(Equal(DNSTreeNodeData{
					RecordType:    endpoint.RecordTypeCNAME,
					SetIdentifier: "",
					RecordTTL:     300,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "geo-code",
							Value: "*",
						},
					},
					Targets: []string{
						"eu.klb.testdomain.com",
					},
				}))
				Expect(child.DataSets[1]).To(Equal(DNSTreeNodeData{
					RecordType:    endpoint.RecordTypeCNAME,
					SetIdentifier: "",
					RecordTTL:     300,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "geo-code",
							Value: "GEO-NA",
						},
					},
					Targets: []string{
						"us.klb.testdomain.com",
					},
				}))
				Expect(child.DataSets[2]).To(Equal(DNSTreeNodeData{
					RecordType:    endpoint.RecordTypeCNAME,
					SetIdentifier: "",
					RecordTTL:     300,
					ProviderSpecific: endpoint.ProviderSpecific{
						{
							Name:  "geo-code",
							Value: "GEO-EU",
						},
					},
					Targets: []string{
						"eu.klb.testdomain.com",
					},
				}))

				for _, c := range child.Children {
					Expect([]string{"eu.klb.testdomain.com", "us.klb.testdomain.com"}).To(ContainElement(c.Name))
					Expect(len(c.Children)).To(Equal(1))

					if c.Name == "eu.klb.testdomain.com" {
						Expect(c.Children[0].Name).To(Equal("ip2.testdomain.com"))
						Expect(len(c.Children[0].Children)).To(Equal(1))

						Expect(c.Children[0].Children[0].Name).To(Equal("172.32.200.2"))
						Expect(len(c.Children[0].Children[0].Children)).To(Equal(0))

					} else if c.Name == "us.klb.testdomain.com" {
						Expect(c.Children[0].Name).To(Equal("ip1.testdomain.com"))
						Expect(len(c.Children[0].Children)).To(Equal(1))

						Expect(c.Children[0].Children[0].Name).To(Equal("172.32.200.1"))
						Expect(len(c.Children[0].Children[0].Children)).To(Equal(0))
					}
				}
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			tree := MakeTreeFromDNSRecord(scenario.DNSRecord)
			scenario.Verify(tree)
		})
	}
}

func Test_RemoveNode(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name        string
		Tree        *DNSTreeNode
		RemoveNodes []*DNSTreeNode
		Verify      func(tree *DNSTreeNode)
	}{
		{
			Name: "remove klb",
			Tree: &DNSTreeNode{
				Name: "app.testdomain.com",
				Children: []*DNSTreeNode{
					{
						Name: "klb.testdomain.com",
						Children: []*DNSTreeNode{
							{
								Name: "eu.klb.testdomain.com",
								Children: []*DNSTreeNode{
									{
										Name: "ip1.testdomain.com",
										Children: []*DNSTreeNode{
											{
												Name: "172.32.200.1",
											},
										},
									},
								},
							},
							{
								Name: "us.klb.testdomain.com",
								Children: []*DNSTreeNode{
									{
										Name: "ip2.testdomain.com",
										Children: []*DNSTreeNode{
											{
												Name: "172.32.200.2",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "klb.testdomain.com",
				},
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(len(tree.Children)).To(Equal(0))
			},
		},
		{
			Name: "remove eu",
			Tree: &DNSTreeNode{
				Name: "app.testdomain.com",
				Children: []*DNSTreeNode{
					{
						Name: "klb.testdomain.com",
						Children: []*DNSTreeNode{
							{
								Name: "eu.klb.testdomain.com",
								Children: []*DNSTreeNode{
									{
										Name: "ip1.testdomain.com",
										Children: []*DNSTreeNode{
											{
												Name: "172.32.200.1",
											},
										},
									},
								},
							},
							{
								Name: "us.klb.testdomain.com",
								Children: []*DNSTreeNode{
									{
										Name: "ip2.testdomain.com",
										Children: []*DNSTreeNode{
											{
												Name: "172.32.200.2",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "eu.klb.testdomain.com",
				},
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("app.testdomain.com"))
				Expect(len(tree.Children)).To(Equal(1))

				child := tree.Children[0]
				Expect(child.Name).To(Equal("klb.testdomain.com"))
				Expect(len(child.Children)).To(Equal(1))

				for _, c := range child.Children {
					Expect([]string{"us.klb.testdomain.com"}).To(ContainElement(c.Name))
					Expect(len(c.Children)).To(Equal(1))

					Expect(c.Children[0].Name).To(Equal("ip2.testdomain.com"))
					Expect(len(c.Children[0].Children)).To(Equal(1))

					Expect(c.Children[0].Children[0].Name).To(Equal("172.32.200.2"))
					Expect(len(c.Children[0].Children[0].Children)).To(Equal(0))
				}
			},
		},
		{
			Name: "remove eu",
			Tree: &DNSTreeNode{
				Name: "app.testdomain.com",
				Children: []*DNSTreeNode{
					{
						Name: "klb.testdomain.com",
						Children: []*DNSTreeNode{
							{
								Name: "eu.klb.testdomain.com",
								Children: []*DNSTreeNode{
									{
										Name: "ip1.testdomain.com",
										Children: []*DNSTreeNode{
											{
												Name: "172.32.200.1",
											},
										},
									},
								},
							},
							{
								Name: "us.klb.testdomain.com",
								Children: []*DNSTreeNode{
									{
										Name: "ip2.testdomain.com",
										Children: []*DNSTreeNode{
											{
												Name: "172.32.200.2",
											},
										},
									},
								},
							},
						},
					},
				},
			},
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "eu.klb.testdomain.com",
				},
				{
					Name: "us.klb.testdomain.com",
				},
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("app.testdomain.com"))
				Expect(len(tree.Children)).To(Equal(1))

				child := tree.Children[0]
				Expect(child.Name).To(Equal("klb.testdomain.com"))
				Expect(len(child.Children)).To(Equal(0))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			for _, rn := range scenario.RemoveNodes {
				scenario.Tree.RemoveNode(rn)
			}
			scenario.Verify(scenario.Tree)
		})
	}
}

func Test_ToEndpoints(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name   string
		Record *v1alpha1.DNSRecord
		Verify func(endpoints *[]*endpoint.Endpoint)
	}{
		{
			Name: "geo tree",
			Record: &v1alpha1.DNSRecord{
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "app.testdomain.com",
					Endpoints: []*endpoint.Endpoint{
						{
							DNSName:    "app.testdomain.com",
							RecordTTL:  300,
							RecordType: "CNAME",
							Targets: []string{
								"klb.testdomain.com",
							},
						},
						{
							DNSName:    "ip1.testdomain.com",
							RecordTTL:  60,
							RecordType: "A",
							Targets: []string{
								"172.32.200.1",
							},
						},
						{
							DNSName:    "ip2.testdomain.com",
							RecordTTL:  60,
							RecordType: "A",
							Targets: []string{
								"172.32.200.2",
							},
						},
						{
							DNSName:    "eu.klb.testdomain.com",
							RecordTTL:  60,
							RecordType: "CNAME",
							Targets: []string{
								"ip2.testdomain.com",
							},
						},
						{
							DNSName:    "us.klb.testdomain.com",
							RecordTTL:  60,
							RecordType: "CNAME",
							Targets: []string{
								"ip1.testdomain.com",
							},
						},
						{
							DNSName: "klb.testdomain.com",
							ProviderSpecific: []endpoint.ProviderSpecificProperty{
								{
									Name:  "geo-code",
									Value: "*",
								},
							},
							RecordTTL:     300,
							RecordType:    "CNAME",
							SetIdentifier: "",
							Targets: []string{
								"eu.klb.testdomain.com",
							},
						},
						{
							DNSName: "klb.testdomain.com",
							ProviderSpecific: []endpoint.ProviderSpecificProperty{
								{
									Name:  "geo-code",
									Value: "GEO-NA",
								},
							},
							RecordTTL:     300,
							RecordType:    "CNAME",
							SetIdentifier: "",
							Targets: []string{
								"us.klb.testdomain.com",
							},
						},
						{
							DNSName: "klb.testdomain.com",
							ProviderSpecific: []endpoint.ProviderSpecificProperty{
								{
									Name:  "geo-code",
									Value: "GEO-EU",
								},
							},
							RecordTTL:     300,
							RecordType:    "CNAME",
							SetIdentifier: "",
							Targets: []string{
								"eu.klb.testdomain.com",
							},
						},
					},
				},
			},
			Verify: func(endpoints *[]*endpoint.Endpoint) {
				Expect(len(*endpoints)).To(Equal(8))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName:    "app.testdomain.com",
					RecordTTL:  300,
					RecordType: "CNAME",
					Targets: []string{
						"klb.testdomain.com",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName:    "ip1.testdomain.com",
					RecordTTL:  60,
					RecordType: "A",
					Targets: []string{
						"172.32.200.1",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName:    "ip2.testdomain.com",
					RecordTTL:  60,
					RecordType: "A",
					Targets: []string{
						"172.32.200.2",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName:    "eu.klb.testdomain.com",
					RecordTTL:  60,
					RecordType: "CNAME",
					Targets: []string{
						"ip2.testdomain.com",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName:    "us.klb.testdomain.com",
					RecordTTL:  60,
					RecordType: "CNAME",
					Targets: []string{
						"ip1.testdomain.com",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName: "klb.testdomain.com",
					ProviderSpecific: []endpoint.ProviderSpecificProperty{
						{
							Name:  "geo-code",
							Value: "*",
						},
					},
					RecordTTL:     300,
					RecordType:    "CNAME",
					SetIdentifier: "",
					Targets: []string{
						"eu.klb.testdomain.com",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName: "klb.testdomain.com",
					ProviderSpecific: []endpoint.ProviderSpecificProperty{
						{
							Name:  "geo-code",
							Value: "GEO-NA",
						},
					},
					RecordTTL:     300,
					RecordType:    "CNAME",
					SetIdentifier: "",
					Targets: []string{
						"us.klb.testdomain.com",
					},
				}))
				Expect(*endpoints).To(ContainElement(&endpoint.Endpoint{
					DNSName: "klb.testdomain.com",
					ProviderSpecific: []endpoint.ProviderSpecificProperty{
						{
							Name:  "geo-code",
							Value: "GEO-EU",
						},
					},
					RecordTTL:     300,
					RecordType:    "CNAME",
					SetIdentifier: "",
					Targets: []string{
						"eu.klb.testdomain.com",
					},
				}))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			endpoints := &[]*endpoint.Endpoint{}
			tree := MakeTreeFromDNSRecord(scenario.Record)
			ToEndpoints(tree, endpoints)
			scenario.Verify(endpoints)
		})
	}
}
