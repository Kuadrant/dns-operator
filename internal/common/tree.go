package common

import (
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

type DNSTreeNode struct {
	Name     string
	Children []*DNSTreeNode
	DataSets []DNSTreeNodeData
}

type DNSTreeNodeData struct {
	RecordType       string
	SetIdentifier    string
	RecordTTL        endpoint.TTL
	Labels           endpoint.Labels
	ProviderSpecific endpoint.ProviderSpecific
	Targets          []string
}

func (n *DNSTreeNode) RemoveNode(deleteNode *DNSTreeNode) {
	for i, node := range n.Children {
		if node.Name == deleteNode.Name {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)
			return
		}

		// no children matched, try on each child
		node.RemoveNode(deleteNode)
	}
}

func ToEndpoints(node *DNSTreeNode, endpoints *[]*endpoint.Endpoint) *[]*endpoint.Endpoint {
	// no children means this is pointing to an IP or a host outside of the DNS Record
	if len(node.Children) == 0 {
		return endpoints
	}
	targets := []string{}
	for _, child := range node.Children {
		targets = append(targets, child.Name)
		ToEndpoints(child, endpoints)
	}

	if node.DataSets == nil {
		*endpoints = append(*endpoints, &endpoint.Endpoint{
			DNSName: node.Name,
			Targets: targets,
		})
		return endpoints
	}

	for _, data := range node.DataSets {
		*endpoints = append(*endpoints, &endpoint.Endpoint{
			DNSName:          node.Name,
			Targets:          data.Targets,
			RecordType:       data.RecordType,
			RecordTTL:        data.RecordTTL,
			SetIdentifier:    data.SetIdentifier,
			Labels:           data.Labels,
			ProviderSpecific: data.ProviderSpecific,
		})
	}
	return endpoints
}

func MakeTreeFromDNSRecord(record *v1alpha1.DNSRecord) *DNSTreeNode {
	rootNode := &DNSTreeNode{Name: record.Spec.RootHost}
	populateNode(rootNode, record)
	return rootNode
}

func populateNode(node *DNSTreeNode, record *v1alpha1.DNSRecord) {
	node.DataSets = findDataSets(node.Name, record)

	children := findChildren(node.Name, record)
	if len(children) == 0 {
		return
	}

	for _, c := range children {
		populateNode(c, record)
	}
	node.Children = children
}

func findChildren(name string, record *v1alpha1.DNSRecord) []*DNSTreeNode {
	nodes := []*DNSTreeNode{}
	targets := map[string]string{}
	for _, ep := range record.Spec.Endpoints {
		if ep.DNSName == name {
			for _, t := range ep.Targets {
				targets[t] = t
			}
		}
	}
	for _, t := range targets {
		nodes = append(nodes, &DNSTreeNode{Name: t})
	}

	return nodes
}

func findDataSets(name string, record *v1alpha1.DNSRecord) []DNSTreeNodeData {
	dataSets := []DNSTreeNodeData{}
	for _, ep := range record.Spec.Endpoints {
		if ep.DNSName == name {
			dataSets = append(dataSets, DNSTreeNodeData{
				RecordType:       ep.RecordType,
				RecordTTL:        ep.RecordTTL,
				SetIdentifier:    ep.SetIdentifier,
				Labels:           ep.Labels,
				ProviderSpecific: ep.ProviderSpecific,
				Targets:          ep.Targets,
			})
		}
	}
	return dataSets
}
