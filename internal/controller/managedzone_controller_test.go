//go:build integration

/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controller

import (
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

var _ = Describe("ManagedZoneReconciler", func() {
	Context("testing ManagedZone controller", func() {
		var dnsProviderSecret *v1.Secret
		var managedZone *v1alpha1.ManagedZone
		var testNamespace string

		BeforeEach(func() {
			CreateNamespace(&testNamespace)

			dnsProviderSecret = testBuildInMemoryCredentialsSecret("inmemory-credentials", testNamespace)
			managedZone = testBuildManagedZone("mz-example-com", testNamespace, "example.com", dnsProviderSecret.Name)

			Expect(k8sClient.Create(ctx, dnsProviderSecret)).To(Succeed())
		})

		AfterEach(func() {
			// Clean up managedZones
			mzList := &v1alpha1.ManagedZoneList{}
			err := k8sClient.List(ctx, mzList, client.InNamespace(testNamespace))
			Expect(err).NotTo(HaveOccurred())
			for _, mz := range mzList.Items {
				err = k8sClient.Delete(ctx, &mz)
				Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
			}

			if dnsProviderSecret != nil {
				err := k8sClient.Delete(ctx, dnsProviderSecret)
				Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
			}
		})

		It("should accept a managed zone for this controller and allow deletion", func() {
			Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())

			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(managedZone.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"ObservedGeneration": Equal(managedZone.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			Expect(k8sClient.Delete(ctx, managedZone)).To(Succeed())
			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone)
				if err != nil && !errors.IsNotFound(err) {
					return err
				}
				return nil
			}, TestTimeoutMedium, TestRetryIntervalMedium).Should(Succeed())
		})

		It("should reject a managed zone with an invalid domain name", func() {
			invalidDomainNameManagedZone := &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid_domain",
					Namespace: testNamespace,
				},
				Spec: v1alpha1.ManagedZoneSpec{
					ID:         "invalid_domain",
					DomainName: "invalid_domain",
				},
			}
			err := k8sClient.Create(ctx, invalidDomainNameManagedZone)
			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("spec.domainName in body should match"))
		})

		It("managed zone should not become ready with a spec.ID that does not exist", func() {
			//Create a managed zone with no spec.ID
			Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(managedZone.Status.ID).To(Equal("example.com"))
				g.Expect(managedZone.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"ObservedGeneration": Equal(managedZone.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			//Create a second managed zone with spec.ID referencing a zone id that does not exist
			mz := testBuildManagedZone("mz-example-com-2", testNamespace, "example.com", dnsProviderSecret.Name)
			mz.Spec.ID = "dosnotexist"
			Expect(k8sClient.Create(ctx, mz)).To(Succeed())
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: mz.Namespace, Name: mz.Name}, mz)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mz.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":   Equal(string(v1alpha1.ConditionTypeReady)),
						"Status": Equal(metav1.ConditionFalse),
						"Reason": Equal("ProviderError"),
						"Message": And(
							ContainSubstring("The DNS provider failed to ensure the managed zone"),
							ContainSubstring("not found")),
					})),
				)
				g.Expect(mz.Finalizers).To(ContainElement(ManagedZoneFinalizer))
				return nil
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			//Update second managed zone to use the known existing managed zone id (managedZone.Status.ID)
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mz), mz)
				g.Expect(err).NotTo(HaveOccurred())

				mz.Spec.ID = managedZone.Status.ID
				err = k8sClient.Update(ctx, mz)
				g.Expect(err).NotTo(HaveOccurred())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: mz.Namespace, Name: mz.Name}, mz)
				Expect(err).NotTo(HaveOccurred())
				g.Expect(mz.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"ObservedGeneration": Equal(mz.Generation),
					})),
				)
				g.Expect(mz.Finalizers).To(ContainElement(ManagedZoneFinalizer))
				return nil
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})

		It("managed zone should not become ready with a spec.DomainName and spec.ID that do no match provider zone", func() {
			//Create a managed zone with no spec.ID
			Expect(k8sClient.Create(ctx, managedZone)).To(Succeed())
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(managedZone), managedZone)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(managedZone.Status.ID).To(Equal("example.com"))
				g.Expect(managedZone.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"ObservedGeneration": Equal(managedZone.Generation),
					})),
				)
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			//Create a second managed zone with spec.ID of known existing managed zone (managedZone.Status.ID) but with a different domainName i.e. !example.com
			mz := testBuildManagedZone("mz-example-com-2", testNamespace, "foo.example.com", dnsProviderSecret.Name)
			mz.Spec.ID = managedZone.Status.ID
			Expect(k8sClient.Create(ctx, mz)).To(Succeed())
			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: mz.Namespace, Name: mz.Name}, mz)
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(mz.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":  Equal(metav1.ConditionFalse),
						"Reason":  Equal("ZoneValidationError"),
						"Message": Equal("ZoneValidationError, zone DNS name 'example.com' and managed zone domain name 'foo.example.com' do not match for zone id 'example.com'"),
					})),
				)
				g.Expect(mz.Finalizers).To(ContainElement(ManagedZoneFinalizer))
				return nil
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			//Update second managed zone to use the known existing managed zone domainName (managedZone.Spec.DomainName)
			Eventually(func(g Gomega) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(mz), mz)
				g.Expect(err).NotTo(HaveOccurred())
				mz.Spec.DomainName = managedZone.Spec.DomainName
				err = k8sClient.Update(ctx, mz)
				g.Expect(err).NotTo(HaveOccurred())
			}, TestTimeoutMedium, time.Second).Should(Succeed())

			Eventually(func(g Gomega) error {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: mz.Namespace, Name: mz.Name}, mz)
				Expect(err).NotTo(HaveOccurred())
				g.Expect(mz.Status.Conditions).To(
					ContainElement(MatchFields(IgnoreExtras, Fields{
						"Type":               Equal(string(v1alpha1.ConditionTypeReady)),
						"Status":             Equal(metav1.ConditionTrue),
						"Reason":             Equal("ProviderSuccess"),
						"ObservedGeneration": Equal(mz.Generation),
					})),
				)
				g.Expect(mz.Finalizers).To(ContainElement(ManagedZoneFinalizer))
				return nil
			}, TestTimeoutMedium, time.Second).Should(Succeed())
		})
	})
})
