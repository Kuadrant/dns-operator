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
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

var errUnsupportedProvider = fmt.Errorf("provider type given is not supported")

// ProviderConstructor constructs a provider given a Context.
// An error will be returned if the appropriate provider is not registered.
type ProviderConstructor func(context.Context, Config) (Provider, error)

// ProviderConstructorFromSecret constructs a provider given a Context.
// An error will be returned if the appropriate provider is not registered.
type ProviderConstructorFromSecret func(context.Context, *v1.Secret, Config) (Provider, error)

var (
	constructors     = make(map[string]ProviderConstructor)
	constructorsLock sync.RWMutex

	constructorsFromSecret     = make(map[string]ProviderConstructorFromSecret)
	constructorsFromSecretLock sync.RWMutex

	defaultProviders []string
)

// RegisterProvider will register a provider constructor, so it can be used within the application.
// 'name' should be unique, and should be used to identify this provider
// `asDefault` indicates if the provider should be added as a default provider and included in the default providers list.
func RegisterProvider(name string, c ProviderConstructor, asDefault bool) {
	constructorsLock.Lock()
	defer constructorsLock.Unlock()
	constructors[name] = c
	if asDefault {
		defaultProviders = append(defaultProviders, name)
	}
}

// RegisterProviderFromSecret will register a provider constructor, so it can be used within the application.
// 'name' should be unique, and should be used to identify this provider
// `asDefault` indicates if the provider should be added as a default provider and included in the default providers list.
func RegisterProviderFromSecret(name string, c ProviderConstructorFromSecret, asDefault bool) {
	constructorsFromSecretLock.Lock()
	defer constructorsFromSecretLock.Unlock()
	constructorsFromSecret[name] = c
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
}

// factory is the default Factory implementation
type factory struct {
	client.Client
	providers []string
}

// NewFactory returns a new provider factory with the given client and given providers enabled.
// Will return an error if any given provider has no registered provider implementation.
func NewFactory(c client.Client, p []string) (Factory, error) {
	var err error
	registeredProviders := append(maps.Keys(constructors), maps.Keys(constructorsFromSecret)...)
	for _, provider := range p {
		if !slices.Contains(registeredProviders, provider) {
			err = errors.Join(err, fmt.Errorf("provider '%s' not registered", provider))
		}
	}
	return &factory{Client: c, providers: p}, err
}

// ProviderFor will return a Provider interface for the given ProviderAccessor secret.
// If the requested ProviderAccessor secret does not exist, an error will be returned.
func (f *factory) ProviderFor(ctx context.Context, pa v1alpha1.ProviderAccessor, c Config) (Provider, error) {
	logger := log.FromContext(ctx)

	// If the provider accessor has a provider set, we will use that provider & pass nil as the secret
	provider := pa.GetProvider().Name
	if provider != "" {
		if !slices.Contains(f.providers, provider) {
			return nil, fmt.Errorf("provider '%s' not enabled", provider)
		}
		logger.V(1).Info(fmt.Sprintf("initializing %s provider with config", provider), "config", c)
		constructorsLock.RLock()
		defer constructorsLock.RUnlock()
		if constructor, ok := constructors[provider]; ok {
			return constructor(ctx, c)
		}
		return nil, fmt.Errorf("provider '%s' not registered", provider)
	}

	// Get the provider secret from the accessor
	providerSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pa.GetProviderRef().Name,
			Namespace: pa.GetNamespace(),
		}}

	if err := f.Client.Get(ctx, client.ObjectKeyFromObject(providerSecret), providerSecret); err != nil {
		return nil, err
	}

	provider, err := NameForProviderSecret(providerSecret)
	if err != nil {
		return nil, err
	}

	constructorsFromSecretLock.RLock()
	defer constructorsFromSecretLock.RUnlock()
	if constructor, ok := constructorsFromSecret[provider]; ok {
		if !slices.Contains(f.providers, provider) {
			return nil, fmt.Errorf("provider '%s' not enabled", provider)
		}
		logger.V(1).Info(fmt.Sprintf("initializing %s provider with config", provider), "config", c)
		return constructor(ctx, providerSecret, c)
	}

	return nil, fmt.Errorf("provider '%s' not registered", provider)
}

func NameForProviderSecret(secret *v1.Secret) (string, error) {
	switch secret.Type {
	case v1alpha1.SecretTypeKuadrantAWS:
		return DNSProviderFromSecretAWS.String(), nil
	case v1alpha1.SecretTypeKuadrantAzure:
		return DNSProviderFromSecretAzure.String(), nil
	case v1alpha1.SecretTypeKuadrantGCP:
		return DNSProviderFromSecretGCP.String(), nil
	case v1alpha1.SecretTypeKuadrantInmemory:
		return DNSProviderFromSecretInMem.String(), nil
	case v1alpha1.SecretTypeKuadrantCoreDNS:
		return DNSProviderFromSecretCoreDNS.String(), nil
	}
	return "", errUnsupportedProvider
}
