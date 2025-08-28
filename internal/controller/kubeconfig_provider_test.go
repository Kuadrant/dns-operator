//go:build integration

package controller

import (
	"fmt"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/onsi/gomega/gbytes"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

var _ = Describe("Kubeconfig Provider", Labels{"multicluster"}, func() {

	var (
		secondaryClusterSecret *v1.Secret

		// buffer containing all the log entries added during the current spec execution
		// can be used in conjunction with `gbytes.Say` matcher to check log entries see https://onsi.github.io/gomega/#codegbytescode-testing-streaming-buffers
		logBuffer *gbytes.Buffer
	)

	BeforeEach(func() {
		logBuffer = gbytes.NewBuffer()
		GinkgoWriter.TeeTo(logBuffer)

		secondaryClusterSecret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secondary-cluster-1",
				Namespace: testDefaultClusterSecretNamespace,
				Labels: map[string]string{
					testDefaultClusterSecretLabel: "true",
				},
			},
			StringData: map[string]string{
				"kubeconfig": string(secondaryKubeconfig),
			},
		}
	})

	AfterEach(func() {
		if secondaryClusterSecret != nil {
			Expect(client.IgnoreNotFound(primaryK8sClient.Delete(ctx, secondaryClusterSecret))).To(Succeed())
		}
		GinkgoWriter.ClearTeeWriters()
	})

	It("successfully establishes a connection to a remote cluster when valid kubeconfig secret is created", Labels{"primary"}, func(ctx SpecContext) {
		createClusterKubeconfigSecret(primaryK8sClient, secondaryClusterSecret, logBuffer)
	})

	It("successfully closes connection to the remote cluster when kubeconfig secret is deleted", Labels{"primary"}, func(ctx SpecContext) {
		createClusterKubeconfigSecret(primaryK8sClient, secondaryClusterSecret, logBuffer)
		deleteClusterKubeconfigSecret(primaryK8sClient, secondaryClusterSecret, logBuffer)
	})

	It("fails to establishes a connection to a remote cluster when invalid kubeconfig secret is created", Labels{"primary"}, func(ctx SpecContext) {
		invalidSecret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "secondary-cluster-1",
				Namespace: testDefaultClusterSecretNamespace,
				Labels: map[string]string{
					testDefaultClusterSecretLabel: "true",
				},
			},
			StringData: map[string]string{
				"kubeconfig": "",
			},
		}
		Expect(primaryK8sClient.Create(ctx, invalidSecret)).To(Succeed())
		Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("Secret does not contain kubeconfig data, skipping\\s*{\"cluster\": \"%s\", \"secret\": \"%s\\/%s\", \"key\": \"kubeconfig\"}", invalidSecret.Name, invalidSecret.Namespace, invalidSecret.Name)))
	})

	It("triggers reconcile of secondary cluster record on primary", Labels{"primary", "secondary"}, func(ctx SpecContext) {
		createClusterKubeconfigSecret(primaryK8sClient, secondaryClusterSecret, logBuffer)

		testNamespace := generateTestNamespaceName()
		CreateNamespace(testNamespace, secondaryK8sClient)

		testZoneDomainName := strings.Join([]string{GenerateName(), "example.com"}, ".")
		testHostname := strings.Join([]string{"foo", testZoneDomainName}, ".")

		secondaryRecord := &v1alpha1.DNSRecord{
			ObjectMeta: metav1.ObjectMeta{
				Name:      testHostname,
				Namespace: testNamespace,
			},
			Spec: v1alpha1.DNSRecordSpec{
				RootHost:  testHostname,
				Endpoints: getTestEndpoints(testHostname, []string{"127.0.0.1"}),
			},
		}
		Expect(secondaryK8sClient.Create(ctx, secondaryRecord)).To(Succeed())
		Eventually(func(g Gomega) {
			err := secondaryK8sClient.Get(ctx, client.ObjectKeyFromObject(secondaryRecord), secondaryRecord)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutShort, time.Second).Should(Succeed())

		//Verify the secondary log contains the expected statements
		Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("secondary.dnsrecord_controller\\s*Reconciled DNSRecord %s from namespace %s", secondaryRecord.Name, secondaryRecord.Namespace)))
		//Example: 2025-08-26T17:55:11+01:00     INFO    secondary.dnsrecord_controller  Reconciled DNSRecord foo.hidden-voice.example.com from namespace test-namespace-a874ca2a-19b1-4e38-b040-bb357df7dd8e in 7.031572ms      {"controller": "dnsrecord", "controllerGroup": "kuadrant.io", "controllerKind": "DNSRecord", "DNSRecord": {"name":"foo.hidden-voice.example.com","namespace":"test-namespace-a874ca2a-19b1-4e38-b040-bb357df7dd8e"}, "namespace": "test-namespace-a874ca2a-19b1-4e38-b040-bb357df7dd8e", "name": "foo.hidden-voice.example.com", "reconcileID": "2d512957-50bf-4c75-a701-0f7b4f409ae3"}

		//Verify the primary log contains the expected statements
		Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("primary.remote_dnsrecord_controller\\s+Remote Reconcile\\s+{\"controller\": \"remote-dnsrecord-controller\".+\"cluster\": \"%s\".+\"req\": \"cluster:\\/\\/%s\\/%s\\/%s\"", secondaryClusterSecret.Name, secondaryClusterSecret.Name, secondaryRecord.Namespace, secondaryRecord.Name)))
		//Example: 2025-08-26T17:55:11+01:00     INFO    primary.remote_dnsrecord_controller     Remote Reconcile        {"controller": "remote-dnsrecord-controller", "controllerGroup": "kuadrant.io", "controllerKind": "DNSRecord", "reconcileID": "728d600c-12b8-4650-9fee-baf635636bad", "cluster": "secondary-cluster-1", "req": "cluster://secondary-cluster-1/test-namespace-a874ca2a-19b1-4e38-b040-bb357df7dd8e/foo.hidden-voice.example.com"}
	})

})

func createClusterKubeconfigSecret(k8sClient client.Client, secret *v1.Secret, logBuffer *gbytes.Buffer) {
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("Creating new cluster from kubeconfig\\s*{\"cluster\": \"%s\", \"secret\": \"%s\\/%s\"}", secret.Name, secret.Namespace, secret.Name)))
	Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("Successfully added cluster\\s*{\"cluster\": \"%s\", \"secret\": \"%s\\/%s\"}", secret.Name, secret.Namespace, secret.Name)))
	Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("Successfully engaged manager\\s*{\"cluster\": \"%s\", \"secret\": \"%s\\/%s\"}", secret.Name, secret.Namespace, secret.Name)))
}

func deleteClusterKubeconfigSecret(k8sClient client.Client, secret *v1.Secret, logBuffer *gbytes.Buffer) {
	Expect(k8sClient.Delete(ctx, secret)).To(Succeed())
	Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("Removing cluster\\s*{\"cluster\": \"%s\"}", secret.Name)))
	Eventually(logBuffer).Should(gbytes.Say(fmt.Sprintf("Successfully removed cluster and cancelled cluster context\\s*{\"cluster\": \"%s\"}", secret.Name)))
}
