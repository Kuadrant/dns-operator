package kuadrant

import (
	"context"
	"os"
	"strings"

	core "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/watch"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/kuadrant/dns-operator/api/v1alpha1"

	"github.com/kuadrant/coredns-kuadrant/dnsop"
)

const (
	defaultResyncPeriod   = 0
	watchNamespacesEnvVar = "WATCH_NAMESPACES"
	zoneNameLabel         = "kuadrant.io/coredns-zone-name"
)

type zoneInformers struct {
	informers  []cache.SharedInformer
	zone       *Zone
	zoneOrigin string
}

func (zi *zoneInformers) refreshZone() {
	log.Infof("updating zone %s", zi.zoneOrigin)
	newZ := NewZone(zi.zoneOrigin)

	for _, informer := range zi.informers {
		for _, obj := range informer.GetStore().List() {
			rec := obj.(*v1alpha1.DNSRecord)
			for _, ep := range rec.Spec.Endpoints {
				log.Debugf("adding %s record endpoint %s to zone %s from %s/%s", ep.RecordType, ep.DNSName, zi.zoneOrigin, rec.Namespace, rec.Name)
				err := newZ.InsertEndpoint(ep)
				if err != nil {
					log.Error(err)
				}
			}
		}
	}

	zi.zone.RefreshFrom(newZ)
}

// KubeController stores the current runtime configuration and cache
type KubeController struct {
	client      dnsop.Interface
	controllers []zoneInformers
	hasSynced   bool
	labelFilter string
}

func newKubeController(ctx context.Context, c *dnsop.DNSRecordClient, zones map[string]*Zone) *KubeController {
	ctrl := &KubeController{
		client: c,
	}

	if existDNSRecordCRDs(ctx, c) {
		for origin, zone := range zones {
			labelSelector := labels.SelectorFromSet(map[string]string{
				zoneNameLabel: stripClosingDot(origin),
			})

			var namespaces []string
			if w := os.Getenv(watchNamespacesEnvVar); w != "" {
				namespaces = strings.Split(w, ",")
			} else {
				namespaces = []string{core.NamespaceAll}
			}

			log.Infof("creating zone informer for %s with label selector %s and namespaces %s", origin, labelSelector.String(), namespaces)

			zi := zoneInformers{
				informers:  make([]cache.SharedInformer, 0, len(namespaces)),
				zone:       zone,
				zoneOrigin: origin,
			}

			for _, ns := range namespaces {
				informer := cache.NewSharedInformer(
					&cache.ListWatch{
						ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
							opts.LabelSelector = labelSelector.String()
							return c.DNSRecords(ns).List(ctx, opts)
						},
						WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
							opts.LabelSelector = labelSelector.String()
							return c.DNSRecords(ns).Watch(ctx, opts)
						},
					},
					&v1alpha1.DNSRecord{},
					defaultResyncPeriod,
				)
				_, _ = informer.AddEventHandler(cache.ResourceEventHandlerFuncs{
					AddFunc: func(obj interface{}) {
						zi.refreshZone()
					},
					UpdateFunc: func(old, new interface{}) {
						zi.refreshZone()
					},
					DeleteFunc: func(obj interface{}) {
						zi.refreshZone()
					},
				})
				zi.informers = append(zi.informers, informer)
			}

			ctrl.controllers = append(ctrl.controllers, zi)
		}
	}
	return ctrl
}

func (ctrl *KubeController) run() {
	stopCh := make(chan struct{})
	defer close(stopCh)

	var synced []cache.InformerSynced

	log.Infof("Starting kube controllers")
	for _, ctrlZone := range ctrl.controllers {
		for i, informer := range ctrlZone.informers {
			log.Infof("Starting informer %v for zone %s", i, ctrlZone.zoneOrigin)
			go informer.Run(stopCh)
			synced = append(synced, informer.HasSynced)
		}
	}
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

	k.Controller = newKubeController(ctx, dnsOpKubeClient, k.Z)
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
