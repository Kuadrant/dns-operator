package provider

import (
	"context"
	"fmt"
	"sync"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/kuadrant-dns-operator/api/v1alpha1"
)

// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch

var errUnsupportedProvider = fmt.Errorf("provider type given is not supported")

// ProviderConstructor constructs a provider given a Secret resource and a Context.
// An error will be returned if the appropriate provider is not registered.
type ProviderConstructor func(context.Context, *v1.Secret) (Provider, error)

var (
	constructors     = make(map[string]ProviderConstructor)
	constructorsLock sync.RWMutex
)

// RegisterProvider will register a provider constructor, so it can be used within the application.
// 'name' should be unique, and should be used to identify this provider.
func RegisterProvider(name string, c ProviderConstructor) {
	constructorsLock.Lock()
	defer constructorsLock.Unlock()
	constructors[name] = c
}

// Factory is an interface that can be used to obtain Provider implementations.
// It determines which provider implementation to use by introspecting the given ProviderAccessor resource.
type Factory interface {
	ProviderFor(context.Context, v1alpha1.ProviderAccessor) (Provider, error)
}

// factory is the default Factory implementation
type factory struct {
	client.Client
}

// NewFactory returns a new provider factory with the given client.
func NewFactory(c client.Client) Factory {
	return &factory{Client: c}
}

// ProviderFor will return a Provider interface for the given ProviderAccessor secret.
// If the requested ProviderAccessor secret does not exist, an error will be returned.
func (f *factory) ProviderFor(ctx context.Context, pa v1alpha1.ProviderAccessor) (Provider, error) {
	providerSecret := &v1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pa.GetProviderRef().Name,
			Namespace: pa.GetNamespace(),
		}}

	if err := f.Client.Get(ctx, client.ObjectKeyFromObject(providerSecret), providerSecret); err != nil {
		return nil, err
	}

	providerType, err := nameForProviderSecret(providerSecret)
	if err != nil {
		return nil, err
	}

	constructorsLock.RLock()
	defer constructorsLock.RUnlock()
	if constructor, ok := constructors[providerType]; ok {
		return constructor(ctx, providerSecret)
	}

	return nil, fmt.Errorf("provider '%s' not registered", providerType)
}

func nameForProviderSecret(secret *v1.Secret) (string, error) {
	switch secret.Type {
	case "kuadrant.io/aws":
		return "aws", nil
	case "kuadrant.io/gcp":
		return "google", nil
	}
	return "", errUnsupportedProvider
}
