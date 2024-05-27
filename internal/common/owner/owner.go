package owner

import (
	"reflect"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func Owns(owner, object metav1.Object) bool {
	for _, ref := range object.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

func EnsureOwnerRef(owned, owner metav1.Object, blockDelete bool) error {
	ownerType, err := meta.TypeAccessor(owner)
	if err != nil {
		return err
	}

	ownerRef := metav1.OwnerReference{
		APIVersion:         ownerType.GetAPIVersion(),
		Kind:               ownerType.GetKind(),
		Name:               owner.GetName(),
		UID:                owner.GetUID(),
		BlockOwnerDeletion: &blockDelete,
	}

	// check for existing ref
	for i, ref := range owned.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			if reflect.DeepEqual(ref, ownerRef) {
				// no changes to make, return
				return nil
			}
			// we need to update the ownerRef, remove the existing one
			if len(owned.GetOwnerReferences()) == 1 {
				owned.SetOwnerReferences([]metav1.OwnerReference{})
			} else {
				owned.SetOwnerReferences(append(owned.GetOwnerReferences()[:i], owner.GetOwnerReferences()[i+1:]...))
			}
			break
		}
	}

	// add ownerRef to object
	owned.SetOwnerReferences(append(owned.GetOwnerReferences(), ownerRef))

	return nil

}
