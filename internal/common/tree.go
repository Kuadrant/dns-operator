package common

import (
	"fmt"
	"io"
	"slices"

	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/slice"
)

// DNSTreeNode stores a relation between endpoints that were parsed into a tree
type DNSTreeNode struct {
	Name      string
	Children  []*DNSTreeNode
	Endpoints []*endpoint.Endpoint
	Parent    *DNSTreeNode
}

func (d *DNSTreeNode) String() string {
	return fmt.Sprintf("host: %s, endpoints: %+v, children: %+v", d.Name, d.Endpoints, d.Children)
}

func WriteTree(s io.Writer, node *DNSTreeNode, title string) {
	WriteEndpoints(s, *ToEndpoints(node, &[]*endpoint.Endpoint{}), title)
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
	PropagateLabel(node, propLabel, value)

	//propagate stop labels
	resolveStops(node, propLabel, value, stopLabel)
}

func CopyLabel(label string, from, to *DNSTreeNode) {
	toNode := FindNode(to, from.Name)
	if toNode != nil {
		// copy label values to any matching datasets
		for _, fromEP := range from.Endpoints {
			for _, toEP := range toNode.Endpoints {
				if fromEP.Key() == toEP.Key() {
					if fromEP.Labels == nil || fromEP.Labels[label] == "" {
						if toEP.Labels != nil {
							delete(toEP.Labels, label)
						}
					} else {
						if toEP.Labels == nil {
							toEP.Labels = endpoint.NewLabels()
						}
						toEP.Labels[label] = fromEP.Labels[label]
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
		hadLabel := false
		endpoints := findEndpointsForChild(node, c.Name)
		for _, ep := range endpoints {
			//has label and stop label = remove label from this dataset and all children
			if ep.Labels != nil && ep.Labels[stopLabel] == value && ep.Labels[label] == value {
				delete(ep.Labels, label)
				RemoveLabelFromTree(c, label)
				removeLabelFromParents(node, label)
				hadLabel = true
				break
			}
		}
		if !hadLabel {
			resolveStops(c, label, value, stopLabel)
		}

	}

}

func PropagateLabel(node *DNSTreeNode, label, value string) {
	for _, c := range node.Children {
		dsets := findEndpointsForChild(node, c.Name)
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

func findEndpointsForChild(node *DNSTreeNode, name string) []*endpoint.Endpoint {
	ret := []*endpoint.Endpoint{}
	for _, ep := range node.Endpoints {
		if slices.Contains(ep.Targets, name) {
			ret = append(ret, ep)
		}
	}
	return ret
}

func AddLabelToBranch(node *DNSTreeNode, branch, label, value string) {
	eps := findEndpointsForChild(node, branch)
	if len(eps) == 0 {
		node.Endpoints = append(node.Endpoints, &endpoint.Endpoint{
			DNSName: node.Name,
			Labels: endpoint.Labels{
				label: value,
			},
			Targets: []string{
				branch,
			},
		})
	} else {
		for _, ep := range eps {
			if len(ep.Targets) == 1 {
				if ep.Labels == nil {
					ep.Labels = endpoint.NewLabels()
				}
				ep.Labels[label] = value
			} else {
				//remove target from shared endpoint and recreate uniquely
				for i, t := range ep.Targets {
					if t == branch {
						ep.Targets = append(ep.Targets[:i], ep.Targets[i+1:]...)
						newEP := &endpoint.Endpoint{
							Labels:  ep.Labels.DeepCopy(),
							Targets: []string{branch},
						}
						if newEP.Labels == nil {
							newEP.Labels = endpoint.NewLabels()
						}
						newEP.Labels[label] = value
						node.Endpoints = append(node.Endpoints, newEP)
						break
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

	eps := findEndpointsForChild(node.Parent, node.Name)

	for _, ep := range eps {
		delete(ep.Labels, label)

	}
	removeLabelFromParents(node.Parent, label)
}

func RemoveLabelFromTree(node *DNSTreeNode, label string) {
	for _, ep := range node.Endpoints {
		delete(ep.Labels, label)
	}

	for _, c := range node.Children {
		RemoveLabelFromTree(c, label)
	}
}

func AddLabelToTree(node *DNSTreeNode, label, value string) {

	for _, ep := range node.Endpoints {
		if ep.Labels == nil {
			ep.Labels = endpoint.NewLabels()
		}
		ep.Labels[label] = value
	}

	for _, c := range node.Children {
		AddLabelToBranch(node, c.Name, label, value)
		AddLabelToTree(c, label, value)
	}
}

func hasLabelForBranch(node *DNSTreeNode, branch, label, value string) bool {
	for _, ep := range findEndpointsForChild(node, branch) {
		if v, ok := ep.Labels[label]; ok {
			return value == v
		}
	}
	return false
}

func RemoveNode(container, removeNode *DNSTreeNode) {
	children := append([]*DNSTreeNode{}, container.Children...)
	for i, child := range children {
		if child.Name == removeNode.Name {
			container.Children = append(container.Children[:i], container.Children[i+1:]...)

			newEps := []*endpoint.Endpoint{}
			for _, ep := range container.Endpoints {
				if slices.Contains(ep.Targets, removeNode.Name) {
					//only one target shouldn't go to newEPs
					if len(ep.Targets) > 1 {
						ep.Targets = slice.RemoveString(ep.Targets, removeNode.Name)
						newEps = append(newEps, ep)
					}
				} else {
					newEps = append(newEps, ep)
				}
			}
			container.Endpoints = newEps
			// no children left? Remove this node from it's parent
			if len(container.Children) == 0 && container.Parent != nil {
				RemoveNode(container.Parent, container)
			}
			return
		}

		RemoveNode(child, removeNode)
	}
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

	for _, ep := range node.Endpoints {
		if len(ep.Labels) == 0 {
			ep.Labels = nil
		}
		*endpoints = append(*endpoints, ep)
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
	if rootHost == "" {
		return &DNSTreeNode{}
	}
	rootNode := &DNSTreeNode{Name: rootHost}
	populateNode(rootNode, endpoints)
	return rootNode
}

func populateNode(node *DNSTreeNode, endpoints []*endpoint.Endpoint) {
	node.Endpoints = findEndpointsForName(node.Name, endpoints)

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

func findEndpointsForName(name string, endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	retEndpoints := []*endpoint.Endpoint{}
	for _, ep := range endpoints {
		if ep.DNSName == name {
			retEndpoints = append(retEndpoints, ep)
		}
	}
	return retEndpoints
}

// isALeafNode check if this is the last node in a tree
func isALeafNode(node *DNSTreeNode) bool {
	// no children means this is pointing to an IP or a host outside of the DNS Record
	return len(node.Children) == 0
}
