package owner

import (
	"testing"

	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/pointer"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

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
							BlockOwnerDeletion: pointer.Bool(true),
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
							BlockOwnerDeletion: pointer.Bool(true),
						},
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone",
							UID:                "unique-uid",
							BlockOwnerDeletion: pointer.Bool(true),
						},
						{
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other2",
							UID:                "unique-uid-other2",
							BlockOwnerDeletion: pointer.Bool(true),
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
							BlockOwnerDeletion: pointer.Bool(false),
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
							BlockOwnerDeletion: pointer.Bool(true),
						}, {
							APIVersion:         "v1beta1",
							Kind:               "ManagedZone",
							Name:               "test-zone-other2",
							UID:                "unique-uid-other2",
							BlockOwnerDeletion: pointer.Bool(false),
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
					BlockOwnerDeletion: pointer.Bool(true),
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
							BlockOwnerDeletion: pointer.Bool(true),
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
					BlockOwnerDeletion: pointer.Bool(true),
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
							BlockOwnerDeletion: pointer.Bool(false),
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
					BlockOwnerDeletion: pointer.Bool(true),
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
							BlockOwnerDeletion: pointer.Bool(false),
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
					BlockOwnerDeletion: pointer.Bool(true),
				}
				Expect(obj.GetOwnerReferences()[1]).To(Equal(expectedOwnerRef))
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			err := EnsureOwnerRef(testCase.Owned, testCase.Owner, testCase.BlockDelete)
			testCase.Verify(t, err, testCase.Owned)
		})
	}
}
