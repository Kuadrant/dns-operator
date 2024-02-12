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
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	//+kubebuilder:scaffold:imports
)

var _ = Describe("ManagedZoneReconciler", func() {
	Context("testing ManagedZone controller", func() {
		var managedZone *v1alpha1.ManagedZone
		var ctx context.Context

		BeforeEach(func() {
			ctx = context.Background()
			managedZone = &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "example.com",
					Namespace: defaultNS,
				},
				Spec: v1alpha1.ManagedZoneSpec{
					ID:         "example.com",
					DomainName: "example.com",
					SecretRef: v1alpha1.ProviderRef{
						Name: providerCredential,
					},
				},
			}
		})

		AfterEach(func() {
			// Clean up managedZones
			mzList := &v1alpha1.ManagedZoneList{}
			err := k8sClient.List(ctx, mzList, client.InNamespace(defaultNS))
			Expect(err).NotTo(HaveOccurred())
			for _, mz := range mzList.Items {
				err = k8sClient.Delete(ctx, &mz)
				Expect(client.IgnoreNotFound(err)).NotTo(HaveOccurred())
			}
		})

		It("should accept a managed zone for this controller and allow deletion", func() {
			Expect(k8sClient.Create(ctx, managedZone)).To(BeNil())

			createdMZ := &v1alpha1.ManagedZone{}

			Eventually(func() error {
				return k8sClient.Get(ctx, client.ObjectKey{Namespace: managedZone.Namespace, Name: managedZone.Name}, createdMZ)
			}, TestTimeoutMedium, TestRetryIntervalMedium).ShouldNot(HaveOccurred())

			Expect(k8sClient.Delete(ctx, managedZone)).To(BeNil())

			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKey{Namespace: managedZone.Namespace, Name: managedZone.Name}, createdMZ)
				if err != nil && !errors.IsNotFound(err) {
					return err
				}
				return nil
			}, TestTimeoutMedium, TestRetryIntervalMedium).Should(BeNil())
		})

		It("should reject a managed zone with an invalid domain name", func() {
			invalidDomainNameManagedZone := &v1alpha1.ManagedZone{
				ObjectMeta: metav1.ObjectMeta{
					Name:      "invalid_domain",
					Namespace: defaultNS,
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
	})
})
