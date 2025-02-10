package common

import (
	"fmt"
	"io"
	"slices"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// DNSTreeNode stores a relation between endpoints that were parsed into a tree
type DNSTreeNode struct {
	Name     string
	Children []*DNSTreeNode
	DataSets []*DNSTreeNodeData
	Parent   *DNSTreeNode
}

func (d *DNSTreeNode) String() string {
	return fmt.Sprintf("host: %s, datasets: %+v, children: %+v", d.Name, d.DataSets, d.Children)
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

func (d DNSTreeNodeData) String() string {
	return fmt.Sprintf("targets: %+v, labels: %+v", d.Targets, d.Labels)
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

func WriteTree(s io.Writer, node *DNSTreeNode, title string) {
	fmt.Fprintf(s, "\n====== START TREE: %s ======\n", title)
	for _, ep := range *ToEndpoints(node, &[]*endpoint.Endpoint{}) {
		fmt.Fprintf(s, "	endpoint: %v > %+v with labels: %+v\n", ep.DNSName, ep.Targets, ep.Labels)
	}
	fmt.Fprintf(s, "====== END TREE: %s ======\n\n", title)
}

func PropagateStoppableLabel(node *DNSTreeNode, propLabel, value, stopLabel string) {
	//propagate labels regardless of stop labels
	PropagateLabel(node, propLabel, value)

	//propagate stop labels
	resolveStops(node, propLabel, value, stopLabel)
}

func CopyLabel(label string, from, to *DNSTreeNode) {
	toNode := FindNode(to, from.Name)
	if toNode != nil {
		// copy label values to any matching datasets
		for _, fd := range from.DataSets {
			for _, td := range toNode.DataSets {
				if slices.Equal(fd.Targets, td.Targets) {
					if fd.Labels == nil || fd.Labels[label] == "" {
						if td.Labels != nil {
							delete(td.Labels, label)
						}
					} else if fd.Labels[label] != "" {
						if td.Labels == nil {
							td.Labels = endpoint.NewLabels()
						}
						td.Labels[label] = fd.Labels[label]
					}
				}
			}
		}
	}
	for _, c := range from.Children {
		CopyLabel(label, c, to)
	}
}

func FindNode(tree *DNSTreeNode, name string) *DNSTreeNode {
	if tree.Name == name {
		return tree
	}
	for _, c := range tree.Children {
		n := FindNode(c, name)
		if n != nil {
			return n
		}
	}
	return nil
}

func resolveStops(node *DNSTreeNode, label, value, stopLabel string) {
	if isRoot(node) && allChildrenHaveLabel(node, label, value) {
		RemoveLabelFromTree(node, label)
		//entire tree cleaned so no need for any further checks
		return
	}

	//remove label from stop labelled children
	for _, c := range node.Children {
		dsets := findDataSetsForChild(node, c.Name)
		if len(dsets) == 0 {
			resolveStops(c, label, value, stopLabel)

		}
		for _, d := range dsets {
			//has label and stop label = remove label from this dataset and all children
			if d.Labels[stopLabel] != "" && d.Labels[label] != "" {
				delete(d.Labels, label)
				RemoveLabelFromTree(c, label)
				removeLabelFromParents(node, label)
			} else {
				resolveStops(c, label, value, stopLabel)
			}
		}

	}

}

func PropagateLabel(node *DNSTreeNode, label, value string) {
	for _, c := range node.Children {
		dsets := findDataSetsForChild(node, c.Name)
		if len(dsets) == 0 {
			PropagateLabel(c, label, value)
		}
		foundLabel := false
		for _, d := range dsets {
			if d.Labels[label] != "" {
				foundLabel = true
				break
			}
		}
		if foundLabel {
			//this child is labelled, indiscriminately label entire tree under this child
			AddLabelToTree(c, label, value)
		} else {
			// this child is not labelled, continue descending to propagate label
			PropagateLabel(c, label, value)
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
		if !hasLabelForBranch(node, c.Name, label, value) {
			return false
		}
	}
	return true
}

func findDataSetsForChild(node *DNSTreeNode, name string) []*DNSTreeNodeData {
	ret := []*DNSTreeNodeData{}
	for _, d := range node.DataSets {
		if slices.Contains(d.Targets, name) {
			ret = append(ret, d)
		}
	}
	return ret
}

func AddLabelToBranch(node *DNSTreeNode, branch, label, value string) {
	dsets := findDataSetsForChild(node, branch)
	if len(dsets) == 0 {
		node.DataSets = append(node.DataSets, &DNSTreeNodeData{
			Labels: endpoint.Labels{
				label: value,
			},
			Targets: []string{
				branch,
			},
		})
	} else {
		for _, d := range dsets {
			if len(d.Targets) == 1 {
				if d.Labels == nil {
					d.Labels = endpoint.NewLabels()
				}
				d.Labels[label] = value
			} else {
				//remove target from shared dataset and recreate uniquely
				for i, t := range d.Targets {
					if t == branch {
						d.Targets = append(d.Targets[:i], d.Targets[i+1:]...)
						newDS := &DNSTreeNodeData{
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
}

func AddChild(parent *DNSTreeNode, child *DNSTreeNode) {
	parent.Children = append(parent.Children, child)
	child.Parent = parent
}

func removeLabelFromParents(node *DNSTreeNode, label string) {
	if isRoot(node) {
		return
	}

	dsets := findDataSetsForChild(node.Parent, node.Name)
	if len(dsets) == 0 {
		return
	}

	for _, d := range dsets {
		delete(d.Labels, label)

	}
	removeLabelFromParents(node.Parent, label)
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
	if len(node.DataSets) == 0 {
		node.DataSets = append(node.DataSets, &DNSTreeNodeData{Labels: map[string]string{label: value}})
	} else {
		for _, d := range node.DataSets {
			if d.Labels == nil {
				d.Labels = endpoint.NewLabels()
			}
			d.Labels[label] = value
		}
	}
	for _, c := range node.Children {
		AddLabelToBranch(node, c.Name, label, value)
		AddLabelToTree(c, label, value)
	}
}

func hasLabelForBranch(node *DNSTreeNode, branch, label, value string) bool {
	for _, d := range findDataSetsForChild(node, branch) {
		if v, ok := d.Labels[label]; ok {
			if value == v {
				return true
			} else {
				return false
			}
		}
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

	var healthyDataSets []*DNSTreeNodeData
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
	if node == nil {
		return &[]*endpoint.Endpoint{}
	}

	if endpoints == nil {
		endpoints = &[]*endpoint.Endpoint{}
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
		c.Parent = node
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

func findDataSets(name string, endpoints []*endpoint.Endpoint) []*DNSTreeNodeData {
	dataSets := []*DNSTreeNodeData{}
	for _, ep := range endpoints {
		if ep.DNSName == name {
			dataSets = append(dataSets, &DNSTreeNodeData{
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
