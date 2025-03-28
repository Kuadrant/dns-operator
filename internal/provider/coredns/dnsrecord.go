//TODO this is copied over from the core dns plugin. Likely want to share the code

package coredns

import (
	"context"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

const GroupName = "kuadrant.io"
const GroupVersion = "v1alpha1"

var SchemeGroupVersion = schema.GroupVersion{Group: GroupName, Version: GroupVersion}

var (
	SchemeBuilder = runtime.NewSchemeBuilder(addKnownTypes)
	AddToScheme   = SchemeBuilder.AddToScheme
)

type DNSRecordClient struct {
	restClient rest.Interface
}

type DNSRecordInterface interface {
	DNSRecords(namespace string) DNSRecord
}

type dnsRecordClient struct {
	restClient rest.Interface
	ns         string
}

func (c *DNSRecordClient) DNSRecords(namespace string) DNSRecord {
	return &dnsRecordClient{
		restClient: c.restClient,
		ns:         namespace,
	}
}

type DNSRecord interface {
	List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error)
}

func NewForConfig(c *rest.Config) (*DNSRecordClient, error) {
	config := *c
	config.ContentConfig.GroupVersion = &schema.GroupVersion{Group: GroupName, Version: GroupVersion}
	config.APIPath = "/apis"
	config.NegotiatedSerializer = scheme.Codecs.WithoutConversion()
	config.UserAgent = rest.DefaultKubernetesUserAgent()

	client, err := rest.RESTClientFor(&config)
	if err != nil {
		return nil, err
	}

	return &DNSRecordClient{restClient: client}, nil
}

func (c *dnsRecordClient) List(ctx context.Context, opts metav1.ListOptions) (*v1alpha1.DNSRecordList, error) {
	result := v1alpha1.DNSRecordList{}
	err := c.restClient.
		Get().
		Namespace(c.ns).
		Resource("dnsrecords").
		VersionedParams(&opts, scheme.ParameterCodec).
		Do(ctx).
		Into(&result)

	return &result, err
}

func addKnownTypes(scheme *runtime.Scheme) error {
	scheme.AddKnownTypes(SchemeGroupVersion,
		&v1alpha1.DNSRecord{},
		&v1alpha1.DNSRecordList{},
	)

	metav1.AddToGroupVersion(scheme, SchemeGroupVersion)
	return nil
}
