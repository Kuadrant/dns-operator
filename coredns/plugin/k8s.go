package kuadrant

import (
	"context"
	"strings"

	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/coredns-kuadrant/dnsop"
)

const (
	defaultResyncPeriod  = 0
	dnsRecordUniqueIndex = "dnsRecordIndex"
)

type LookupDNSRecordEndpoint func(indexKey string) (record *v1alpha1.DNSRecord, endpoint *externaldns.Endpoint)

type ResourceWithLookup struct {
	Name   string
	Lookup LookupDNSRecordEndpoint
}

var Resources = struct {
	DNSRecord *ResourceWithLookup
}{
	DNSRecord: &ResourceWithLookup{
		Name: "DNSRecord",
	},
}

// KubeController stores the current runtime configuration and cache
type KubeController struct {
	client              dnsop.Interface
	dnsRecordController cache.SharedIndexInformer
	hasSynced           bool
	labelFilter         string
}

func newKubeController(ctx context.Context, c *dnsop.DNSRecordClient) *KubeController {
	log.Infof("Building kube controller")

	ctrl := &KubeController{
		client: c,
	}

	if existDNSRecordCRDs(ctx, c) {
		dnsRecordController := cache.NewSharedIndexInformer(
			&cache.ListWatch{
				ListFunc:  dnsRecordLister(ctx, ctrl.client, core.NamespaceAll, ctrl.labelFilter),
				WatchFunc: dnsRecordWatcher(ctx, ctrl.client, core.NamespaceAll, ctrl.labelFilter),
			},
			&v1alpha1.DNSRecord{},
			defaultResyncPeriod,
			cache.Indexers{dnsRecordUniqueIndex: endpointHostnameIndexFunc},
		)
		Resources.DNSRecord.Lookup = ctrl.getEndpointByDNSName
		ctrl.dnsRecordController = dnsRecordController
	}
	return ctrl
}

func (ctrl *KubeController) run() {
	stopCh := make(chan struct{})
	defer close(stopCh)

	var synced []cache.InformerSynced

	log.Infof("Starting kube controller")
	ctrl.dnsRecordController.Run(stopCh)
	synced = append(synced, ctrl.dnsRecordController.HasSynced)

	log.Infof("Waiting for controllers to sync")
	if !cache.WaitForCacheSync(stopCh, synced...) {
		ctrl.hasSynced = false
	}
	log.Infof("Synced all required resources")
	ctrl.hasSynced = true

	<-stopCh
}

// HasSynced returns true if all controllers have been synced
func (ctrl *KubeController) HasSynced() bool {
	return ctrl.hasSynced
}

// RunKubeController kicks off the k8s controllers
func (k *Kuadrant) RunKubeController(ctx context.Context) error {
	config, err := k.getClientConfig()
	if err != nil {
		return err
	}

	err = dnsop.AddToScheme(scheme.Scheme)
	if err != nil {
		return err
	}

	dnsOpKubeClient, err := dnsop.NewForConfig(config)
	if err != nil {
		return err
	}

	k.Controller = newKubeController(ctx, dnsOpKubeClient)
	go k.Controller.run()

	return nil

}

func (k *Kuadrant) getClientConfig() (*rest.Config, error) {
	if k.ConfigFile != "" {
		overrides := &clientcmd.ConfigOverrides{}
		overrides.CurrentContext = k.ConfigContext

		config := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
			&clientcmd.ClientConfigLoadingRules{ExplicitPath: k.ConfigFile},
			overrides,
		)

		return config.ClientConfig()
	}

	return rest.InClusterConfig()
}

func dnsRecordLister(ctx context.Context, c dnsop.Interface, ns, label string) func(metav1.ListOptions) (runtime.Object, error) {
	return func(opts metav1.ListOptions) (runtime.Object, error) {
		opts.LabelSelector = label
		return c.DNSRecords(ns).List(ctx, opts)
	}
}

func dnsRecordWatcher(ctx context.Context, c dnsop.Interface, ns, label string) func(metav1.ListOptions) (watch.Interface, error) {
	return func(opts metav1.ListOptions) (watch.Interface, error) {
		opts.LabelSelector = label
		return c.DNSRecords(ns).Watch(ctx, opts)
	}
}

func existDNSRecordCRDs(ctx context.Context, c *dnsop.DNSRecordClient) bool {
	_, err := c.DNSRecords("").List(ctx, metav1.ListOptions{})
	return handleCRDCheckError(err, "DNSRecord", "kuadrant.io")
}

func handleCRDCheckError(err error, resourceName string, apiGroup string) bool {
	if meta.IsNoMatchError(err) || runtime.IsNotRegisteredError(err) || apierrors.IsNotFound(err) {
		log.Infof("%s CRDs are not found. Not syncing %s resources.", resourceName, resourceName)
		return false
	}
	if apierrors.IsForbidden(err) {
		log.Infof("access to `%s` is forbidden, please check RBAC. Not syncing %s resources.", apiGroup, resourceName)
		return false
	}
	if err != nil {
		panic(err)
	}
	return true
}

func endpointHostnameIndexFunc(obj interface{}) ([]string, error) {
	record, ok := obj.(*v1alpha1.DNSRecord)
	if !ok {
		return []string{}, nil
	}

	var hostnames []string
	for _, ep := range record.Spec.Endpoints {
		log.Infof("adding index %s for endpoints %s", ep.DNSName, record.Name)
		hostnames = append(hostnames, ep.DNSName)
	}
	return hostnames, nil
}

func (ctrl *KubeController) getEndpointByDNSName(host string) (record *v1alpha1.DNSRecord, endpoint *externaldns.Endpoint) {
	log.Debugf("Index key %+v", host)

	recordList := ctrl.dnsRecordController.GetIndexer().List()

	for _, obj := range recordList {
		rec := obj.(*v1alpha1.DNSRecord)
		for _, ep := range rec.Spec.Endpoints {
			if strings.EqualFold(ep.DNSName, host) {
				log.Debugf("found matching DNSRecord with Endpoint for host: %s:%s, %s", rec.Name, ep.DNSName, host)
				return rec, ep
			}
		}
	}
	log.Debugf("no matching endpoint found for host: %s", host)
	return nil, nil
}
