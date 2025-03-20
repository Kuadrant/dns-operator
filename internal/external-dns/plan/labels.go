package plan

import (
	"slices"
	"strings"
)

const (
	// LabelDelaminator is a default delaminator for labels if a label key has multiple values
	LabelDelaminator = "&&"

	// SoftDeleteKey indicates that endpoint can be soft deleted
	SoftDeleteKey = "kuadrant/soft-delete"

	// StopSoftDeleteKey forbids soft deletion of endpoint
	StopSoftDeleteKey = "kuadrant/stop-soft-delete"
)

func EnsureLabel(labels, label string) string {
	labelsSplit := SplitLabels(labels)
	labelsSplit = append(labelsSplit, label)
	// remove empty values
	return RemoveLabel(joinLabels(labelsSplit), "")
}

func RemoveLabel(labels, label string) string {
	labelsSplit := SplitLabels(labels)
	for i, l := range labelsSplit {
		if l == label {
			labelsSplit = append(labelsSplit[:i], labelsSplit[i+1:]...)
			break
		}
	}
	return joinLabels(labelsSplit)
}

func SplitLabels(labels string) []string {
	if labels == "" {
		return []string{}
	}
	return strings.Split(labels, LabelDelaminator)
}

func joinLabels(labels []string) string {
	slices.Sort(labels)
	labels = slices.Compact(labels)
	return strings.Join(labels, LabelDelaminator)
}
