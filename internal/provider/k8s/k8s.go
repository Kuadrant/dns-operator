package k8s

import (
	"context"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/tools/clientcmd"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/provider"
)

const (
	NamespaceRemoteDNSRecords = "kuadrant-dns-records"
)

var (
	scheme = runtime.NewScheme()
)

type K8SDNSProvider struct {
	client client.Client
}

func NewProviderFromSecret(ctx context.Context, s *v1.Secret, _ provider.Config) (provider.Provider, error) {
	config, _ := clientcmd.RESTConfigFromKubeConfig(s.Data["kubeconfig"])
	c, _ := client.New(config, client.Options{
		Scheme: scheme,
	})
	return &K8SDNSProvider{
		client: c,
	}, nil
}

var p provider.Provider = &K8SDNSProvider{}

func (p *K8SDNSProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	remoteRecord := &v1alpha1.DNSRecord{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "",
			Namespace: NamespaceRemoteDNSRecords,
		},
	}
	if err := p.client.Get(ctx, client.ObjectKeyFromObject(remoteRecord), remoteRecord); client.IgnoreNotFound(err) != nil {
		return nil, err
	}
	return remoteRecord.Spec.Endpoints, nil
}

func (p *K8SDNSProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	//TODO implement me
	panic("implement me")
}

func (p *K8SDNSProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return endpoints, nil
}

func (p *K8SDNSProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}

func (p *K8SDNSProvider) DNSZones(_ context.Context) ([]provider.DNSZone, error) {
	return nil, provider.ErrZoneLookupUnsupported
}

func (p *K8SDNSProvider) DNSZoneForHost(_ context.Context, _ string) (*provider.DNSZone, error) {
	return nil, provider.ErrZoneLookupUnsupported
}

func (p *K8SDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *K8SDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderK8s
}

// Register this Provider with the provider factory
func init() {
	utilruntime.Must(v1alpha1.AddToScheme(scheme))

	provider.RegisterProvider(p.Name().String(), NewProviderFromSecret, true)
}
