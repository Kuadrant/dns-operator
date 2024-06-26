package common

import (
	"reflect"
	"time"

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/rand"
)

// RandomizeDuration randomizes duration for a given variance.
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeDuration(variance float64, duration time.Duration) time.Duration {
	// do not allow less than a second requeue
	if duration.Milliseconds() < 1000 {
		duration = time.Second * 1
	}
	// we won't go smaller than a second - using milliseconds to have a relatively big number to randomize
	millisecond := float64(duration.Milliseconds())

	upperLimit := millisecond * (1.0 + variance)
	lowerLimit := millisecond * (1.0 - variance)

	return time.Millisecond * time.Duration(rand.Int63nRange(
		int64(lowerLimit),
		int64(upperLimit)))
}

func Owns(owner, object metav1.Object) bool {
	for _, ref := range object.GetOwnerReferences() {
		if ref.UID == owner.GetUID() {
			return true
		}
	}
	return false
}

func EnsureOwnerRef(owner, owned metav1.Object, blockDelete bool) error {
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
