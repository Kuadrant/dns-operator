package common

import (
	"slices"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// DNSTreeNode stores a relation between endpoints that were parsed into a tree
type DNSTreeNode struct {
	Name     string
	Children []*DNSTreeNode
	DataSets []DNSTreeNodeData
}

// DNSTreeNodeData holds a data for the enpoint(s) that correspond to this node
type DNSTreeNodeData struct {
	RecordType       string
	SetIdentifier    string
	RecordTTL        endpoint.TTL
	Labels           endpoint.Labels
	ProviderSpecific endpoint.ProviderSpecific
	Targets          []string
}

// RemoveNode removes a node from a tree.
// If the node was the only child of the parent node,
// the parent will be removed as well unless the parent is a root node
func (n *DNSTreeNode) RemoveNode(deleteNode *DNSTreeNode) {
	// store indexes of dead branches
	var deadBranches []int
	var deadDataSets []string

	if deleteNode == nil {
		return
	}

	for i, node := range n.Children {
		if node.Name == deleteNode.Name {
			n.Children = append(n.Children[:i], n.Children[i+1:]...)

			// we removed child, but need to clean up data sets
			for j, dataset := range n.DataSets {
				index := slices.Index(dataset.Targets, deleteNode.Name)
				if index >= 0 {
					n.DataSets[j].Targets = append(dataset.Targets[:index], dataset.Targets[index+1:]...)
				}
			}
			return
		}

		// no children matched, try on each child
		node.RemoveNode(deleteNode)

		// the removed node was the only child - we have a dead branch (it is a leaf now)
		// children are nil on leaf node
		// not checking for it will nuke the whole tree as leafs will be considered dead branches
		if node.Children != nil && isALeafNode(node) {
			// store the index. indexes are in ascending order
			deadBranches = append(deadBranches, i)

			// we can't rely on indexes for data sets, so store node name
			deadDataSets = append(deadDataSets, node.Name)
		}
	}

	// prune dead branches separately from the main for loop.
	// doing it inside will shift indexes the for loop is iterating through
	for count, deadBranchIndex := range deadBranches {
		// after the first removal, all subsequent deadBranchIndexes will de one to high,
		// but since we have them ascending, we can use count as modifier
		n.Children = append(n.Children[:deadBranchIndex-count], n.Children[deadBranchIndex-count+1:]...)
	}

	var healthyDataSets []DNSTreeNodeData
	// clean up data nodes from dead branches
	for _, dataSet := range n.DataSets {

		// we are dealing with CNAMES only here.
		// the A record is already removed from datasets
		if !slices.Contains(deadDataSets, dataSet.Targets[0]) {
			healthyDataSets = append(healthyDataSets, dataSet)
		}
	}
	n.DataSets = healthyDataSets

}

// GetLeafsTargets returns IP or CNAME of the leafs of a tree.
// alternatively, it can populate the passed in array with pointers to targets
func GetLeafsTargets(node *DNSTreeNode, targets *[]string) *[]string {
	if node == nil || targets == nil {
		return &[]string{}
	}

	if isALeafNode(node) {
		*targets = append(*targets, node.Name)
		return nil
	}
	for _, child := range node.Children {
		GetLeafsTargets(child, targets)
	}
	return targets
}

// ToEndpoints transforms a tree into an array of endpoints.
// The array could be returned or passed in to be populated
func ToEndpoints(node *DNSTreeNode, endpoints *[]*endpoint.Endpoint) *[]*endpoint.Endpoint {
	if node == nil || endpoints == nil {
		return &[]*endpoint.Endpoint{}
	}

	if isALeafNode(node) {
		return endpoints
	}
	targets := []string{}
	for _, child := range node.Children {
		targets = append(targets, child.Name)
		ToEndpoints(child, endpoints)
	}

	// this should not happen. the node is either leaf or has datasets (unless the cree was made manually)
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
	if record == nil {
		return &DNSTreeNode{}
	}
	return MakeTreeFromEndpoints(record.Spec.RootHost, record.Spec.Endpoints)
}

func MakeTreeFromEndpoints(rootHost string, endpoints []*endpoint.Endpoint) *DNSTreeNode {
	rootNode := &DNSTreeNode{Name: rootHost}
	populateNode(rootNode, endpoints)
	return rootNode
}

func populateNode(node *DNSTreeNode, endpoints []*endpoint.Endpoint) {
	node.DataSets = findDataSets(node.Name, endpoints)

	children := findChildren(node.Name, endpoints)
	if len(children) == 0 {
		return
	}

	for _, c := range children {
		populateNode(c, endpoints)
	}
	node.Children = children
}

func findChildren(name string, endpoints []*endpoint.Endpoint) []*DNSTreeNode {
	nodes := []*DNSTreeNode{}
	targets := map[string]string{}
	for _, ep := range endpoints {
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

func findDataSets(name string, endpoints []*endpoint.Endpoint) []DNSTreeNodeData {
	dataSets := []DNSTreeNodeData{}
	for _, ep := range endpoints {
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

// isALeafNode check if this is the last node in a tree
func isALeafNode(node *DNSTreeNode) bool {
	// no children means this is pointing to an IP or a host outside of the DNS Record
	return node.Children == nil || len(node.Children) == 0
}
