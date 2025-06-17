//go:build e2e

package e2e

import (
	"context"
	"strings"
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/pkg/builder"
	. "github.com/kuadrant/dns-operator/test/e2e/helpers"
)

// Test Cases covering known provider errors that can be expected with misconfigured DNSRecord resource.
var _ = Describe("DNSRecord Provider Errors", Labels{"provider_errors"}, func() {
	// testID is a randomly generated identifier for the test
	// it is used to name resources and/or namespaces so different
	// tests can be run in parallel in the same cluster
	var testID string
	// testDomainName generated domain for this test e.g. t-e2e-12345.e2e.hcpapps.net
	var testDomainName string
	// testHostname generated hostname for this test e.g. t-gw-mgc-12345.t-e2e-12345.e2e.hcpapps.net
	var testHostname string

	var k8sClient client.Client
	var testDNSProviderSecret *v1.Secret
	var invalidProviderSecret *v1.Secret

	var dnsRecord *v1alpha1.DNSRecord

	var namespace string

	BeforeEach(func(ctx SpecContext) {
		testID = "t-errors-" + GenerateName()
		testDomainName = strings.Join([]string{testSuiteID, testZoneDomainName}, ".")
		testHostname = strings.Join([]string{testID, testDomainName}, ".")
		k8sClient = testClusters[0].k8sClient

		if testProviderSecretName != "" {
			testDNSProviderSecret = testClusters[0].testDNSProviderSecrets[0]
			namespace = testDNSProviderSecret.Namespace
		} else {
			namespace = testNamespaces[0]
			testDNSProviderSecret = &v1.Secret{}
		}
	})

	AfterEach(func(ctx SpecContext) {
		if dnsRecord != nil {
			err := k8sClient.Delete(ctx, dnsRecord,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
		if invalidProviderSecret != nil {
			err := k8sClient.Delete(ctx, invalidProviderSecret,
				client.PropagationPolicy(metav1.DeletePropagationForeground))
			Expect(client.IgnoreNotFound(err)).ToNot(HaveOccurred())
		}
	})

	It("correctly handles invalid provider credentials", func(ctx SpecContext) {
		var expectedProviderErr string
		pBuilder := builder.NewProviderBuilder("invalid-credentials", namespace).
			For(testDNSProviderSecret.Type)

		if testDNSProvider == provider.DNSProviderAWS.String() {
			// AWS
			pBuilder.WithDataItem(v1alpha1.AWSAccessKeyIDKey, "1234")
			pBuilder.WithDataItem(v1alpha1.AWSSecretAccessKeyKey, "1234")
			expectedProviderErr = "failed to list hosted zones, InvalidClientTokenId: The security token included in the request is invalid."
		} else if testDNSProvider == provider.DNSProviderGCP.String() {
			// Google
			pBuilder.WithDataItem(v1alpha1.GoogleJsonKey, "{\"client_id\": \"foo.bar.com\",\"client_secret\": \"1234\",\"refresh_token\": \"1234\",\"type\": \"authorized_user\"}")
			pBuilder.WithDataItem(v1alpha1.GoogleProjectIDKey, "foobar")
			expectedProviderErr = "oauth2: \"invalid_client\" \"The OAuth client was not found.\""
		} else if testDNSProvider == provider.DNSProviderAzure.String() {
			// Azure
			pBuilder.WithDataItem(v1alpha1.AzureJsonKey, "{}")
			Skip("not yet supported for azure")
		} else if testDNSProvider == provider.DNSProviderCoreDNS.String() {
			// CoreDNS
			pBuilder.WithDataItem("ZONES", "wrong.me")
			Skip("not yet supported for coredns")
		} else if testDNSProvider == provider.DNSProviderFromSecretGCP.String() {
			// Google from secret
			Expect(testDNSProviderSecret.Type).To(Equal(v1alpha1.SecretTypeKuadrantGCP))
			pBuilder.WithDataItem(v1alpha1.GoogleJsonKey, "{\"client_id\": \"foo.bar.com\",\"client_secret\": \"1234\",\"refresh_token\": \"1234\",\"type\": \"authorized_user\"}")
			pBuilder.WithDataItem(v1alpha1.GoogleProjectIDKey, "foobar")
			expectedProviderErr = "oauth2: \"invalid_client\" \"The OAuth client was not found.\""
		} else if testDNSProvider == provider.DNSProviderFromSecretAzure.String() {
			// Azure from secret
			Expect(testDNSProviderSecret.Type).To(Equal(v1alpha1.SecretTypeKuadrantAzure))
			pBuilder.WithDataItem(v1alpha1.AzureJsonKey, "{}")
			Skip("not yet supported for azure")
		} else if testDNSProvider == provider.DNSProviderFromSecretCoreDNS.String() {
			// CoreDNS from secret
			Expect(testDNSProviderSecret.Type).To(Equal(v1alpha1.SecretTypeKuadrantCoreDNS))
			pBuilder.WithDataItem("ZONES", "wrong.me")
			Skip("not yet supported for coredns")
		} else if testDNSProvider == provider.DNSProviderFromSecretAWS.String() {
			// AWS from secret
			Expect(testDNSProviderSecret.Type).To(Equal(v1alpha1.SecretTypeKuadrantAWS))
			pBuilder.WithDataItem(v1alpha1.AWSAccessKeyIDKey, "1234")
			pBuilder.WithDataItem(v1alpha1.AWSSecretAccessKeyKey, "1234")
			expectedProviderErr = "failed to list hosted zones, InvalidClientTokenId: The security token included in the request is invalid."
		}
		invalidProviderSecret = pBuilder.Build()
		Expect(k8sClient.Create(ctx, invalidProviderSecret)).To(Succeed())

		dnsRecord = testBuildDNSRecord(testID, invalidProviderSecret.Namespace, invalidProviderSecret.Name, testDNSProvider, "test-owner", testHostname)

		By("creating dnsrecord " + dnsRecord.Name + " with invalid provider credentials")
		err := k8sClient.Create(ctx, dnsRecord)
		Expect(err).ToNot(HaveOccurred())

		By("checking " + dnsRecord.Name + " is not ready and has the expected provider error in the status")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionFalse),
					"Message": And(ContainSubstring("Unable to find suitable zone in provider"), ContainSubstring(expectedProviderErr)),
				})),
			)
		}, recordsReadyMaxDuration, time.Second, ctx).Should(Succeed())

		By("checking dnsrecord " + dnsRecord.Name + " is not being updated repeatedly")
		tmpRecord := &v1alpha1.DNSRecord{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), tmpRecord)).To(Succeed())
		Consistently(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
			g.Expect(dnsRecord.ResourceVersion).To(Equal(tmpRecord.ResourceVersion))
		}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

	})

	It("correctly handles invalid geo", func(ctx SpecContext) {
		var validGeoCode string
		var expectedProviderErr string
		if testDNSProvider == provider.DNSProviderGCP.String() {
			//Google
			expectedProviderErr = "location': 'notageocode', invalid"
			validGeoCode = "us-east1"
		} else if testDNSProvider == provider.DNSProviderAzure.String() {
			//Azure
			expectedProviderErr = "The following locations specified in the geoMapping property for endpoint ‘foo-example-com’ are not supported: NOTAGEOCODE."
			validGeoCode = "GEO-NA"
		} else {
			//AWS or Core DNS
			expectedProviderErr = "unexpected geo code. Prefix with GEO- for continents or use ISO_3166 Alpha 2 supported code for countries"
			validGeoCode = "US"
		}

		invalidEndpoint := &externaldnsendpoint.Endpoint{
			DNSName: testHostname,
			Targets: []string{
				"foo.example.com",
			},
			RecordType:    "CNAME",
			RecordTTL:     300,
			SetIdentifier: "foo.example.com",
			ProviderSpecific: externaldnsendpoint.ProviderSpecific{
				{
					Name:  "geo-code",
					Value: "notageocode", //invalid in all providers
				},
			},
		}
		testEndpoints := []*externaldnsendpoint.Endpoint{
			invalidEndpoint,
		}

		dnsRecord = testBuildDNSRecord(testID, namespace, testDNSProviderSecret.Name, testDNSProvider, "test-owner", testHostname)
		dnsRecord.Spec.Endpoints = testEndpoints

		By("creating dnsrecord " + dnsRecord.Name + " with invalid geo endpoint")
		err := k8sClient.Create(ctx, dnsRecord)
		Expect(err).ToNot(HaveOccurred())

		By("checking " + dnsRecord.Name + " is not ready and has the expected provider error in the status")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionFalse),
					"Message": And(ContainSubstring("The DNS provider failed to ensure the record"), ContainSubstring(expectedProviderErr)),
				})),
			)
		}, recordsReadyMaxDuration, time.Second, ctx).Should(Succeed())

		By("checking dnsrecord " + dnsRecord.Name + " is not being updated repeatedly")
		tmpRecord := &v1alpha1.DNSRecord{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), tmpRecord)).To(Succeed())
		Consistently(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
			g.Expect(dnsRecord.ResourceVersion).To(Equal(tmpRecord.ResourceVersion))
		}, TestTimeoutMedium, time.Second, ctx).Should(Succeed())

		By("updating dnsrecord " + dnsRecord.Name + " with valid geo endpoint")
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			invalidEndpoint.ProviderSpecific = externaldnsendpoint.ProviderSpecific{
				{
					Name:  "geo-code",
					Value: validGeoCode, //valid for provider under test
				},
			}
			dnsRecord.Spec.Endpoints = testEndpoints
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		// TODO dig is failing in the local test setup so not getting a response from the configured nameserver

		By("checking dnsrecord " + dnsRecord.Name + " no longer has provider error")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type": Equal(string(v1alpha1.ConditionTypeReady)),
					// Since this is an e2e test we have no idea how long it might take to become ready, so we can only really
					// check that the message is one of the expected ones if it was accepted by the provider
					"Message": Or(Equal("Provider ensured the dns record"), Equal("Awaiting validation")),
				})),
			)
		}, recordsReadyMaxDuration, time.Second, ctx).Should(Succeed())

	})

	It("correctly handles invalid weight", func(ctx SpecContext) {
		var expectedProviderErr string
		if testDNSProvider == provider.DNSProviderGCP.String() {
			//Google
			expectedProviderErr = "weight': '-1.0' Reason: backendError, Message: Invalid Value"
		} else if testDNSProvider == provider.DNSProviderAzure.String() {
			//Azure
			expectedProviderErr = "Operation input is malformed. Please retry the request."
		} else if testDNSProvider == provider.DNSProviderCoreDNS.String() {
			expectedProviderErr = "invalid weight expected a value >= 0"
		} else {
			//AWS
			expectedProviderErr = "weight' failed to satisfy constraint: Member must have value greater than or equal to 0"
		}
		validWeight := "100"

		invalidEndpoint := &externaldnsendpoint.Endpoint{
			DNSName: testHostname,
			Targets: []string{
				"foo.example.com",
			},
			RecordType:    "CNAME",
			RecordTTL:     300,
			SetIdentifier: "foo.example.com",
			ProviderSpecific: externaldnsendpoint.ProviderSpecific{
				{
					Name:  "weight",
					Value: "-1",
				},
			},
		}
		testEndpoints := []*externaldnsendpoint.Endpoint{
			invalidEndpoint,
		}

		dnsRecord = testBuildDNSRecord(testID, namespace, testDNSProviderSecret.Name, testDNSProvider, "test-owner", testHostname)
		dnsRecord.Spec.Endpoints = testEndpoints

		By("creating dnsrecord " + dnsRecord.Name + " with invalid weight endpoint")
		err := k8sClient.Create(ctx, dnsRecord)
		Expect(err).ToNot(HaveOccurred())

		By("checking " + dnsRecord.Name + " is not ready and has the expected provider error in the status")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type":    Equal(string(v1alpha1.ConditionTypeReady)),
					"Status":  Equal(metav1.ConditionFalse),
					"Message": And(ContainSubstring("The DNS provider failed to ensure the record"), ContainSubstring(expectedProviderErr)),
				})),
			)
		}, recordsReadyMaxDuration, time.Second, ctx).Should(Succeed())

		By("checking dnsrecord " + dnsRecord.Name + " is not being updated repeatedly")
		tmpRecord := &v1alpha1.DNSRecord{}
		Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), tmpRecord)).To(Succeed())
		Consistently(func(g Gomega, ctx context.Context) {
			g.Expect(k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)).To(Succeed())
			g.Expect(dnsRecord.ResourceVersion).To(Equal(tmpRecord.ResourceVersion))
		}, 10*time.Second, time.Second, ctx).Should(Succeed())

		By("updating dnsrecord " + dnsRecord.Name + " with valid weight endpoint")
		Eventually(func(g Gomega) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			invalidEndpoint.ProviderSpecific = externaldnsendpoint.ProviderSpecific{
				{
					Name:  "weight",
					Value: validWeight, //valid for provider under test
				},
			}
			dnsRecord.Spec.Endpoints = testEndpoints
			err = k8sClient.Update(ctx, dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
		}, TestTimeoutMedium, time.Second).Should(Succeed())

		By("checking dnsrecord " + dnsRecord.Name + " no longer has provider error")
		Eventually(func(g Gomega, ctx context.Context) {
			err := k8sClient.Get(ctx, client.ObjectKeyFromObject(dnsRecord), dnsRecord)
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(dnsRecord.Status.Conditions).To(
				ContainElement(MatchFields(IgnoreExtras, Fields{
					"Type": Equal(string(v1alpha1.ConditionTypeReady)),
					// Since this is an e2e test we have no idea how long it might take to become ready, so we can only really
					// check that the message is one of the expected ones if it was accepted by the provider
					"Message": Or(Equal("Provider ensured the dns record"), Equal("Awaiting validation")),
				})),
			)
		}, recordsReadyMaxDuration, time.Second, ctx).Should(Succeed())

	})

})

// testBuildDNSRecord creates a valid dnsrecord with a single valid endpoint
func testBuildDNSRecord(name, ns, dnsProviderSecretName, dnsProviderName, ownerID, rootHost string) *v1alpha1.DNSRecord {
	var providerRefObj *v1alpha1.ProviderRef
	var providerObj *v1alpha1.Provider
	if dnsProviderSecretName != "" {
		providerRefObj = &v1alpha1.ProviderRef{
			Name: testProviderSecretName,
		}
	} else {
		providerObj = &v1alpha1.Provider{
			Name: dnsProviderName,
		}
	}

	return &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: ns,
		},
		Spec: v1alpha1.DNSRecordSpec{
			OwnerID:     ownerID,
			RootHost:    rootHost,
			ProviderRef: providerRefObj,
			Provider:    providerObj,
			Endpoints: []*externaldnsendpoint.Endpoint{
				{
					DNSName: rootHost,
					Targets: []string{
						"127.0.0.1",
					},
					RecordType: "A",
					RecordTTL:  60,
				},
			},
		},
	}
}
