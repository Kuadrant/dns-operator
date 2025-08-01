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

var errUnsupportedProvider = fmt.Errorf("provider type given is not supported")

// ProviderConstructor constructs a provider given a Secret resource and a Context.
// An error will be returned if the appropriate provider is not registered.
type ProviderConstructor func(context.Context, *v1.Secret, Config) (Provider, error)
type ProviderConstructorWithClient func(context.Context, dynamic.Interface, *v1.Secret, Config) (Provider, error)

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
}

// factory is the default Factory implementation
type factory struct {
	client.Client
	dynamicClient dynamic.Interface
	providers     []string
}

// NewFactory returns a new provider factory with the given client and given providers enabled.
// Will return an error if any given provider has no registered provider implementation.
func NewFactory(c client.Client, d dynamic.Interface, p []string) (Factory, error) {
	var err error
	registeredProviders := maps.Keys(constructors)
	for _, provider := range p {
		if !slices.Contains(registeredProviders, provider) {
			err = errors.Join(err, fmt.Errorf("provider '%s' not registered", provider))
		}
	}
	return &factory{Client: c, dynamicClient: d, providers: p}, err
}

// ProviderFor will return a Provider interface for the given ProviderAccessor secret.
// If the ProviderAccessor is delegating an in memory endpoint provider secret is returned with configuration appropriate for delegation of that resource.
// If the requested ProviderAccessor is not delegating, and the secret does not exist in the resource namespace, an error will be returned.
func (f *factory) ProviderFor(ctx context.Context, pa v1alpha1.ProviderAccessor, c Config) (Provider, error) {
	logger := log.FromContext(ctx)

	var providerSecret *v1.Secret
	if pa.IsDelegating() {
		providerSecret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pa.GetProviderRef().Name,
				Namespace: pa.GetNamespace(),
			},
			Type: v1alpha1.SecretTypeKuadrantEndpoint,
			Data: map[string][]byte{
				v1alpha1.EndpointLabelSelectorKey: []byte(fmt.Sprintf("%s=%s", v1alpha1.DelegationAuthoritativeRecordLabel, pa.GetRootHost())),
			},
		}
	} else {
		providerSecret = &v1.Secret{
			ObjectMeta: metav1.ObjectMeta{
				Name:      pa.GetProviderRef().Name,
				Namespace: pa.GetNamespace(),
			}}

		if err := f.Client.Get(ctx, client.ObjectKeyFromObject(providerSecret), providerSecret); err != nil {
			return nil, err
		}
	}

	provider, err := NameForProviderSecret(providerSecret)
	if err != nil {
		return nil, err
	}

	constructorsLock.RLock()
	defer constructorsLock.RUnlock()
	if constructor, ok := constructors[provider]; ok {
		if !slices.Contains(f.providers, provider) {
			return nil, fmt.Errorf("provider '%s' not enabled", provider)
		}
		logger.V(1).Info(fmt.Sprintf("initializing %s provider with config", provider), "config", c)
		switch typedConstructor := constructor.(type) {
		case ProviderConstructor:
			return typedConstructor(ctx, providerSecret, c)
		case ProviderConstructorWithClient:
			return typedConstructor(ctx, f.dynamicClient, providerSecret, c)
		}

		return nil, fmt.Errorf("unrecognised contructor for provider '%s'", provider)
	}

	return nil, fmt.Errorf("provider '%s' not registered", provider)
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
