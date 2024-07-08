//go:build unit

package common

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func TestRandomizeDuration(t *testing.T) {
	testIterations := 100

	tests := []struct {
		name     string
		variance float64
		duration time.Duration
	}{
		{
			name:     "returns valid duration in range",
			variance: 0.5,
			duration: time.Second * 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := 0
			for i < testIterations {
				if got := RandomizeDuration(tt.variance, tt.duration); !isValidVariance(tt.duration, got, tt.variance) {
					t.Errorf("RandomizeDuration() invalid randomization; got = %v", got.String())
				}
				i++
			}
		})
	}
}

func isValidVariance(duration, randomizedDuration time.Duration, variance float64) bool {
	upperLimit := float64(duration.Milliseconds()) + float64(duration.Milliseconds())*variance
	lowerLimmit := float64(duration.Milliseconds()) - float64(duration.Milliseconds())*variance

	return float64(randomizedDuration.Milliseconds()) >= lowerLimmit &&
		float64(randomizedDuration.Milliseconds()) < upperLimit
}

func TestOwns(t *testing.T) {
	RegisterTestingT(t)
	testCases := []struct {
		Name   string
		Object metav1.Object
		Owner  metav1.Object
		Verify func(t *testing.T, result bool)
	}{
		{
			Name: "object is owned",
			Object: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone",
							UID:                "unique-uid",
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					UID: "unique-uid",
				},
			},
			Verify: func(t *testing.T, result bool) {
				Expect(result).To(BeTrue())
			},
		}, {
			Name: "object is owned by multiple",
			Object: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other",
							UID:                "unique-uid-other",
							BlockOwnerDeletion: ptr.To(true),
						},
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone",
							UID:                "unique-uid",
							BlockOwnerDeletion: ptr.To(true),
						},
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other2",
							UID:                "unique-uid-other2",
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					UID: "unique-uid",
				},
			},
			Verify: func(t *testing.T, result bool) {
				Expect(result).To(BeTrue())
			},
		}, {
			Name: "object is not owned",
			Object: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other",
							UID:                "unique-uid-other",
							BlockOwnerDeletion: ptr.To(false),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					UID: "unique-uid",
				},
			},
			Verify: func(t *testing.T, result bool) {
				Expect(result).To(BeFalse())
			},
		}, {
			Name: "object is not owned multiple",
			Object: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other",
							UID:                "unique-uid-other",
							BlockOwnerDeletion: ptr.To(true),
						}, {
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other2",
							UID:                "unique-uid-other2",
							BlockOwnerDeletion: ptr.To(false),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					UID: "unique-uid",
				},
			},
			Verify: func(t *testing.T, result bool) {
				Expect(result).To(BeFalse())
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			testCase.Verify(t, Owns(testCase.Owner, testCase.Object))
		})
	}
}

func TestEnsureOwnerRef(t *testing.T) {
	RegisterTestingT(t)
	testCases := []struct {
		Name        string
		Owned       metav1.Object
		Owner       metav1.Object
		BlockDelete bool
		Verify      func(t *testing.T, err error, obj metav1.Object)
	}{
		{
			Name:  "Owner is added",
			Owned: &v1alpha1.DNSRecord{},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-zone",
					UID:  "unique-uid",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "ManagedZone",
					APIVersion: "v1beta1",
				},
			},
			BlockDelete: true,
			Verify: func(t *testing.T, err error, obj metav1.Object) {
				Expect(err).NotTo(HaveOccurred())
				Expect(len(obj.GetOwnerReferences())).To(Equal(1))

				expectedOwnerRef := metav1.OwnerReference{
					APIVersion:         "v1beta1",
					Kind:               "ManagedZone",
					Name:               "test-zone",
					UID:                "unique-uid",
					BlockOwnerDeletion: ptr.To(true),
				}
				Expect(obj.GetOwnerReferences()[0]).To(Equal(expectedOwnerRef))
			},
		},
		{
			Name: "Does not duplicate owner ref",
			Owned: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone",
							UID:                "unique-uid",
							BlockOwnerDeletion: ptr.To(true),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-zone",
					UID:  "unique-uid",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "ManagedZone",
					APIVersion: "v1beta1",
				},
			},
			BlockDelete: true,
			Verify: func(t *testing.T, err error, obj metav1.Object) {
				Expect(err).NotTo(HaveOccurred())
				Expect(len(obj.GetOwnerReferences())).To(Equal(1))

				expectedOwnerRef := metav1.OwnerReference{
					APIVersion:         "v1beta1",
					Kind:               "ManagedZone",
					Name:               "test-zone",
					UID:                "unique-uid",
					BlockOwnerDeletion: ptr.To(true),
				}
				Expect(obj.GetOwnerReferences()[0]).To(Equal(expectedOwnerRef))
			},
		},
		{
			Name: "Does update owner ref",
			Owned: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone",
							UID:                "unique-uid",
							BlockOwnerDeletion: ptr.To(false),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-zone",
					UID:  "unique-uid",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "ManagedZone",
					APIVersion: "v1beta1",
				},
			},
			BlockDelete: true,
			Verify: func(t *testing.T, err error, obj metav1.Object) {
				Expect(err).NotTo(HaveOccurred())
				Expect(len(obj.GetOwnerReferences())).To(Equal(1))

				expectedOwnerRef := metav1.OwnerReference{
					APIVersion:         "v1beta1",
					Kind:               "ManagedZone",
					Name:               "test-zone",
					UID:                "unique-uid",
					BlockOwnerDeletion: ptr.To(true),
				}
				Expect(obj.GetOwnerReferences()[0]).To(Equal(expectedOwnerRef))
			},
		},
		{
			Name: "Does append owner ref",
			Owned: &v1alpha1.DNSRecord{
				ObjectMeta: metav1.ObjectMeta{
					OwnerReferences: []metav1.OwnerReference{
						{
							APIVersion:         "v1",
							Kind:               "OtherThing",
							Name:               "otherName",
							UID:                "other-unique-uid",
							BlockOwnerDeletion: ptr.To(false),
						},
					},
				},
			},
			Owner: &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name: "test-zone",
					UID:  "unique-uid",
				},
				TypeMeta: metav1.TypeMeta{
					Kind:       "ManagedZone",
					APIVersion: "v1beta1",
				},
			},
			BlockDelete: true,
			Verify: func(t *testing.T, err error, obj metav1.Object) {
				Expect(err).NotTo(HaveOccurred())
				Expect(len(obj.GetOwnerReferences())).To(Equal(2))

				expectedOwnerRef := metav1.OwnerReference{
					APIVersion:         "v1beta1",
					Kind:               "ManagedZone",
					Name:               "test-zone",
					UID:                "unique-uid",
					BlockOwnerDeletion: ptr.To(true),
				}
				Expect(obj.GetOwnerReferences()[1]).To(Equal(expectedOwnerRef))
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			err := EnsureOwnerRef(testCase.Owner, testCase.Owned, testCase.BlockDelete)
			testCase.Verify(t, err, testCase.Owned)
		})
	}
}
