package common

import metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

// MergeLabels combines the map of labels to the existing labels on an object
func MergeLabels(object metav1.Object, labels map[string]string) bool {
	objLabels := object.GetLabels()

	if objLabels == nil {
		object.SetLabels(labels)
		return true
	}

	updated := false
	for key, value := range labels {
		label, exists := objLabels[key]
		if !exists || label != value {
			objLabels[key] = value
			updated = true
		}
	}

	object.SetLabels(objLabels)
	return updated
}
