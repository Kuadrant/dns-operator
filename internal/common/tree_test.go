package common

import (
	"testing"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	"k8s.io/utils/ptr"
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

func Test_GetLeafsTargets(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name      string
		DNSRecord *v1alpha1.DNSRecord
		Verify    func(targets []string)
	}{
		{
			Name: "geo targets",
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
			Verify: func(targets []string) {
				Expect(targets).To(HaveLen(2))
				for _, target := range targets {
					Expect(target).To(Or(
						Equal("172.32.200.1"),
						Equal("172.32.200.2")))
				}
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			targets := make([]string, 0)
			GetLeafsTargets(MakeTreeFromDNSRecord(scenario.DNSRecord), &targets)

			// verify for passed in targets
			scenario.Verify(targets)

			// verify for returned targets
			scenario.Verify(*GetLeafsTargets(MakeTreeFromDNSRecord(scenario.DNSRecord), ptr.To([]string{})))
		})
	}
}

func Test_RemoveNode(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name            string
		Tree            *DNSTreeNode
		RemoveNodes     []*DNSTreeNode
		VerifyTree      func(tree *DNSTreeNode)
		VerifyEndpoints func(endpoints *[]*endpoint.Endpoint)
	}{
		{
			Name: "remove eu / prune dead branch front",
			Tree: getTestTree(),
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "172.32.200.1",
				},
			},
			VerifyTree: func(tree *DNSTreeNode) {
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
			VerifyEndpoints: func(endpoints *[]*endpoint.Endpoint) {
				Expect(*endpoints).To(HaveLen(4))
				Expect(*endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("app.testdomain.com"),
						"Targets": ConsistOf("klb.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("klb.testdomain.com"),
						"Targets": ConsistOf("us.klb.testdomain.com"),
						"ProviderSpecific": ConsistOf(
							MatchFields(IgnoreExtras, Fields{
								"Name":  Equal("geo-code"),
								"Value": Equal("GEO-NA"),
							})),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("us.klb.testdomain.com"),
						"Targets": ConsistOf("ip2.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("ip2.testdomain.com"),
						"Targets": ConsistOf("172.32.200.2"),
					})),
				))
			},
		},
		{
			Name: "remove us / prune dead branch back",
			Tree: getTestTree(),
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "172.32.200.2",
				},
			},
			VerifyTree: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("app.testdomain.com"))
				Expect(len(tree.Children)).To(Equal(1))

				child := tree.Children[0]
				Expect(child.Name).To(Equal("klb.testdomain.com"))
				Expect(len(child.Children)).To(Equal(1))

				grandChild := child.Children[0]
				Expect(grandChild.Name).To(Equal("eu.klb.testdomain.com"))
			},
			VerifyEndpoints: func(endpoints *[]*endpoint.Endpoint) {
				Expect(*endpoints).To(HaveLen(5))
				Expect(*endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("app.testdomain.com"),
						"Targets": ConsistOf("klb.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("klb.testdomain.com"),
						"Targets": ConsistOf("eu.klb.testdomain.com"),
						"ProviderSpecific": ConsistOf(
							MatchFields(IgnoreExtras, Fields{
								"Name":  Equal("geo-code"),
								"Value": Equal("GEO-EU"),
							})),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("klb.testdomain.com"),
						"Targets": ConsistOf("eu.klb.testdomain.com"),
						"ProviderSpecific": ConsistOf(
							MatchFields(IgnoreExtras, Fields{
								"Name":  Equal("geo-code"),
								"Value": Equal("*"),
							})),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("eu.klb.testdomain.com"),
						"Targets": ConsistOf("ip1.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("ip1.testdomain.com"),
						"Targets": ConsistOf("172.32.200.1"),
					})),
				))
			},
		},
		{
			Name: "remove eu and us / prune multiple dead branches",
			Tree: getTestTree(),
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "172.32.200.1",
				},
				{
					Name: "172.32.200.2",
				},
			},
			VerifyTree: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("app.testdomain.com"))
				Expect(len(tree.Children)).To(Equal(0))
			},
			VerifyEndpoints: func(endpoints *[]*endpoint.Endpoint) {
				Expect(*endpoints).To(HaveLen(0))
			},
		},
		{
			Name: "remove IP from a leaf",
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
											{
												Name: "172.32.200.2",
											},
										},
										DataSets: []DNSTreeNodeData{
											{
												RecordType: "A",
												Targets: []string{
													"172.32.200.1",
													"172.32.200.2",
												},
											},
										},
									},
								},
								DataSets: []DNSTreeNodeData{
									{
										RecordType: "CNAME",
										Targets: []string{
											"ip1.testdomain.com",
										},
									},
								},
							},
						},
						DataSets: []DNSTreeNodeData{
							{
								RecordType: "CNAME",
								Targets: []string{
									"eu.klb.testdomain.com",
								},
							},
						},
					},
				},
				DataSets: []DNSTreeNodeData{
					{
						RecordType: "CNAME",
						Targets: []string{
							"klb.testdomain.com",
						},
					},
				},
			},
			RemoveNodes: []*DNSTreeNode{
				{
					Name: "172.32.200.2",
				},
			},
			VerifyTree: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("app.testdomain.com"))
				Expect(len(tree.Children)).To(Equal(1))

				Expect(tree.Children[0].Name).To(Equal("klb.testdomain.com"))
				Expect(len(tree.Children[0].Children)).To(Equal(1))

				Expect(tree.Children[0].Children[0].Name).To(Equal("eu.klb.testdomain.com"))
				Expect(len(tree.Children[0].Children[0].Children)).To(Equal(1))

				ipNode := tree.Children[0].Children[0].Children[0]
				Expect(ipNode.Name).To(Equal("ip1.testdomain.com"))
				Expect(len(ipNode.Children)).To(Equal(1))
				Expect(ipNode.Children[0].Name).To(Equal("172.32.200.1"))
				Expect(len(ipNode.DataSets)).To(Equal(1))
				Expect(len(ipNode.DataSets[0].Targets)).To(Equal(1))
				Expect(ipNode.DataSets[0].Targets[0]).To(Equal("172.32.200.1"))
			},
			VerifyEndpoints: func(endpoints *[]*endpoint.Endpoint) {
				Expect(*endpoints).To(HaveLen(4))
				Expect(*endpoints).To(ConsistOf(
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("app.testdomain.com"),
						"Targets": ConsistOf("klb.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("klb.testdomain.com"),
						"Targets": ConsistOf("eu.klb.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("eu.klb.testdomain.com"),
						"Targets": ConsistOf("ip1.testdomain.com"),
					})),
					PointTo(MatchFields(IgnoreExtras, Fields{
						"DNSName": Equal("ip1.testdomain.com"),
						"Targets": ConsistOf("172.32.200.1"),
					})),
				))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			for _, rn := range scenario.RemoveNodes {
				scenario.Tree.RemoveNode(rn)
			}
			scenario.VerifyTree(scenario.Tree)
			scenario.VerifyEndpoints(ToEndpoints(scenario.Tree, ptr.To([]*endpoint.Endpoint{})))
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

func getTestTree() *DNSTreeNode {
	return &DNSTreeNode{
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
								DataSets: []DNSTreeNodeData{
									{
										RecordType: "A",
										Targets: []string{
											"172.32.200.1",
										},
									},
								},
							},
						},
						DataSets: []DNSTreeNodeData{
							{
								RecordType: "CNAME",
								Targets: []string{
									"ip1.testdomain.com",
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
								DataSets: []DNSTreeNodeData{
									{
										RecordType: "A",
										Targets: []string{
											"172.32.200.2",
										},
									},
								},
							},
						},
						DataSets: []DNSTreeNodeData{
							{
								RecordType: "CNAME",
								Targets: []string{
									"ip2.testdomain.com",
								},
							},
						},
					},
				},
				DataSets: []DNSTreeNodeData{
					{
						RecordType: "CNAME",
						ProviderSpecific: endpoint.ProviderSpecific{
							{
								Name:  "geo-code",
								Value: "*",
							},
						},
						Targets: []string{
							"eu.klb.testdomain.com",
						},
					},
					{
						RecordType: "CNAME",
						ProviderSpecific: endpoint.ProviderSpecific{
							{
								Name:  "geo-code",
								Value: "GEO-NA",
							},
						},
						Targets: []string{
							"us.klb.testdomain.com",
						},
					},
					{
						RecordType: "CNAME",
						ProviderSpecific: endpoint.ProviderSpecific{
							{
								Name:  "geo-code",
								Value: "GEO-EU",
							},
						},
						Targets: []string{
							"eu.klb.testdomain.com",
						},
					},
				},
			},
		},
		DataSets: []DNSTreeNodeData{
			{
				RecordType: "CNAME",
				Targets: []string{
					"klb.testdomain.com",
				},
			},
		},
	}
}
