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
				Expect(tree.Parent).To(BeNil())

				klbHost := tree.Children[0]
				Expect(klbHost.Name).To(Equal("klb.testdomain.com"))
				Expect(len(klbHost.Children)).To(Equal(2))
				Expect(klbHost.Parent).To(Equal(tree))

				Expect(len(klbHost.DataSets)).To(Equal(3))
				Expect(klbHost.DataSets[0]).To(Equal(&DNSTreeNodeData{
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
				Expect(klbHost.DataSets[1]).To(Equal(&DNSTreeNodeData{
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
				Expect(klbHost.DataSets[2]).To(Equal(&DNSTreeNodeData{
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

				for _, c := range klbHost.Children {
					Expect(c.Parent).To(Equal(klbHost))
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
		{
			Name: "zone endpoints to tree",
			DNSRecord: &v1alpha1.DNSRecord{
				Spec: v1alpha1.DNSRecordSpec{
					RootHost: "test.domain.com",
					Endpoints: []*endpoint.Endpoint{
						{
							DNSName: "domain.com",
							Targets: []string{
								"ns-332.awsdns-41.com",
								"ns-1602.awsdns-08.co.uk",
								"ns-1323.awsdns-37.org",
								"ns-1010.awsdns-62.net",
							},
							RecordType: "NS",
							RecordTTL:  172800,
						},
						{
							DNSName: "test.domain.com",
							Targets: []string{
								"klb.test.domain.com",
							},
							RecordType: "CNAME",
							RecordTTL:  300,
							Labels: endpoint.Labels{
								"owner": "2d357tbm&&6rvin4zy&&roql6iqy",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
							},
						},

						{
							DNSName: "klb.test.domain.com",
							Targets: []string{
								"ie.klb.test.domain.com",
							},
							RecordType:    "CNAME",
							SetIdentifier: "IE",
							RecordTTL:     300,
							Labels: endpoint.Labels{
								"owner": "2d357tbm&&6rvin4zy",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
								endpoint.ProviderSpecificProperty{
									Name:  "aws/geolocation-country-code",
									Value: "IE",
								},
							},
						},
						{
							DNSName: "klb.test.domain.com",
							Targets: []string{
								"us.klb.test.domain.com",
							},
							RecordType:    "CNAME",
							SetIdentifier: "US",
							RecordTTL:     300,
							Labels: endpoint.Labels{
								"owner": "roql6iqy",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
								endpoint.ProviderSpecificProperty{
									Name:  "aws/geolocation-country-code",
									Value: "US",
								},
							},
						},
						{
							DNSName: "klb.test.domain.com",
							Targets: []string{
								"ie.klb.test.domain.com",
							},
							RecordType:    "CNAME",
							SetIdentifier: "default",
							RecordTTL:     300,
							Labels: endpoint.Labels{
								"owner": "2d357tbm&&6rvin4zy",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
								endpoint.ProviderSpecificProperty{
									Name:  "aws/geolocation-country-code",
									Value: "*",
								},
							},
						},
						{
							DNSName: "cluster1-gw1-ns1.klb.test.domain.com",
							Targets: []string{
								"172.18.200.1",
							},
							RecordType: "A",
							RecordTTL:  60,
							Labels: endpoint.Labels{
								"owner": "6rvin4zy",
							},
						},
						{
							DNSName: "cluster2-gw1-ns1.klb.test.domain.com",
							Targets: []string{
								"172.18.200.2",
							},
							RecordType: "A",
							RecordTTL:  60,
							Labels: endpoint.Labels{
								"owner": "2d357tbm",
							},
						},
						{
							DNSName: "cluster3-gw1-ns1.klb.test.domain.com",
							Targets: []string{
								"172.18.200.3",
							},
							RecordType: "A",
							RecordTTL:  60,
							Labels: endpoint.Labels{
								"owner": "roql6iqy",
							},
						},
						{
							DNSName: "ie.klb.test.domain.com",
							Targets: []string{
								"cluster1-gw1-ns1.klb.test.domain.com",
							},
							RecordType:    "CNAME",
							SetIdentifier: "cluster1-gw1-ns1.klb.test.domain.com",
							RecordTTL:     60,
							Labels: endpoint.Labels{
								"owner": "6rvin4zy",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
								{
									Name:  "aws/weight",
									Value: "200",
								},
							},
						},
						{
							DNSName: "ie.klb.test.domain.com",
							Targets: []string{
								"cluster2-gw1-ns1.klb.test.domain.com",
							},
							RecordType:    "CNAME",
							SetIdentifier: "cluster2-gw1-ns1.klb.test.domain.com",
							RecordTTL:     60,
							Labels: endpoint.Labels{
								"owner": "2d357tbm",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
								endpoint.ProviderSpecificProperty{
									Name:  "aws/weight",
									Value: "100",
								},
							},
						},
						{
							DNSName: "us.klb.test.domain.com",
							Targets: []string{
								"cluster3-gw1-ns1.klb.test.domain.com",
							},
							RecordType:    "CNAME",
							SetIdentifier: "cluster3-gw1-ns1.klb.test.domain.com",
							RecordTTL:     60,
							Labels: endpoint.Labels{
								"owner": "roql6iqy",
							},
							ProviderSpecific: endpoint.ProviderSpecific{
								endpoint.ProviderSpecificProperty{
									Name:  "alias",
									Value: "false",
								},
								{
									Name:  "aws/weight",
									Value: "100",
								},
							},
						},
					},
				},
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("test.domain.com"))
				Expect(len(tree.Children)).To(Equal(1))

				Expect(tree.Children[0].Name).To(Equal("klb.test.domain.com"))
				Expect(len(tree.Children[0].Children)).To(Equal(2))

				var ieNode, usNode *DNSTreeNode

				if tree.Children[0].Children[0].Name == "ie.klb.test.domain.com" {
					ieNode = tree.Children[0].Children[0]
					usNode = tree.Children[0].Children[1]
				} else {
					ieNode = tree.Children[0].Children[1]
					usNode = tree.Children[0].Children[0]
				}
				Expect(ieNode.Name).To(Equal("ie.klb.test.domain.com"))
				Expect(len(ieNode.Children)).To(Equal(2))

				Expect(ieNode.Children[0].Name).To(Equal("cluster1-gw1-ns1.klb.test.domain.com"))
				Expect(len(ieNode.Children[0].Children)).To(Equal(1))
				Expect(ieNode.Children[0].Children[0].Name).To(Equal("172.18.200.1"))

				Expect(ieNode.Children[1].Name).To(Equal("cluster2-gw1-ns1.klb.test.domain.com"))
				Expect(len(ieNode.Children[1].Children)).To(Equal(1))
				Expect(ieNode.Children[1].Children[0].Name).To(Equal("172.18.200.2"))

				Expect(usNode.Name).To(Equal("us.klb.test.domain.com"))
				Expect(len(usNode.Children[0].Children)).To(Equal(1))
				Expect(len(usNode.Children)).To(Equal(1))

				Expect(usNode.Children[0].Name).To(Equal("cluster3-gw1-ns1.klb.test.domain.com"))
				Expect(usNode.Children[0].Children[0].Name).To(Equal("172.18.200.3"))
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
										DataSets: []*DNSTreeNodeData{
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
								DataSets: []*DNSTreeNodeData{
									{
										RecordType: "CNAME",
										Targets: []string{
											"ip1.testdomain.com",
										},
									},
								},
							},
						},
						DataSets: []*DNSTreeNodeData{
							{
								RecordType: "CNAME",
								Targets: []string{
									"eu.klb.testdomain.com",
								},
							},
						},
					},
				},
				DataSets: []*DNSTreeNodeData{
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

func Test_CopyLabels(t *testing.T) {
	RegisterTestingT(t)
	scenarios := []struct {
		Name   string
		From   func() *DNSTreeNode
		To     func() *DNSTreeNode
		Verify func(to *DNSTreeNode)
	}{
		{
			Name: "Copy soft label",
			From: func() *DNSTreeNode {
				root := &DNSTreeNode{
					Name: "test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{
								"klb.test.domain.com",
							},
						},
					},
				}
				klbHost := &DNSTreeNode{
					Name: "klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"ie.klb.test.domain.com"},
							Labels:  map[string]string{"stop_soft_delete": "true"},
						},
						{
							Targets: []string{"ie.klb.test.domain.com"},
							Labels:  map[string]string{"stop_soft_delete": "true"},
						},
					},
				}
				ieHost := &DNSTreeNode{
					Name: "ie.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"luster1-gw1-ns1.klb.test.domain.com"},
						},
					},
				}
				cluster1Host := &DNSTreeNode{
					Name: "cluster1-gw1-ns1.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"172.18.200.1"},
							Labels: map[string]string{
								"soft_delete": "true",
							},
						},
					},
				}
				AddChild(cluster1Host, &DNSTreeNode{Name: "172.18.200.1"})
				AddChild(ieHost, cluster1Host)
				AddChild(klbHost, ieHost)
				AddChild(root, klbHost)
				return root
			},
			To: func() *DNSTreeNode {

				root := &DNSTreeNode{
					Name: "test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{
								"klb.test.domain.com",
							},
							Labels: map[string]string{
								"owner": "10ancflq&&1lsnsc8i&&365k1r5i",
							},
						},
					},
				}

				klbHost := &DNSTreeNode{
					Name: "klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{
								"ie.klb.test.domain.com",
							},
							Labels: map[string]string{
								"owner":            "10ancflq&&1lsnsc8i",
								"stop_soft_delete": "true",
							},
						},
						{
							Targets: []string{
								"us.klb.test.domain.com",
							},
							Labels: map[string]string{
								"owner":            "365k1r5i",
								"stop_soft_delete": "true",
							},
						},
						{
							Targets: []string{
								"ie.klb.test.domain.com",
							},
							Labels: map[string]string{
								"owner":            "10ancflq&&1lsnsc8i",
								"stop_soft_delete": "true",
							},
						},
					},
				}
				ieHost := &DNSTreeNode{
					Name: "ie.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"cluster1-gw1-ns1.klb.test.domain.com"},
							Labels: map[string]string{
								"owner": "10ancflq",
							},
						},
						{
							Targets: []string{"cluster2-gw1-ns1.klb.test.domain.com"},
							Labels: map[string]string{
								"owner": "1lsnsc8i",
							},
						},
					},
				}
				cluster1Host := &DNSTreeNode{
					Name: "cluster1-gw1-ns1.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"172.18.200.1"},
							Labels: map[string]string{
								"owner": "10ancflq",
							},
						},
					},
				}
				cluster2Host := &DNSTreeNode{
					Name: "cluster2-gw1-ns1.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"172.18.200.2"},
							Labels: map[string]string{
								"owner": "1lsnsc8i",
							},
						},
					},
				}
				usHost := &DNSTreeNode{
					Name: "us.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"cluster3-gw1-ns1.klb.test.domain.com"},
							Labels: map[string]string{
								"owner": "365k1r5i",
							},
						},
					},
				}
				cluster3Host := &DNSTreeNode{
					Name: "cluster3-gw1-ns1.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"172.18.200.3"},
							Labels: map[string]string{
								"owner": "365k1r5i",
							},
						},
					},
				}
				AddChild(cluster1Host, &DNSTreeNode{
					Name: "172.18.200.1",
				})
				AddChild(ieHost, cluster1Host)
				AddChild(cluster2Host, &DNSTreeNode{
					Name: "172.18.200.2",
				})
				AddChild(ieHost, cluster2Host)

				AddChild(cluster3Host, &DNSTreeNode{
					Name: "172.18.200.3",
				})
				AddChild(usHost, cluster3Host)

				AddChild(klbHost, ieHost)
				AddChild(klbHost, usHost)
				AddChild(root, klbHost)
				return root
			},
			Verify: func(to *DNSTreeNode) {
				cluster1 := FindNode(to, "cluster1-gw1-ns1.klb.test.domain.com")
				cluster2 := FindNode(to, "cluster2-gw1-ns1.klb.test.domain.com")
				cluster3 := FindNode(to, "cluster3-gw1-ns1.klb.test.domain.com")

				Expect(len(cluster1.DataSets)).To(Equal(1))
				Expect(len(cluster2.DataSets)).To(Equal(1))
				Expect(len(cluster3.DataSets)).To(Equal(1))

				Expect(cluster1.DataSets[0].Labels).To(HaveKey("soft_delete"))
				Expect(cluster2.DataSets[0].Labels).NotTo(HaveKey("soft_delete"))
				Expect(cluster3.DataSets[0].Labels).NotTo(HaveKey("soft_delete"))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			from := scenario.From()
			to := scenario.To()
			CopyLabel("soft_delete", from, to)
			scenario.Verify(to)
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

func Test_AddLabelToBranch(t *testing.T) {
	RegisterTestingT(t)

	PropagateFlag := "propagate_flag"
	scenarios := []struct {
		Name   string
		Branch string
		Label  string
		Value  string
		Node   *DNSTreeNode
		Verify func(node *DNSTreeNode)
	}{
		{
			Name:   "Label is applied to passed in object",
			Branch: "branch",
			Label:  PropagateFlag,
			Value:  "true",
			Node: &DNSTreeNode{
				Name: "node",
				DataSets: []*DNSTreeNodeData{
					{
						Targets: []string{"branch"},
					},
				},
			},
			Verify: func(node *DNSTreeNode) {
				Expect(len(node.DataSets)).To(Equal(1))
				Expect(node.DataSets[0].Labels).NotTo(BeNil())
				Expect(node.DataSets[0].Labels).To(HaveKey(PropagateFlag))
			},
		},
	}
	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			AddLabelToBranch(scenario.Node, scenario.Branch, scenario.Label, scenario.Value)
			scenario.Verify(scenario.Node)
		})
	}
}

func Test_PropagateLabel(t *testing.T) {
	RegisterTestingT(t)

	PropagateFlag := "propagate_flag"
	StopPropagateFlag := "stop_propagate_flag"
	scenarios := []struct {
		Name   string
		Tree   func() *DNSTreeNode
		Verify func(tree *DNSTreeNode)
	}{
		{
			Name: "leaf propagates through multiple matching datasets",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{
					Name: "test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"klb.test.domain.com"},
						},
					},
				}

				klbHost := &DNSTreeNode{
					Name: "klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"ie.klb.test.domain.com"},
							Labels:  map[string]string{StopPropagateFlag: "true"},
						},
						{
							Targets: []string{"ie.klb.test.domain.com"},
							Labels:  map[string]string{StopPropagateFlag: "true"},
						},
					},
				}
				ieHost := &DNSTreeNode{
					Name: "ie.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"cluster1-gw1-ns1.klb.test.domain.com"},
						},
					},
				}
				clusterHost := &DNSTreeNode{
					Name: "cluster1-gw1-ns1.klb.test.domain.com",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"172.18.200.1"},
							Labels:  map[string]string{PropagateFlag: "true"},
						},
					},
				}
				AddChild(clusterHost, &DNSTreeNode{Name: "172.18.200.1"})
				AddChild(ieHost, clusterHost)
				AddChild(klbHost, ieHost)
				AddChild(root, klbHost)
				return root
			},
			Verify: func(root *DNSTreeNode) {
				Expect(len(root.Children)).To(Equal(1))

				Expect(len(root.DataSets)).To(Equal(1))
				Expect(root.DataSets[0].Labels).To(HaveKey(PropagateFlag))

				klbHost := root.Children[0]
				Expect(len(klbHost.DataSets)).To(Equal(2))

				Expect(klbHost.DataSets[0].Labels).To(HaveKey(PropagateFlag))
				Expect(klbHost.DataSets[1].Labels).To(HaveKey(PropagateFlag))

				Expect(len(klbHost.Children)).To(Equal(1))

				ieHost := klbHost.Children[0]
				Expect(len(ieHost.Children)).To(Equal(1))
				Expect(len(ieHost.DataSets)).To(Equal(1))
				Expect(ieHost.DataSets[0].Labels).To(HaveKey(PropagateFlag))

				clusterHost := ieHost.Children[0]
				Expect(len(clusterHost.DataSets)).To(Equal(1))
				Expect(clusterHost.DataSets[0].Labels).To(HaveKey(PropagateFlag))

				Expect(len(clusterHost.Children)).To(Equal(1))

				leafHost := clusterHost.Children[0]
				Expect(len(leafHost.DataSets)).To(Equal(1))
				Expect(leafHost.DataSets[0].Labels).To(HaveKey(PropagateFlag))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			tree := scenario.Tree()
			PropagateLabel(tree, PropagateFlag, "true")
			scenario.Verify(tree)
		})
	}
}

func Test_PropagateStoppableLabel(t *testing.T) {
	RegisterTestingT(t)
	/*
		legend:
			R: root node
			X: node with stop label
			D: node with delete label
			L: leaf node
			O: other node
	*/
	PropagateFlag := "propagate_flag"
	StopPropagateFlag := "stop_propagate_flag"
	scenarios := []struct {
		Name   string
		Tree   func() *DNSTreeNode
		Verify func(tree *DNSTreeNode)
	}{
		/*

			## Test no delete labels = no changes
			in:
			R:
				O1:
					L
				O2:
					L
			out:
			R:
				O1:
					L
				O2:
					L
		*/
		{
			Name: "Test no delete labels = no changes",
			Tree: func() *DNSTreeNode {
				return &DNSTreeNode{
					Name: "root",
					Children: []*DNSTreeNode{
						{
							Name: "other1",
							Children: []*DNSTreeNode{
								{
									Name: "leaf1",
								},
							},
						},
						{
							Name: "other2",
							Children: []*DNSTreeNode{
								{
									Name: "leaf2",
								},
							},
						},
					},
				}
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("root"))
				Expect(len(tree.Children)).To(Equal(2))

				Expect(tree.Children[0].Name).To(Equal("other1"))
				Expect(len(tree.Children[0].Children)).To(Equal(1))

				Expect(tree.Children[1].Name).To(Equal("other2"))
				Expect(len(tree.Children[1].Children)).To(Equal(1))

				Expect(tree.Children[0].Children[0].Name).To(Equal("leaf1"))
				Expect(len(tree.Children[0].Children[0].Children)).To(Equal(0))

				Expect(tree.Children[1].Children[0].Name).To(Equal("leaf2"))
				Expect(len(tree.Children[1].Children[0].Children)).To(Equal(0))
			},
		},

		/*
			## Test Delete label persists
			in:
			R:
				O1:
					L
					D
				O2:
					L
			out:
			R:
				O1:
					L
					D
				O2:
					L
		*/
		{
			Name: "Test delete label persists",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{Name: "root"}
				other1 := &DNSTreeNode{
					Name: "other1",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"leaf2",
							},
						},
					},
				}
				AddChild(other1, &DNSTreeNode{
					Name: "leaf1",
				})
				AddChild(other1, &DNSTreeNode{
					Name: "leaf2",
				})
				AddChild(root, other1)

				other2 := &DNSTreeNode{
					Name: "other2",
				}
				AddChild(other2, &DNSTreeNode{
					Name: "leaf1",
				})
				AddChild(root, other2)
				return root
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("root"))
				Expect(len(tree.Children)).To(Equal(2))
				Expect(hasLabelForBranch(tree, "other1", PropagateFlag, "true")).To(BeFalse())

				Expect(tree.Children[0].Name).To(Equal("other1"))

				Expect(hasLabelForBranch(tree.Children[0], "leaf1", PropagateFlag, "true")).To(BeFalse())
				Expect(hasLabelForBranch(tree.Children[0], "leaf2", PropagateFlag, "true")).To(BeTrue())

				Expect(tree.Children[1].Name).To(Equal("other2"))
			},
		},

		/*
			## Test Delete label propagates upwards
			in:
			R:
				O1:
					D
					D
				O2:
					L
			out:
			R:
				D(O1):
					D
					D
				O2:
					L
		*/
		{
			Name: "Test delete label propagates upwards",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{Name: "root"}
				other1 := &DNSTreeNode{
					Name: "other1",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"leaf1",
								"leaf2",
							},
						},
					},
				}
				AddChild(other1, &DNSTreeNode{
					Name: "leaf1",
				})
				AddChild(other1, &DNSTreeNode{
					Name: "leaf2",
				})
				AddChild(root, other1)

				other2 := &DNSTreeNode{
					Name: "other2",
				}
				AddChild(other2, &DNSTreeNode{
					Name: "leaf1",
				})
				AddChild(root, other2)
				return root
			},
			Verify: func(tree *DNSTreeNode) {
				Expect(tree.Name).To(Equal("root"))
				Expect(len(tree.Children)).To(Equal(2))
				Expect(hasLabelForBranch(tree, "other1", PropagateFlag, "true")).To(BeTrue())

				Expect(tree.Children[0].Name).To(Equal("other1"))

				Expect(hasLabelForBranch(tree.Children[0], "leaf1", PropagateFlag, "true")).To(BeTrue())
				Expect(hasLabelForBranch(tree.Children[0], "leaf2", PropagateFlag, "true")).To(BeTrue())

				Expect(tree.Children[1].Name).To(Equal("other2"))
				Expect(hasLabelForBranch(tree, "other2", PropagateFlag, "true")).To(BeFalse())
			},
		},
		/*
			## Test stop label (X1) below root = no deletes
			in:
			R:
				X1:
					O1:
						D1
						D2
					O2:
						D3
				X2:
					O3:
						L4
			out:
			R:
				X1:
					O1:
						L1
						L2
					O2:
						L3
				X2:
					O3:
						L4
		*/

		{
			Name: "Test existing stop label below root = no deletes",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{
					Name: "root",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								StopPropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"stop1",
								"stop2",
							},
						},
					},
				}
				stop1 := &DNSTreeNode{
					Name: "stop1",
				}
				other1 := &DNSTreeNode{
					Name: "other1",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"leaf1",
								"leaf2",
							},
						},
					},
				}

				AddChild(other1, &DNSTreeNode{Name: "leaf1"})
				AddChild(other1, &DNSTreeNode{Name: "leaf2"})
				AddChild(stop1, other1)

				other2 := &DNSTreeNode{
					Name: "other2",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"leaf3",
							},
						},
					},
				}

				AddChild(other2, &DNSTreeNode{Name: "leaf3"})
				AddChild(stop1, other2)
				AddChild(root, stop1)

				stop2 := &DNSTreeNode{
					Name: "stop2",
				}
				other3 := &DNSTreeNode{
					Name: "other3",
				}
				AddChild(other3, &DNSTreeNode{Name: "leaf4"})
				AddChild(stop2, other3)
				AddChild(root, stop2)
				return root
			},
			Verify: func(root *DNSTreeNode) {
				Expect(root.Name).To(Equal("root"))
				Expect(len(root.Children)).To(Equal(2))
				Expect(hasLabelForBranch(root, "stop1", PropagateFlag, "true")).To(BeFalse())
				Expect(root.Children[0].Name).To(Equal("stop1"))

				stop1 := root.Children[0]
				Expect(hasLabelForBranch(stop1, "other1", PropagateFlag, "true")).To(BeFalse())

				other1 := stop1.Children[0]
				Expect(other1.Name).To(Equal("other1"))
				Expect(hasLabelForBranch(other1, "leaf1", PropagateFlag, "true")).To(BeFalse())
				Expect(hasLabelForBranch(other1, "leaf2", PropagateFlag, "true")).To(BeFalse())
			},
		},
		/*
			## Test Delete label propagates downwards
			in:
			R:
				X:
					D(O1):
						L
						L
					O2:
						L
			out:
			R:
				X:
					D:
						D
						D
					O2:
						L
		*/

		{
			Name: "Test Delete label propagates downwards",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{
					Name: "root",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								StopPropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"stop1",
							},
						},
					},
				}
				stop1 := &DNSTreeNode{
					Name: "stop1",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"other1",
							},
						},
					},
				}
				other1 := &DNSTreeNode{Name: "other1"}

				AddChild(other1, &DNSTreeNode{Name: "leaf1"})
				AddChild(other1, &DNSTreeNode{Name: "leaf2"})
				AddChild(stop1, other1)

				other2 := &DNSTreeNode{Name: "other2"}

				AddChild(other2, &DNSTreeNode{Name: "leaf3"})
				AddChild(stop1, other2)
				AddChild(root, stop1)
				return root
			},
			Verify: func(root *DNSTreeNode) {
				Expect(root.Name).To(Equal("root"))
				Expect(len(root.Children)).To(Equal(1))
				Expect(hasLabelForBranch(root, "stop1", PropagateFlag, "true")).To(BeFalse())

				Expect(root.Children[0].Name).To(Equal("stop1"))
				stop1 := root.Children[0]
				Expect(hasLabelForBranch(stop1, "other1", PropagateFlag, "true")).To(BeTrue())

				other1 := stop1.Children[0]
				Expect(other1.Name).To(Equal("other1"))
				Expect(hasLabelForBranch(other1, "leaf1", PropagateFlag, "true")).To(BeTrue())
				Expect(hasLabelForBranch(other1, "leaf2", PropagateFlag, "true")).To(BeTrue())

				other2 := stop1.Children[1]
				Expect(other2.Name).To(Equal("other2"))
				Expect(hasLabelForBranch(stop1, "other2", PropagateFlag, "true")).To(BeFalse())
				Expect(hasLabelForBranch(other2, "leaf3", PropagateFlag, "true")).To(BeFalse())
			},
		},
		/*

			## Test unstopped root acts as stop
			in:
			R:
				O:
					D
			out:
			R:
				O:
					L
		*/

		{
			Name: "Test unstopped root acts as stop",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{
					Name: "root",
				}

				other := &DNSTreeNode{
					Name: "other",
					DataSets: []*DNSTreeNodeData{
						{
							Targets: []string{"leaf"},
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
						},
					},
				}

				AddChild(other, &DNSTreeNode{Name: "leaf"})
				AddChild(root, other)
				return root
			},
			Verify: func(root *DNSTreeNode) {
				Expect(hasLabelForBranch(root, "other", PropagateFlag, "true")).To(BeFalse())
				Expect(hasLabelForBranch(root.Children[0], "leaf", PropagateFlag, "true")).To(BeFalse())
			},
		},
		/*
			## Test unrelated trees do not impact each other
			in:
			R:
				X1:
					D
				X2:
					O1:
						D
					O2:
						L
			out:
			R:
				X1:
					L
				X2:
					D(O1):
						D
					O2:
						L
		*/

		{
			Name: "Test unrelated trees do not impact each other",
			Tree: func() *DNSTreeNode {
				root := &DNSTreeNode{
					Name: "root",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								StopPropagateFlag: "true",
							},
							Targets: []string{
								"stop1",
								"stop2",
							},
						},
					},
				}
				stop1 := &DNSTreeNode{
					Name: "stop1",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"leaf1",
							},
						},
					},
				}
				AddChild(stop1, &DNSTreeNode{Name: "leaf1"})
				AddChild(root, stop1)

				stop2 := &DNSTreeNode{Name: "stop2"}
				other1 := &DNSTreeNode{
					Name: "other1",
					DataSets: []*DNSTreeNodeData{
						{
							Labels: endpoint.Labels{
								PropagateFlag: "true",
							},
							Targets: endpoint.Targets{
								"leaf2",
							},
						},
					},
				}
				other2 := &DNSTreeNode{
					Name: "other2",
				}

				AddChild(other1, &DNSTreeNode{Name: "leaf2"})
				AddChild(other2, &DNSTreeNode{Name: "leaf3"})
				AddChild(stop2, other1)
				AddChild(stop2, other2)
				AddChild(root, stop2)
				return root
			},
			Verify: func(root *DNSTreeNode) {
				Expect(len(root.Children)).To(Equal(2))
				Expect(hasLabelForBranch(root, "stop1", PropagateFlag, "true")).To(BeFalse())
				Expect(hasLabelForBranch(root, "stop2", PropagateFlag, "true")).To(BeFalse())

				Expect(root.Children[0].Name).To(Equal("stop1"))
				Expect(hasLabelForBranch(root.Children[0], "leaf1", PropagateFlag, "true")).To(BeFalse())

				Expect(root.Children[1].Name).To(Equal("stop2"))
				Expect(hasLabelForBranch(root.Children[1], "other1", PropagateFlag, "true")).To(BeTrue())
				Expect(root.Children[1].Children[0].Name).To(Equal("other1"))
				Expect(hasLabelForBranch(root.Children[1].Children[0], "leaf2", PropagateFlag, "true")).To(BeTrue())

				Expect(hasLabelForBranch(root.Children[1], "other2", PropagateFlag, "true")).To(BeFalse())
				Expect(root.Children[1].Children[1].Name).To(Equal("other2"))
				Expect(hasLabelForBranch(root.Children[1].Children[1], "leaf3", PropagateFlag, "true")).To(BeFalse())
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			tree := scenario.Tree()
			PropagateStoppableLabel(tree, PropagateFlag, "true", StopPropagateFlag)
			scenario.Verify(tree)
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
								DataSets: []*DNSTreeNodeData{
									{
										RecordType: "A",
										Targets: []string{
											"172.32.200.1",
										},
									},
								},
							},
						},
						DataSets: []*DNSTreeNodeData{
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
								DataSets: []*DNSTreeNodeData{
									{
										RecordType: "A",
										Targets: []string{
											"172.32.200.2",
										},
									},
								},
							},
						},
						DataSets: []*DNSTreeNodeData{
							{
								RecordType: "CNAME",
								Targets: []string{
									"ip2.testdomain.com",
								},
							},
						},
					},
				},
				DataSets: []*DNSTreeNodeData{
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
		DataSets: []*DNSTreeNodeData{
			{
				RecordType: "CNAME",
				Targets: []string{
					"klb.testdomain.com",
				},
			},
		},
	}
}
