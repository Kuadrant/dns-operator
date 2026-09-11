package coredns

import (
	"context"
	"embed"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-logr/logr"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/event"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/controller-runtime/pkg/source"
)

//go:embed manifests/*.yaml
var manifests embed.FS

const (
	fieldManager        = "dns-operator"
	imageEnvVar         = "RELATED_IMAGE_COREDNS"
	defaultCoreDNSImage = "quay.io/kuadrant/coredns-kuadrant:latest"
	resourceName        = "kuadrant-coredns"
	managedByLabel      = "app.kubernetes.io/managed-by"
	managedByValue      = "dns-operator"
)

//+kubebuilder:rbac:groups="",resources=namespaces,verbs=create;get;update;patch;watch;delete
//+kubebuilder:rbac:groups="",resources=configmaps;services,verbs=create;get;list;update;patch;watch;delete
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;get;list;update;patch;watch;delete
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=create;get;update;patch;bind;escalate;watch;delete

type Reconciler struct {
	dynClient dynamic.Interface
	mapper    meta.RESTMapper
	logger    logr.Logger
	deploy    bool
}

func NewReconciler(client dynamic.Interface, mapper meta.RESTMapper, logger logr.Logger, deploy bool) *Reconciler {
	return &Reconciler{
		dynClient: client,
		mapper:    mapper,
		logger:    logger,
		deploy:    deploy,
	}
}

func (r *Reconciler) Reconcile(ctx context.Context, _ ctrl.Request) (ctrl.Result, error) {
	if r.deploy {
		return r.reconcileDeploy(ctx)
	}
	return r.reconcileCleanup(ctx)
}

func (r *Reconciler) reconcileDeploy(ctx context.Context) (ctrl.Result, error) {
	r.logger.Info("reconciling CoreDNS resources")

	objects, err := loadManifests()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("loading CoreDNS manifests: %w", err)
	}

	image := os.Getenv(imageEnvVar)
	if image == "" {
		image = defaultCoreDNSImage
	}

	if err := patchDeploymentImage(objects, image); err != nil {
		return ctrl.Result{}, fmt.Errorf("patching CoreDNS image: %w", err)
	}

	sortByInstallOrder(objects)

	for _, obj := range objects {
		setManagedByLabel(obj)
		if err := r.applyResource(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("applying %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
		r.logger.V(1).Info("applied resource", "kind", obj.GetKind(), "name", obj.GetName(), "namespace", obj.GetNamespace())
	}

	r.logger.Info("CoreDNS resources reconciled successfully")
	return ctrl.Result{}, nil
}

func (r *Reconciler) reconcileCleanup(ctx context.Context) (ctrl.Result, error) {
	nsGVR := schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}
	ns, err := r.dynClient.Resource(nsGVR).Get(ctx, resourceName, metav1.GetOptions{})
	if apierrors.IsNotFound(err) {
		return ctrl.Result{}, nil
	}
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("checking for CoreDNS namespace: %w", err)
	}
	if ns.GetLabels()[managedByLabel] != managedByValue {
		return ctrl.Result{}, nil
	}

	r.logger.Info("cleaning up previously deployed CoreDNS resources")

	objects, err := loadManifests()
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("loading CoreDNS manifests for cleanup: %w", err)
	}

	sortByInstallOrder(objects)
	for i := len(objects) - 1; i >= 0; i-- {
		obj := objects[i]
		if err := r.deleteResource(ctx, obj); err != nil {
			return ctrl.Result{}, fmt.Errorf("deleting %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
		r.logger.Info("deleted resource", "kind", obj.GetKind(), "name", obj.GetName())
	}

	r.logger.Info("CoreDNS resources cleaned up successfully")
	return ctrl.Result{}, nil
}

func (r *Reconciler) SetupWithManager(mgr ctrl.Manager) error {
	triggerCh := make(chan event.GenericEvent, 1)

	mapToFixed := handler.EnqueueRequestsFromMapFunc(
		func(_ context.Context, _ client.Object) []reconcile.Request {
			return []reconcile.Request{{
				NamespacedName: types.NamespacedName{Name: resourceName},
			}}
		},
	)

	labelPred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		labels := obj.GetLabels()
		return labels["app.kubernetes.io/instance"] == "kuadrant" &&
			labels["app.kubernetes.io/name"] == "coredns"
	})

	namePred := predicate.NewPredicateFuncs(func(obj client.Object) bool {
		return obj.GetName() == resourceName
	})

	err := ctrl.NewControllerManagedBy(mgr).
		Named("coredns").
		WatchesRawSource(source.Channel(triggerCh, mapToFixed)).
		Watches(&corev1.Namespace{}, mapToFixed, builder.WithPredicates(namePred)).
		Watches(&appsv1.Deployment{}, mapToFixed, builder.WithPredicates(labelPred)).
		Watches(&corev1.ConfigMap{}, mapToFixed, builder.WithPredicates(labelPred)).
		Watches(&corev1.Service{}, mapToFixed, builder.WithPredicates(labelPred)).
		Watches(&rbacv1.ClusterRole{}, mapToFixed, builder.WithPredicates(namePred)).
		Watches(&rbacv1.ClusterRoleBinding{}, mapToFixed, builder.WithPredicates(namePred)).
		Complete(r)
	if err != nil {
		return err
	}

	go func() {
		<-mgr.Elected()
		triggerCh <- event.GenericEvent{Object: &corev1.Namespace{
			ObjectMeta: metav1.ObjectMeta{Name: resourceName},
		}}
	}()

	return nil
}

func (r *Reconciler) applyResource(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := r.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("getting REST mapping for %s: %w", gvk, err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		dr = r.dynClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = r.dynClient.Resource(mapping.Resource)
	}

	data, err := obj.MarshalJSON()
	if err != nil {
		return fmt.Errorf("marshalling %s: %w", obj.GetName(), err)
	}

	force := true
	_, err = dr.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
		FieldManager: fieldManager,
		Force:        &force,
	})
	return err
}

func (r *Reconciler) deleteResource(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := r.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("getting REST mapping for %s: %w", gvk, err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		dr = r.dynClient.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = r.dynClient.Resource(mapping.Resource)
	}

	err = dr.Delete(ctx, obj.GetName(), metav1.DeleteOptions{})
	if apierrors.IsNotFound(err) {
		return nil
	}
	return err
}

func setManagedByLabel(obj *unstructured.Unstructured) {
	labels := obj.GetLabels()
	if labels == nil {
		labels = make(map[string]string)
	}
	labels[managedByLabel] = managedByValue
	obj.SetLabels(labels)
}

func loadManifests() ([]*unstructured.Unstructured, error) {
	var objects []*unstructured.Unstructured
	err := fs.WalkDir(manifests, "manifests", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || filepath.Ext(path) != ".yaml" {
			return nil
		}
		data, err := manifests.ReadFile(path)
		if err != nil {
			return fmt.Errorf("reading %s: %w", path, err)
		}
		decoder := yaml.NewYAMLOrJSONDecoder(strings.NewReader(string(data)), 4096)
		for {
			obj := &unstructured.Unstructured{}
			if err := decoder.Decode(obj); err != nil {
				break
			}
			if obj.GetKind() == "" {
				continue
			}
			objects = append(objects, obj)
		}
		return nil
	})
	return objects, err
}

func patchDeploymentImage(objects []*unstructured.Unstructured, image string) error {
	if image == "" {
		return nil
	}
	for _, obj := range objects {
		if obj.GetKind() != "Deployment" {
			continue
		}
		containers, found, err := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", "containers")
		if err != nil || !found || len(containers) == 0 {
			continue
		}
		container, ok := containers[0].(map[string]interface{})
		if !ok {
			continue
		}
		container["image"] = image
		containers[0] = container
		if err := unstructured.SetNestedSlice(obj.Object, containers, "spec", "template", "spec", "containers"); err != nil {
			return fmt.Errorf("patching image on Deployment %s: %w", obj.GetName(), err)
		}
	}
	return nil
}

var installOrder = map[string]int{
	"Namespace":          0,
	"ClusterRole":        1,
	"ClusterRoleBinding": 2,
	"ConfigMap":          3,
	"Service":            4,
	"Deployment":         5,
}

func sortByInstallOrder(objects []*unstructured.Unstructured) {
	sort.SliceStable(objects, func(i, j int) bool {
		oi := installOrder[objects[i].GetKind()]
		oj := installOrder[objects[j].GetKind()]
		return oi < oj
	})
}
