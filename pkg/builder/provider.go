/*
Copyright 2024.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package builder

import (
	"strings"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

// ProviderBuilder builds a Provider.
type ProviderBuilder struct {
	name         string
	namespace    string
	secretType   corev1.SecretType
	strDataItems map[string]string
}

// NewProviderBuilder returns a new provider builder with the name and namespace provided
func NewProviderBuilder(name, namespace string) *ProviderBuilder {
	return &ProviderBuilder{name: name, namespace: namespace}
}

// For defines the type of provider secret being created
func (pb *ProviderBuilder) For(secretType corev1.SecretType) *ProviderBuilder {
	pb.secretType = secretType
	return pb
}

// WithDataItem adds key/values that will be included in the provider secret data.
// Defaults to an empty map.
func (pb *ProviderBuilder) WithDataItem(key, value string) *ProviderBuilder {
	if pb.strDataItems == nil {
		pb.strDataItems = map[string]string{}
	}
	pb.strDataItems[key] = value
	return pb
}

// WithZonesInitialisedFor sets the domains of zones to initialize in the provider.
// Only used by v1alpha1.SecretTypeKuadrantInmemory provider, ignored by all others.
// Defaults to the empty list.
func (pb *ProviderBuilder) WithZonesInitialisedFor(domains ...string) *ProviderBuilder {
	if val, ok := pb.strDataItems[v1alpha1.InmemInitZonesKey]; ok {
		domains = append(strings.Split(val, ","), domains...)
	}
	return pb.WithDataItem(v1alpha1.InmemInitZonesKey, strings.Join(domains, ","))
}

// Build builds and returns the provider secret.
func (pb *ProviderBuilder) Build() *corev1.Secret {
	return &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      pb.name,
			Namespace: pb.namespace,
		},
		StringData: pb.strDataItems,
		Type:       pb.secretType,
	}
}
