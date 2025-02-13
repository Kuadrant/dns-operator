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
	Parent   *DNSTreeNode
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

// PropagateStoppableLabel takes a propLabel (and value) to propagate throughout a tree, and a stopLabel
// whenever the label is propagated to a dataset in a node which also has the stopLabel, this node and
// all of the children of this node and any parents will have the propLabel removed.
//
// N.B. Any node with no parents is assumed to have the stopLabel, even when not present, to prevent the
// propLabel propagating to the entire tree (if this is required, use `AddLabelToBranch` on the root node).
//
// The overview of the logic of this function is as follows:
// - Spread propLabels as greedily as possible
//   - Any labelled node labels all of it's children
//   - Any node with all children labelled get's the propLabel too
//
// - Resolve the stopLabels
//   - Any node with the stopLabel and the propLabel:
//   - Has the propLabel removed from itself and all it's children
//   - Has the propLabel removed from any parent (or parent's parent) that has the label

func PropagateStoppableLabel(node *DNSTreeNode, propLabel, value, stopLabel string) {
	//propagate labels regardless of stop labels
	propagateLabel(node, propLabel, value)

	//propagate stop labels
	resolveStops(node, propLabel, value, stopLabel)
}

func resolveStops(node *DNSTreeNode, label, value, stopLabel string) {
	if isRoot(node) && allChildrenHaveLabel(node, label, value) {
		RemoveLabelFromTree(node, label)
		//entire tree cleaned so no need for any further checks
		return
	}

	//remove label from stop labelled children
	for _, c := range node.Children {
		d := findDataSetForChild(node, c.Name)
		//has label and stop label = remove label from this dataset and all children
		if d != nil && d.Labels[stopLabel] != "" && d.Labels[label] != "" {
			delete(d.Labels, label)
			RemoveLabelFromTree(c, label)
			RemoveLabelFromParents(node, label)
		} else {
			resolveStops(c, label, value, stopLabel)
		}

	}

}

func propagateLabel(node *DNSTreeNode, label, value string) {
	for _, c := range node.Children {
		d := findDataSetForChild(node, c.Name)
		if d != nil && d.Labels[label] != "" {
			//this child is labelled, indiscriminately label entire tree under this child
			AddLabelToTree(c, label, value)
		} else {
			// this child is not labelled, continue descending to propagate label
			propagateLabel(c, label, value)
		}
	}

	// if all children are labelled, label this branch in parent node
	if len(node.Children) > 0 && allChildrenHaveLabel(node, label, value) && node.Parent != nil {
		AddLabelToBranch(node.Parent, node.Name, label, value)
	}
}

func isRoot(node *DNSTreeNode) bool {
	return node.Parent == nil
}

func allChildrenHaveLabel(node *DNSTreeNode, label, value string) bool {
	for _, c := range node.Children {
		if !HasLabelForBranch(node, c.Name, label, value) {
			return false
		}
	}
	return true
}

func findDataSetForChild(node *DNSTreeNode, name string) *DNSTreeNodeData {
	for _, d := range node.DataSets {
		if slices.Contains(d.Targets, name) {
			return &d
		}
	}
	return nil
}

func AddLabelToBranch(node *DNSTreeNode, branch, label, value string) {
	d := findDataSetForChild(node, branch)
	if d == nil {
		node.DataSets = append(node.DataSets, DNSTreeNodeData{
			Labels: endpoint.Labels{
				label: value,
			},
			Targets: []string{
				branch,
			},
		})
	} else {
		if len(d.Targets) == 1 {
			d.Labels[label] = value
		} else {
			//remove target from shared dataset and recreate uniquely
			for i, t := range d.Targets {
				if t == branch {
					d.Targets = append(d.Targets[:i], d.Targets[i+1:]...)
					newDS := DNSTreeNodeData{
						Labels:  d.Labels.DeepCopy(),
						Targets: []string{branch},
					}
					newDS.Labels[label] = value
					node.DataSets = append(node.DataSets, newDS)
				}
			}
		}
	}
}

func AddChild(parent *DNSTreeNode, child *DNSTreeNode) {
	parent.Children = append(parent.Children, child)
	child.Parent = parent
}

func RemoveLabelFromParents(node *DNSTreeNode, label string) {
	if isRoot(node) {
		return
	}

	d := findDataSetForChild(node.Parent, node.Name)
	if d == nil {
		return
	}

	delete(d.Labels, label)

	RemoveLabelFromParents(node.Parent, label)
}

func RemoveLabelFromTree(node *DNSTreeNode, label string) {
	for _, d := range node.DataSets {
		delete(d.Labels, label)
	}

	for _, c := range node.Children {
		RemoveLabelFromTree(c, label)
	}
}

func AddLabelToTree(node *DNSTreeNode, label, value string) {
	for _, c := range node.Children {
		AddLabelToBranch(node, c.Name, label, value)
		AddLabelToTree(c, label, value)
	}
}

func HasLabelForBranch(node *DNSTreeNode, branch, label, value string) bool {
	d := findDataSetForChild(node, branch)
	if d == nil {
		return false
	}
	if v, ok := d.Labels[label]; ok {
		return value == v
	}
	return false
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

	for _, child := range node.Children {
		ToEndpoints(child, endpoints)
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
		c.Parent = node
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

// isALeafNode check if this is the last node in a tree
func isALeafNode(node *DNSTreeNode) bool {
	// no children means this is pointing to an IP or a host outside of the DNS Record
	return len(node.Children) == 0
}
