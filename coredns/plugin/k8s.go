package kuadrant

import (
	"context"
	"net"

	"github.com/coredns/coredns/plugin/file"
	"github.com/coredns/coredns/plugin/pkg/dnsutil"
	"github.com/miekg/dns"

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
	"sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/coredns-kuadrant/dnsop"
	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

const (
	ZoneNameLabel       = "kuadrant.io/zone-name"
	defaultResyncPeriod = 0
)

type zoneInformer struct {
	cache.SharedInformer
	zone       *file.Zone
	zoneOrigin string
}

func (zi *zoneInformer) refreshZone() {
	log.Infof("updating zone %s", zi.zoneOrigin)
	newZ := file.NewZone(zi.zoneOrigin, "")

	ns := &dns.NS{Hdr: dns.RR_Header{Name: dns.Fqdn(zi.zoneOrigin), Rrtype: dns.TypeNS, Ttl: ttlSOA, Class: dns.ClassINET},
		Ns: dnsutil.Join("ns1", zi.zoneOrigin),
	}
	newZ.Insert(ns)

	soa := &dns.SOA{Hdr: dns.RR_Header{Name: dns.Fqdn(zi.zoneOrigin), Rrtype: dns.TypeSOA, Ttl: ttlSOA, Class: dns.ClassINET},
		Mbox:    dnsutil.Join("hostmaster", zi.zoneOrigin),
		Ns:      dnsutil.Join("ns1", zi.zoneOrigin),
		Serial:  12345,
		Refresh: 7200,
		Retry:   1800,
		Expire:  86400,
		Minttl:  ttlSOA,
	}
	newZ.Insert(soa)

	for _, obj := range zi.GetStore().List() {
		rec := obj.(*v1alpha1.DNSRecord)
		for _, ep := range rec.Spec.Endpoints {
			log.Debugf("adding %s record for %s to zone %s", ep.RecordType, ep.DNSName, zi.zoneOrigin)

			if ep.RecordType == endpoint.RecordTypeA {
				for _, t := range ep.Targets {
					a := &dns.A{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
						A: net.ParseIP(t)}
					newZ.Insert(a)
				}
			}

			if ep.RecordType == endpoint.RecordTypeAAAA {
				for _, t := range ep.Targets {
					aaaa := &dns.AAAA{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeAAAA, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
						AAAA: net.ParseIP(t)}
					newZ.Insert(aaaa)
				}
			}

			if ep.RecordType == endpoint.RecordTypeTXT {
				txt := &dns.TXT{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeTXT, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
					Txt: ep.Targets}
				newZ.Insert(txt)
			}

			if ep.RecordType == endpoint.RecordTypeCNAME {
				cname := &dns.CNAME{Hdr: dns.RR_Header{Name: dns.Fqdn(ep.DNSName), Rrtype: dns.TypeCNAME, Class: dns.ClassINET, Ttl: uint32(ep.RecordTTL)},
					Target: dns.Fqdn(ep.Targets[0])}
				newZ.Insert(cname)
			}
		}
	}

	// copy elements we need
	zi.zone.Lock()
	zi.zone.Apex = newZ.Apex
	zi.zone.Tree = newZ.Tree
	zi.zone.Unlock()
}

// KubeController stores the current runtime configuration and cache
type KubeController struct {
	client      dnsop.Interface
	controllers []zoneInformer
	hasSynced   bool
	labelFilter string
}

func newKubeController(ctx context.Context, c *dnsop.DNSRecordClient, zones map[string]*file.Zone) *KubeController {
	ctrl := &KubeController{
		client: c,
	}

	if existDNSRecordCRDs(ctx, c) {
		for origin, zone := range zones {

			labelSelector := labels.SelectorFromSet(map[string]string{
				ZoneNameLabel: stripClosingDot(origin),
			})

			log.Infof("creating zone informer for %s with label selector %s", origin, labelSelector.String())

			zi := zoneInformer{
				SharedInformer: cache.NewSharedInformer(
					&cache.ListWatch{
						ListFunc: func(opts metav1.ListOptions) (runtime.Object, error) {
							opts.LabelSelector = labelSelector.String()
							return c.DNSRecords(core.NamespaceAll).List(ctx, opts)
						},
						WatchFunc: func(opts metav1.ListOptions) (watch.Interface, error) {
							opts.LabelSelector = labelSelector.String()
							return c.DNSRecords(core.NamespaceAll).Watch(ctx, opts)
						},
					},
					&v1alpha1.DNSRecord{},
					defaultResyncPeriod,
				),
				zone:       zone,
				zoneOrigin: origin,
			}
			_, _ = zi.AddEventHandler(cache.ResourceEventHandlerFuncs{
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
		log.Infof("Starting controller for zone %s", ctrlZone.zoneOrigin)
		go ctrlZone.Run(stopCh)
		synced = append(synced, ctrlZone.HasSynced)
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
