package plan

import (
	"slices"
	"strings"
)

const (
	// LabelDelimiter is a default delimiter for labels if a label key has multiple values
	LabelDelimiter = "&&"
)

func EnsureLabel(labels, label string) string {
	labelsSplit := SplitLabels(labels)
	// this can cause duplicate, but the joinLabels will clean that up
	labelsSplit = append(labelsSplit, label)
	// remove empty values
	return RemoveLabel(joinLabels(labelsSplit), "")
}

func RemoveLabel(labels, label string) string {
	labelsSplit := SplitLabels(labels)
	var returnLabels []string
	for _, l := range labelsSplit {
		if l == label {
			continue
		}
		returnLabels = append(returnLabels, l)
	}
	return joinLabels(returnLabels)
}

func SplitLabels(labels string) []string {
	if labels == "" {
		return []string{}
	}
	return strings.Split(labels, LabelDelimiter)
}

func joinLabels(labels []string) string {
	slices.Sort(labels)
	labels = slices.Compact(labels)
	return strings.Join(labels, LabelDelimiter)
}
