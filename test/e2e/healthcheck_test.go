//go:build e2e

package e2e

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	appsv1 "k8s.io/api/apps/v1"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

// Helper function to create a DNSRecord with health check configuration
func createDNSRecordWithHealthCheck(testID string, namespace string, hostname string, targetIP string, port int, failureThreshold int, interval *metav1.Duration) *v1alpha1.DNSRecord {
	dnsRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testID,
			Namespace: namespace,
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: hostname,
			ProviderRef: &v1alpha1.ProviderRef{
				Name: testProviderSecretName,
			},
			Endpoints: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    hostname,
					Targets:    []string{targetIP},
					RecordType: "A",
					RecordTTL:  60,
				},
			},
			HealthCheck: &v1alpha1.HealthCheckSpec{
				Path:             "/healthz",
				Port:             port,
				Protocol:         v1alpha1.HttpProtocol,
				FailureThreshold: failureThreshold,
				Interval:         interval,
			},
		},
	}
	return dnsRecord
}

// Helper function to wait for a DNSHealthCheckProbe to be created
func waitForProbeCreation(ctx context.Context, k8sClient client.Client, probeName, namespace string) *v1alpha1.DNSHealthCheckProbe {
	probe := &v1alpha1.DNSHealthCheckProbe{}
	Eventually(func(g Gomega, ctx context.Context) {
		err := k8sClient.Get(ctx, client.ObjectKey{
			Name:      probeName,
			Namespace: namespace,
		}, probe)
		g.Expect(err).ToNot(HaveOccurred())
	}, TestTimeoutLong, time.Second, ctx).Should(Succeed())
	return probe
}

// Helper function to verify probe spec configuration
func verifyProbeSpec(probe *v1alpha1.DNSHealthCheckProbe, targetIP, hostname string, port, failureThreshold int) {
	Expect(probe.Spec.Address).To(Equal(targetIP))
	Expect(probe.Spec.Hostname).To(Equal(hostname))
	Expect(probe.Spec.Port).To(Equal(port))
	Expect(probe.Spec.Protocol).To(Equal(v1alpha1.HttpProtocol))
	Expect(probe.Spec.FailureThreshold).To(Equal(failureThreshold))
}

// Helper function to deploy an in-cluster HTTP server pod and service
func deployInClusterHTTPServer(ctx context.Context, k8sClient client.Client, testID, namespace string, port int) string {
	By("operator is deployed in-cluster, using ClusterIP service")

	// Create a pod that runs a simple HTTP server responding to health checks
	testPod := &v1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testID + "-server",
			Namespace: namespace,
			Labels: map[string]string{
				"app": testID,
			},
		},
		Spec: v1.PodSpec{
			Containers: []v1.Container{
				{
					Name:  "httpd",
					Image: "registry.k8s.io/e2e-test-images/agnhost:2.43",
					Args: []string{
						"netexec",
						"--http-port", fmt.Sprintf("%d", port),
					},
					Ports: []v1.ContainerPort{
						{
							ContainerPort: int32(port),
							Protocol:      v1.ProtocolTCP,
						},
					},
					ReadinessProbe: &v1.Probe{
						ProbeHandler: v1.ProbeHandler{
							HTTPGet: &v1.HTTPGetAction{
								Path: "/",
								Port: intstr.FromInt(port),
							},
						},
						InitialDelaySeconds: 1,
						PeriodSeconds:       1,
					},
				},
			},
		},
	}

	By("creating test HTTP server pod " + testPod.Name)
	err := k8sClient.Create(ctx, testPod)
	Expect(err).ToNot(HaveOccurred())

	// Cleanup pod after test
	DeferCleanup(func(ctx SpecContext) {
		By("deleting test HTTP server pod " + testPod.Name)
		err := k8sClient.Delete(ctx, testPod, client.PropagationPolicy(metav1.DeletePropagationForeground))
		Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
	})

	// Create a service to expose the pod
	testService := &v1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      testID + "-service",
			Namespace: namespace,
		},
		Spec: v1.ServiceSpec{
			Selector: map[string]string{
				"app": testID,
			},
			Ports: []v1.ServicePort{
				{
					Port:       int32(port),
					TargetPort: intstr.FromInt(port),
					Protocol:   v1.ProtocolTCP,
				},
			},
			Type: v1.ServiceTypeClusterIP,
		},
	}

	By("creating test service " + testService.Name)
	err = k8sClient.Create(ctx, testService)
	Expect(err).ToNot(HaveOccurred())

	// Cleanup service after test
	DeferCleanup(func(ctx SpecContext) {
		By("deleting test service " + testService.Name)
		err := k8sClient.Delete(ctx, testService, client.PropagationPolicy(metav1.DeletePropagationForeground))
		Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
	})

	// Wait for the pod to be ready
	By("waiting for test HTTP server pod to be ready")
	Eventually(func(g Gomega, ctx context.Context) {
		err := k8sClient.Get(ctx, client.ObjectKeyFromObject(testPod), testPod)
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(testPod.Status.Phase).To(Equal(v1.PodRunning))
		var podReady bool
		for _, condition := range testPod.Status.Conditions {
			if condition.Type == v1.PodReady {
				podReady = true
				g.Expect(condition.Status).To(Equal(v1.ConditionTrue))
				break
			}
		}
		g.Expect(podReady).To(BeTrue(), "Pod ready condition not found")
	}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

	// Get the service ClusterIP
	By("getting service ClusterIP")
	err = k8sClient.Get(ctx, client.ObjectKeyFromObject(testService), testService)
	Expect(err).ToNot(HaveOccurred())
	testTargetIP := testService.Spec.ClusterIP
	Expect(testTargetIP).ToNot(BeEmpty(), "Service ClusterIP should not be empty")
	return testTargetIP
}

// Helper function to start a local HTTP server
func startLocalHTTPServer(port int) string {
	By("operator is running locally, using localhost HTTP server")
	testTargetIP := "127.0.0.1"

	// Start a local HTTP server that responds to health checks
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" || r.URL.Path == "/" {
			w.WriteHeader(http.StatusOK)
			fmt.Fprint(w, "OK")
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})

	server := &http.Server{
		Addr:    fmt.Sprintf("%s:%d", testTargetIP, port),
		Handler: handler,
	}

	// Start the server in a goroutine
	go func() {
		_ = server.ListenAndServe()
	}()

	// Ensure server is shut down after the test
	DeferCleanup(func(ctx SpecContext) {
		By("shutting down local HTTP server")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = server.Shutdown(shutdownCtx)
	})

	// Give the server a moment to start and verify it's responding
	time.Sleep(500 * time.Millisecond)
	By("verifying local HTTP server is responding")
	resp, err := http.Get(fmt.Sprintf("http://%s:%d/healthz", testTargetIP, port))
	Expect(err).ToNot(HaveOccurred())
	Expect(resp.StatusCode).To(Equal(http.StatusOK))
	resp.Body.Close()

	return testTargetIP
}

// Helper function to set up a test HTTP server (in-cluster or local)
func setupTestHTTPServer(ctx context.Context, k8sClient client.Client, testID, namespace string, port int) (string, error) {
	// Check if the operator is deployed and running in-cluster
	deployment := &appsv1.Deployment{}
	err := k8sClient.Get(ctx, client.ObjectKey{
		Name:      "dns-operator-controller-manager",
		Namespace: "dns-operator-system",
	}, deployment)

	// Operator is considered deployed only if it exists AND has available replicas
	operatorDeployed := err == nil && deployment.Status.AvailableReplicas > 0

	if operatorDeployed {
		return deployInClusterHTTPServer(ctx, k8sClient, testID, namespace, port), nil
	}

	return startLocalHTTPServer(port), nil
}

// Test Cases covering multiple creation and deletion of health checks
var _ = Describe("Health Check Test", Serial, Labels{"health_checks"}, func() {
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var k8sClient client.Client
	var testDNSProviderSecret *v1.Secret

	var dnsRecord *v1alpha1.DNSRecord

	BeforeEach(func() {
		testID = "t-health-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		k8sClient = testClusters[0].k8sClient
		testDNSProviderSecret = testClusters[0].testDNSProviderSecrets[0]
		SetTestEnv("testID", testID)
		SetTestEnv("testHostname", testHostname)
		SetTestEnv("testNamespace", testDNSProviderSecret.Namespace)
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord != nil {
			By("deleting dnsrecord " + dnsRecord.Name)
			err := k8sClient.Delete(ctx, dnsRecord,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	Context("On-cluster healthchecks", func() {
		It("should set healthy status to false when health check fails", func(ctx SpecContext) {
			testTargetIP := "127.0.0.1"
			testPort := 9999 // Using a port that will fail (nothing listening)
			testHostname := testID + "." + testZoneDomainName

			dnsRecord = createDNSRecordWithHealthCheck(
				testID,
				testDNSProviderSecret.Namespace,
				testHostname,
				testTargetIP,
				testPort,
				1,
				&metav1.Duration{Duration: 5 * time.Second},
			)

			By("creating dnsrecord " + dnsRecord.Name)
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			By("checking that DNSHealthCheckProbe is created")
			probeName := testID + "-" + testTargetIP
			probe := waitForProbeCreation(ctx, k8sClient, probeName, dnsRecord.Namespace)

			By("verifying probe spec is correctly configured")
			verifyProbeSpec(probe, testTargetIP, testHostname, testPort, 1)

			By("waiting for probe to fail and set healthy status to false")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(probe), probe)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(probe.Status.Healthy).ToNot(BeNil())
				g.Expect(*probe.Status.Healthy).To(BeFalse(), "Expected probe healthy status to be false")
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("verifying consecutive failures are tracked")
			Expect(probe.Status.ConsecutiveFailures).To(BeNumerically(">", 1))
			Expect(probe.Status.Reason).ToNot(BeEmpty(), "Expected reason to be set on failed probe")
		})

		It("should track consecutive failures with custom threshold and interval", func(ctx SpecContext) {
			testTargetIP := "127.0.0.1"
			testPort := 9998 // Using a port that will fail (nothing listening)
			testHostname := testID + "." + testZoneDomainName

			dnsRecord = createDNSRecordWithHealthCheck(
				testID,
				testDNSProviderSecret.Namespace,
				testHostname,
				testTargetIP,
				testPort,
				5,
				&metav1.Duration{Duration: 5 * time.Second},
			)

			By("creating dnsrecord " + dnsRecord.Name)
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			By("checking that DNSHealthCheckProbe is created")
			probeName := testID + "-" + testTargetIP
			probe := waitForProbeCreation(ctx, k8sClient, probeName, dnsRecord.Namespace)

			By("verifying probe spec is correctly configured")
			verifyProbeSpec(probe, testTargetIP, testHostname, testPort, 5)
			Expect(probe.Spec.Interval.Duration).To(Equal(5 * time.Second))

			By("waiting for consecutive failures to accumulate")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(probe), probe)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(probe.Status.ConsecutiveFailures).To(BeNumerically(">=", 6))
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("verifying probe is not marked unhealthy until threshold is exceeded")
			// Wait a bit to accumulate some failures, but not enough to exceed threshold
			time.Sleep(12 * time.Second) // Should have 2-3 failures by now
			err = k8sClient.Get(ctx, client.ObjectKeyFromObject(probe), probe)
			Expect(err).ToNot(HaveOccurred())

			By("waiting for probe to fail after exceeding threshold of 5")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(probe), probe)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(probe.Status.Healthy).ToNot(BeNil())
				g.Expect(*probe.Status.Healthy).To(BeFalse(), "Expected probe healthy status to be false after exceeding threshold of 5")
				g.Expect(probe.Status.ConsecutiveFailures).To(BeNumerically(">", 5))
			}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

			By("verifying failure reason is set")
			Expect(probe.Status.Reason).ToNot(BeEmpty(), "Expected reason to be set on failed probe")
		})

		It("should set healthy status to true when health check passes", func(ctx SpecContext) {
			testPort := 18080
			testHostname := testID + "." + testZoneDomainName

			testTargetIP, err := setupTestHTTPServer(ctx, k8sClient, testID, testDNSProviderSecret.Namespace, testPort)
			Expect(err).ToNot(HaveOccurred())

			dnsRecord = createDNSRecordWithHealthCheck(
				testID,
				testDNSProviderSecret.Namespace,
				testHostname,
				testTargetIP,
				testPort,
				1,
				nil,
			)

			By("creating dnsrecord " + dnsRecord.Name)
			err = k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			By("checking that DNSHealthCheckProbe is created")
			probeName := testID + "-" + testTargetIP
			probe := waitForProbeCreation(ctx, k8sClient, probeName, dnsRecord.Namespace)

			By("verifying probe spec is correctly configured")
			verifyProbeSpec(probe, testTargetIP, testHostname, testPort, 1)

			By("waiting for probe to succeed and set healthy status to true")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKeyFromObject(probe), probe)
				g.Expect(err).ToNot(HaveOccurred())
				g.Expect(probe.Status.Healthy).ToNot(BeNil())
				g.Expect(*probe.Status.Healthy).To(BeTrue(), "Expected probe healthy status to be true")
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("verifying consecutive failures is 0")
			Expect(probe.Status.ConsecutiveFailures).To(Equal(0))
		})

		It("should delete health check probes when DNSRecord is deleted", func(ctx SpecContext) {
			testTargetIP := "127.0.0.1"
			testPort := 9999 // Using a port that will fail (nothing listening)
			testHostname := testID + "." + testZoneDomainName

			dnsRecord = createDNSRecordWithHealthCheck(
				testID,
				testDNSProviderSecret.Namespace,
				testHostname,
				testTargetIP,
				testPort,
				1,
				nil,
			)

			By("creating dnsrecord " + dnsRecord.Name)
			err := k8sClient.Create(ctx, dnsRecord)
			Expect(err).ToNot(HaveOccurred())

			// Verify probe is created
			By("checking that DNSHealthCheckProbe is created")
			probeName := testID + "-" + testTargetIP
			probe := waitForProbeCreation(ctx, k8sClient, probeName, dnsRecord.Namespace)

			By("verifying probe spec is correctly configured")
			verifyProbeSpec(probe, testTargetIP, testHostname, testPort, 1)

			// Delete the DNSRecord
			By("deleting dnsrecord " + dnsRecord.Name)
			err = k8sClient.Delete(ctx, dnsRecord, client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(err).ToNot(HaveOccurred())

			// Verify the probe is deleted
			By("verifying DNSHealthCheckProbe is automatically deleted when DNSRecord is deleted")
			Eventually(func(g Gomega, ctx context.Context) {
				err := k8sClient.Get(ctx, client.ObjectKey{
					Name:      probeName,
					Namespace: dnsRecord.Namespace,
				}, probe)
				g.Expect(err).To(HaveOccurred())
				g.Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred(), "Probe should be deleted, not erroring")
			}, TestTimeoutLong, time.Second, ctx).Should(Succeed())

			By("verifying no health check probes remain in the namespace")
			probeList := &v1alpha1.DNSHealthCheckProbeList{}
			err = k8sClient.List(ctx, probeList, &client.ListOptions{
				Namespace: testDNSProviderSecret.Namespace,
			})
			Expect(err).ToNot(HaveOccurred())
			Expect(probeList.Items).To(BeEmpty(), "No health check probes should remain after DNSRecord deletion")

			// Set dnsRecord to nil so AfterEach doesn't try to delete it again
			dnsRecord = nil
		})
	})
})
