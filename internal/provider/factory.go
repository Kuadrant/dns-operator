package provider

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"

	"golang.org/x/exp/maps"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/dynamic"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

var (
	errUnsupportedProvider             = fmt.Errorf("provider type given is not supported")
	errProviderRefRequired             = fmt.Errorf("provider ref is required unless delegation is specificed")
	errDelegationProviderNotConfigured = fmt.Errorf("delegation provider not configured")
)

// ProviderConstructor constructs a provider given a Secret resource and a Context.
// An error will be returned if the appropriate provider is not registered.
type ProviderConstructor func(context.Context, *v1.Secret, Config) (Provider, error)
type ProviderConstructorWithClient func(context.Context, dynamic.Interface, *v1.Secret, Config) (Provider, error)
type DelegationProviderFunc func(context.Context, dynamic.Interface, v1alpha1.ProviderAccessor, Config) (Provider, error)

var (
	constructors     = make(map[string]interface{})
	constructorsLock sync.RWMutex
	defaultProviders []string
)

func RegisterProviderWithClient(name string, c ProviderConstructorWithClient, asDefault bool) {
	registerProvider(name, c, asDefault)
}

func RegisterProvider(name string, c ProviderConstructor, asDefault bool) {
	registerProvider(name, c, asDefault)
}

// RegisterProvider will register a provider constructor, so it can be used within the application.
// 'name' should be unique, and should be used to identify this provider
// `asDefault` indicates if the provider should be added as a default provider and included in the default providers list.
func registerProvider(name string, c interface{}, asDefault bool) {
	log.Log.Info("registering provider", "name", name)
	constructorsLock.Lock()
	defer constructorsLock.Unlock()
	constructors[name] = c
	if asDefault {
		defaultProviders = append(defaultProviders, name)
	}
}

func RegisteredDefaultProviders() []string {
	return defaultProviders
}

// Factory is an interface that can be used to obtain Provider implementations.
// It determines which provider implementation to use by introspecting the given ProviderAccessor resource.
type Factory interface {
	ProviderFor(context.Context, v1alpha1.ProviderAccessor, Config) (Provider, error)
	ProviderForSecret(ctx context.Context, pSecret *v1.Secret, pConfig Config) (Provider, error)
}

// factory is the default Factory implementation
type factory struct {
	client.Client
	dynamicClient          dynamic.Interface
	providers              []string
	delegationProviderFunc DelegationProviderFunc
}

// NewFactory returns a new provider factory with the given client and given providers enabled.
// Will return an error if any given provider has no registered provider implementation.
func NewFactory(c client.Client, d dynamic.Interface, p []string, dpf DelegationProviderFunc) (Factory, error) {
	var err error
	registeredProviders := maps.Keys(constructors)
	for _, provider := range p {
		if !slices.Contains(registeredProviders, provider) {
			err = errors.Join(err, fmt.Errorf("provider '%s' not registered", provider))
		}
	}
	return &factory{Client: c, dynamicClient: d, providers: p, delegationProviderFunc: dpf}, err
}

// ProviderFor will return a Provider instance for the given ProviderAccessor(e.g. DNSRecord).
// If the resource is delegating an in memory endpoint provider secret is returned with configuration appropriate for delegation of that resource.
// If the resource is not delegating, and the secret does not exist in the resource namespace, an error will be returned.
func (f *factory) ProviderFor(ctx context.Context, pa v1alpha1.ProviderAccessor, c Config) (Provider, error) {
	var provider Provider
	var err error
	if !pa.IsDelegating() {
		if pa.GetProviderRef().Name == "" {
			return nil, errProviderRefRequired
		}
		pSecret := &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pa.GetProviderRef().Name,
				Namespace: pa.GetNamespace(),
			}}
		if err = f.Client.Get(ctx, client.ObjectKeyFromObject(pSecret), pSecret); err != nil {
			return nil, err
		}

		provider, err = f.ProviderForSecret(ctx, pSecret, c)
		if err != nil {
			return nil, err
		}
	}

	if pa.IsDelegating() || requiresDelegation(provider, pa) {
		return f.delegationProviderFor(ctx, pa, c)
	}

	return provider, nil
}

func (f *factory) delegationProviderFor(ctx context.Context, pa v1alpha1.ProviderAccessor, pConfig Config) (Provider, error) {
	if f.delegationProviderFunc == nil {
		return nil, errDelegationProviderNotConfigured
	}
	return f.delegationProviderFunc(ctx, f.dynamicClient, pa, pConfig)
}

func (f *factory) ProviderForSecret(ctx context.Context, pSecret *v1.Secret, pConfig Config) (Provider, error) {
	logger := log.FromContext(ctx)

	providerName, err := NameForProviderSecret(pSecret)
	if err != nil {
		return nil, err
	}

	constructorsLock.RLock()
	defer constructorsLock.RUnlock()

	if constructor, ok := constructors[providerName]; ok {
		if !slices.Contains(f.providers, providerName) {
			return nil, fmt.Errorf("provider '%s' not enabled", providerName)
		}
		logger.V(1).Info(fmt.Sprintf("initializing %s provider with config", providerName), "config", pConfig)
		switch typedConstructor := constructor.(type) {
		case ProviderConstructor:
			return typedConstructor(ctx, pSecret, pConfig)
		case ProviderConstructorWithClient:
			return typedConstructor(ctx, f.dynamicClient, pSecret, pConfig)
		}
		return nil, fmt.Errorf("unrecognised contructor for provider '%s'", providerName)
	}
	return nil, fmt.Errorf("providerName '%s' not registered", providerName)
}

// requiresDelegation return true if the given provider requires delegation be enforced for the given resource(DNSRecord)
func requiresDelegation(p Provider, pa v1alpha1.ProviderAccessor) bool {
	return p.Name() == DNSProviderCoreDNS && !pa.IsAuthoritativeRecord()
}

func NameForProviderSecret(secret *v1.Secret) (string, error) {
	switch secret.Type {
	case v1alpha1.SecretTypeKuadrantAWS:
		return DNSProviderAWS.String(), nil
	case v1alpha1.SecretTypeKuadrantAzure:
		return DNSProviderAzure.String(), nil
	case v1alpha1.SecretTypeKuadrantGCP:
		return DNSProviderGCP.String(), nil
	case v1alpha1.SecretTypeKuadrantInmemory:
		return DNSProviderInMem.String(), nil
	case v1alpha1.SecretTypeKuadrantCoreDNS:
		return DNSProviderCoreDNS.String(), nil
	case v1alpha1.SecretTypeKuadrantEndpoint:
		return DNSProviderEndpoint.String(), nil
	}
	return "", errUnsupportedProvider
}
