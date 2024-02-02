//go:build integration

package controller

import (
	"context"
	"fmt"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
)

var _ = Describe("DNSHealthCheckProbe controller", func() {
	const (
		ProbeName      = "test-probe"
		ProbeNamespace = "default"

		timeout  = time.Second * 10
		duration = time.Second * 10
		interval = time.Millisecond * 250
	)

	Context("When creating DNSHealthCheckProbe", func() {
		It("Should update health status to healthy", func() {
			By("Performing health check")

			ctx := context.Background()
			probeObj := &v1alpha1.DNSHealthCheckProbe{
				ObjectMeta: metav1.ObjectMeta{
					Name:      ProbeName,
					Namespace: ProbeNamespace,
				},
				Spec: v1alpha1.DNSHealthCheckProbeSpec{
					Host:     "localhost",
					Address:  "0.0.0.0",
					Port:     3333,
					Interval: metav1.Duration{Duration: time.Second * 10},
					Path:     "/healthy",
				},
			}

			Expect(k8sClient.Create(ctx, probeObj)).Should(Succeed())

			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(probeObj), probeObj)
				if err != nil {
					return err
				}
				if probeObj.Status.LastCheckedAt.Time == (time.Time{}) {
					return fmt.Errorf("expected probeObj.Status.LastCheckedAt to be non-zero %s, %s", probeObj.Status.LastCheckedAt.Time, (metav1.Time{}).Time)
				}
				return nil
			}, timeout+(time.Second*20), interval).Should(BeNil())

			GinkgoWriter.Print(probeObj)

			Expect(*probeObj.Status.Healthy).Should(BeTrue())
			Expect(probeObj.Status.LastCheckedAt).Should(Not(BeZero()))
		})
		It("Should update health status to unhealthy", func() {
			By("Updating to unhealthy endpoint")

			ctx := context.Background()
			probeObj := &v1alpha1.DNSHealthCheckProbe{}

			err := k8sClient.Get(ctx, client.ObjectKey{
				Name:      ProbeName,
				Namespace: ProbeNamespace,
			}, probeObj)
			Expect(err).NotTo(HaveOccurred())

			patch := client.MergeFrom(probeObj.DeepCopy())
			lastUpdate := probeObj.Status.LastCheckedAt
			probeObj.Spec.Path = "/unhealthy"
			Expect(k8sClient.Patch(ctx, probeObj, patch)).To(BeNil())

			Eventually(func() error {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(probeObj), probeObj)
				if err != nil {
					return err
				}
				if !probeObj.Status.LastCheckedAt.Time.After(lastUpdate.Time) {
					return fmt.Errorf("expected probeObj.Status.LastCheckedAt to be after lastUpdate")
				}
				return nil
			}, timeout+(time.Second*20), interval).Should(BeNil())

			Expect(*probeObj.Status.Healthy).Should(BeFalse())
			Expect(probeObj.Status.Reason).Should(Equal("Status code: 500"))
		})
	})
})
