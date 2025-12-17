//go:build integration

package controller

import (
	"context"
	"crypto/rand"
	"fmt"
	"math/big"
	"path/filepath"
	"strings"
	"time"

	"github.com/goombaio/namegenerator"
	. "github.com/onsi/gomega"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/utils/ptr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/config"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	externalinmemory "github.com/kuadrant/dns-operator/internal/external-dns/provider/inmemory"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/internal/provider/inmemory"
	"github.com/kuadrant/dns-operator/pkg/builder"
	"github.com/kuadrant/dns-operator/types"
)

const (
	TestTimeoutShort          = time.Second * 5
	TestTimeoutMedium         = time.Second * 15
	TestTimeoutLong           = time.Second * 30
	TestRetryIntervalMedium   = time.Millisecond * 250
	RequeueDuration           = time.Second * 2
	ValidityDuration          = time.Second * 2
	DefaultValidationDuration = time.Second * 1
)

func GenerateName() string {
	nBig, _ := rand.Int(rand.Reader, big.NewInt(1000000))
	return namegenerator.NewNameGenerator(nBig.Int64()).Generate()
}

func getTypeMeta() metav1.TypeMeta {
	gvk := v1alpha1.GroupVersion.WithKind("DNSRecord")
	return metav1.TypeMeta{
		Kind:       gvk.Kind,
		APIVersion: gvk.GroupVersion().String(),
	}
}

type TestEndpoints struct {
	endpoints []*externaldnsendpoint.Endpoint
}

func NewTestEndpoints(dnsName string) *TestEndpoints {
	return &TestEndpoints{
		endpoints: []*externaldnsendpoint.Endpoint{
			{
				DNSName:          dnsName,
				Targets:          []string{"127.0.0.1"},
				RecordType:       "A",
				SetIdentifier:    "foo",
				RecordTTL:        60,
				Labels:           nil,
				ProviderSpecific: nil,
			},
		},
	}
}

func (te *TestEndpoints) WithTargets(targets []string) *TestEndpoints {
	te.endpoints[0].Targets = targets
	return te
}

func (te *TestEndpoints) WithTTL(ttl externaldnsendpoint.TTL) *TestEndpoints {
	te.endpoints[0].RecordTTL = ttl
	return te
}

func (te *TestEndpoints) Endpoints() []*externaldnsendpoint.Endpoint {
	return te.endpoints
}

func getTestHealthCheckSpec() *v1alpha1.HealthCheckSpec {
	return &v1alpha1.HealthCheckSpec{
		Path:             "/healthz",
		Port:             443,
		Protocol:         "HTTPS",
		FailureThreshold: 5,
		Interval:         &metav1.Duration{Duration: time.Minute},
		AdditionalHeadersRef: &v1alpha1.AdditionalHeadersRef{
			Name: "headers",
		},
	}
}

// InMemoryTXTResolver implements TXTResolver by reading from the inmemory provider
type InMemoryTXTResolver struct {
	client *externalinmemory.InMemoryClient
}

func (r *InMemoryTXTResolver) LookupTXT(_ context.Context, host string, nameservers []string) ([]string, error) {
	// nameservers parameter is ignored for inmemory provider
	// Find the zone for this host
	zones := r.client.Zones()
	var zoneID string
	var matchZoneName string

	for id, zoneName := range zones {
		if strings.HasSuffix(host, zoneName) && len(zoneName) > len(matchZoneName) {
			matchZoneName = zoneName
			zoneID = id
		}
	}

	if zoneID == "" {
		return []string{}, nil
	}

	// Get all records from the zone
	records, err := r.client.Records(zoneID)
	if err != nil {
		return []string{}, err
	}

	// Find TXT records matching the host
	for _, record := range records {
		if record.DNSName == host && record.RecordType == "TXT" {
			return record.Targets, nil
		}
	}

	return []string{}, nil
}

// setActiveGroupsInDNS sets active groups via TXT record in the inmemory provider
func setActiveGroupsInDNS(ctx context.Context, zoneDomain string, groups types.Groups) error {
	memClient := inmemory.GetInMemoryClient()

	activeGroupsHost := activeGroupsTXTRecordName + "." + zoneDomain

	// Create the active groups value
	groupsStr := ""
	if len(groups) > 0 {
		groupNames := []string{}
		for _, g := range groups {
			groupNames = append(groupNames, string(g))
		}
		groupsStr = "groups=" + strings.Join(groupNames, "&&")
	}

	// Get existing records
	records, err := memClient.Records(zoneDomain)
	if err != nil {
		return err
	}

	// Check if active groups TXT record exists
	var existingRecord *externaldnsendpoint.Endpoint
	for _, record := range records {
		if record.DNSName == activeGroupsHost && record.RecordType == "TXT" {
			existingRecord = record
			break
		}
	}

	changes := &plan.Changes{}

	if len(groups) == 0 {
		// Delete the active groups record if it exists
		if existingRecord != nil {
			changes.Delete = append(changes.Delete, existingRecord)
		}
	} else {
		newRecord := &externaldnsendpoint.Endpoint{
			DNSName:    activeGroupsHost,
			Targets:    []string{groupsStr},
			RecordType: "TXT",
			RecordTTL:  60,
		}

		if existingRecord != nil {
			// Update existing record
			changes.UpdateOld = append(changes.UpdateOld, existingRecord)
			changes.UpdateNew = append(changes.UpdateNew, newRecord)
		} else {
			// Create new record
			changes.Create = append(changes.Create, newRecord)
		}
	}

	if changes.HasChanges() {
		return memClient.ApplyChanges(ctx, zoneDomain, changes)
	}

	return nil
}

// setupGroupEnv creates a test environment with a controller configured with a specific group
func setupGroupEnv(ctx context.Context, delegationRole string, group types.Group, count int) (*envtest.Environment, ctrl.Manager) {
	testEnv := &envtest.Environment{
		CRDDirectoryPaths:     []string{filepath.Join("..", "..", "config", "crd", "bases")},
		ErrorIfCRDPathMissing: true,
	}

	var err error
	var cfg *rest.Config

	cfg, err = testEnv.Start()
	Expect(err).NotTo(HaveOccurred())
	Expect(cfg).NotTo(BeNil())

	err = v1alpha1.AddToScheme(scheme.Scheme)
	Expect(err).NotTo(HaveOccurred())

	dynClient, err := dynamic.NewForConfig(cfg)
	Expect(err).ToNot(HaveOccurred())
	Expect(dynClient).NotTo(BeNil())

	var mgr ctrl.Manager

	defaultOptions := ctrl.Options{
		Scheme:                 scheme.Scheme,
		HealthProbeBindAddress: "0",
		Metrics:                metricsserver.Options{BindAddress: "0"},
		Controller: config.Controller{
			SkipNameValidation: ptr.To(true),
		},
		Logger: ctrl.LoggerFrom(ctx).WithName(fmt.Sprintf("%s-%v-group-%s", delegationRole, count, group)),
	}

	// Use the normal controller runtime manager - no multi-cluster for now
	mgr, err = ctrl.NewManager(cfg, defaultOptions)
	Expect(err).ToNot(HaveOccurred())
	Expect(mgr).ToNot(BeNil())

	providerFactory, err := provider.NewFactory(mgr.GetClient(), dynClient, []string{provider.DNSProviderInMem.String()}, nil)
	Expect(err).ToNot(HaveOccurred())
	Expect(providerFactory).ToNot(BeNil())

	// Create InMemoryTXTResolver that uses the global inmemory client
	txtResolver := &InMemoryTXTResolver{client: inmemory.GetInMemoryClient()}

	dnsRecordController := &DNSRecordReconciler{
		Client: mgr.GetClient(),
		BaseDNSRecordReconciler: BaseDNSRecordReconciler{
			Scheme:          mgr.GetScheme(),
			ProviderFactory: providerFactory,
			DelegationRole:  delegationRole,
			Group:           group, // Set the group for this controller
			TXTResolver:     txtResolver,
		},
	}

	err = dnsRecordController.SetupWithManager(mgr, RequeueDuration, ValidityDuration, DefaultValidationDuration, true, true)
	Expect(err).ToNot(HaveOccurred())

	return testEnv, mgr
}

// createDefaultDNSProviderSecret creates an inmemory DNS provider secret with default provider label
func createDefaultDNSProviderSecret(ctx context.Context, namespace, zoneDomainName string, k8sClient client.Client) *v1.Secret {
	secret := builder.NewProviderBuilder("inmemory-credentials", namespace).
		For(v1alpha1.SecretTypeKuadrantInmemory).
		WithZonesInitialisedFor(zoneDomainName).
		Build()
	labels := secret.GetLabels()
	if labels == nil {
		labels = map[string]string{}
	}
	labels[v1alpha1.DefaultProviderSecretLabel] = "true"
	secret.SetLabels(labels)
	Expect(k8sClient.Create(ctx, secret)).To(Succeed())
	return secret
}

// createTestDNSRecord creates a DNSRecord with the given parameters
func createTestDNSRecord(name, namespace, hostname, target string) *v1alpha1.DNSRecord {
	return &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: v1alpha1.DNSRecordSpec{
			RootHost: hostname,
			Endpoints: []*externaldnsendpoint.Endpoint{
				{
					DNSName:    hostname,
					Targets:    []string{target},
					RecordType: "A",
					RecordTTL:  60,
				},
			},
		},
	}
}
