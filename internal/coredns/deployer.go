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

	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
)

//go:embed manifests/*.yaml
var manifests embed.FS

const (
	fieldManager        = "dns-operator"
	imageEnvVar         = "RELATED_IMAGE_COREDNS"
	defaultCoreDNSImage = "quay.io/kuadrant/coredns-kuadrant:latest"
)

//+kubebuilder:rbac:groups="",resources=namespaces,verbs=create;get;update;patch
//+kubebuilder:rbac:groups="",resources=configmaps;services,verbs=create;get;list;update;patch;watch
//+kubebuilder:rbac:groups=apps,resources=deployments,verbs=create;get;list;update;patch;watch
//+kubebuilder:rbac:groups=rbac.authorization.k8s.io,resources=clusterroles;clusterrolebindings,verbs=create;get;update;patch;bind;escalate

type Deployer struct {
	client dynamic.Interface
	mapper meta.RESTMapper
	logger logr.Logger
}

func NewDeployer(client dynamic.Interface, mapper meta.RESTMapper, logger logr.Logger) *Deployer {
	return &Deployer{
		client: client,
		mapper: mapper,
		logger: logger,
	}
}

// Start implements manager.Runnable.
func (d *Deployer) Start(ctx context.Context) error {
	d.logger.Info("deploying CoreDNS resources")

	objects, err := loadManifests()
	if err != nil {
		return fmt.Errorf("loading CoreDNS manifests: %w", err)
	}

	image := os.Getenv(imageEnvVar)
	if image == "" {
		image = defaultCoreDNSImage
	}

	if err := patchDeploymentImage(objects, image); err != nil {
		return fmt.Errorf("patching CoreDNS image: %w", err)
	}

	sortByInstallOrder(objects)

	for _, obj := range objects {
		if err := d.applyResource(ctx, obj); err != nil {
			return fmt.Errorf("applying %s %s: %w", obj.GetKind(), obj.GetName(), err)
		}
		d.logger.Info("applied resource", "kind", obj.GetKind(), "name", obj.GetName(), "namespace", obj.GetNamespace())
	}

	d.logger.Info("CoreDNS resources deployed successfully")
	return nil
}

func (d *Deployer) applyResource(ctx context.Context, obj *unstructured.Unstructured) error {
	gvk := obj.GroupVersionKind()
	mapping, err := d.mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
	if err != nil {
		return fmt.Errorf("getting REST mapping for %s: %w", gvk, err)
	}

	var dr dynamic.ResourceInterface
	if mapping.Scope.Name() == meta.RESTScopeNameNamespace {
		dr = d.client.Resource(mapping.Resource).Namespace(obj.GetNamespace())
	} else {
		dr = d.client.Resource(mapping.Resource)
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
