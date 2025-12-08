package common

import (
	"context"
	"fmt"

	"github.com/pkg/errors"

	v1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	runtimeUtil "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func GetScheme() *runtime.Scheme {
	cmdScheme := scheme.Scheme
	runtimeUtil.Must(v1alpha1.AddToScheme(cmdScheme))
	return cmdScheme
}

func GetK8SConfig() *rest.Config {
	return controllerruntime.GetConfigOrDie()
}

func GetK8SClient() (client.Client, error) {
	return client.New(GetK8SConfig(), client.Options{Scheme: GetScheme()})
}

func GetDynamicClient() (dynamic.Interface, error) {
	return dynamic.NewForConfig(GetK8SConfig())
}

func GetProviderSecret(ctx context.Context, resourceRef *ResourceRef) (*v1.Secret, error) {
	if resourceRef == nil {
		return nil, fmt.Errorf("resource reference is nil")
	}

	k8sClient, err := GetK8SClient()
	if err != nil {
		return nil, errors.Wrap(err, "failed to get k8s client")
	}

	// attempt to find a default secret if we don't have a name
	if resourceRef.Namespace == "" {
		secretList := &v1.SecretList{}
		err = k8sClient.List(ctx, secretList, &client.ListOptions{
			LabelSelector: labels.SelectorFromSet(map[string]string{
				v1alpha1.DefaultProviderSecretLabel: "true",
			}),
			Namespace: resourceRef.Namespace,
		})
		if err != nil {
			return nil, errors.Wrap(err, "failed to list k8s secrets")
		}

		if len(secretList.Items) != 1 {
			return nil, fmt.Errorf("unexpected number of secrets: %d; expected 1 default secret\n", len(secretList.Items))
		}
		return &secretList.Items[0], nil
	}

	secret := &v1.Secret{}
	err = k8sClient.Get(ctx, client.ObjectKey{Name: resourceRef.Name, Namespace: resourceRef.Namespace}, secret)
	return secret, err
}
