package endpoint

import (
	"errors"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/external-dns/endpoint"
)

type endpointAccessor struct {
	unstructuredObject *unstructured.Unstructured
	accessor           *endpointType
}

type endpointType struct {
	Spec struct {
		RootHost  string
		Endpoints []*endpoint.Endpoint
	}
}

var (
	RootHostLabel            = "kuadrant.io/rootHost"
	ErrNoSpecFound           = errors.New("no spec found in unstructured object")
	ErrSpecIsInvalid         = errors.New("spec is in an invalid format")
	ErrEndpointsAreInvalid   = errors.New("spec.endpoints is in an invalid format")
	ErrNoRootHost            = errors.New("root host not defined in spec.rootHost or in label: " + RootHostLabel)
	ErrRootHostInvalidFormat = errors.New("root host is defined in an invalid format, it must be of type string")
	ErrNoEndpoints           = errors.New("no endpoints array found in spec")
)

func NewEndpointAccessor(unst *unstructured.Unstructured) (*endpointAccessor, error) {
	ea := &endpointAccessor{unstructuredObject: unst}
	if err := ea.validateUnstructured(); err != nil {
		return nil, err
	}

	return ea, nil
}

func (ea *endpointAccessor) validateUnstructured() error {
	err := runtime.DefaultUnstructuredConverter.FromUnstructured(ea.unstructuredObject.Object, &ea.accessor)
	if err != nil {
		return err
	}
	return nil
}

func (ea *endpointAccessor) GetObject() *unstructured.Unstructured {
	return ea.unstructuredObject
}

func (ea *endpointAccessor) GetRootHost() string {
	return ea.accessor.Spec.RootHost
}

func (ea *endpointAccessor) GetEndpoints() []*endpoint.Endpoint {
	if ea.accessor.Spec.Endpoints == nil {
		ea.accessor.Spec.Endpoints = []*endpoint.Endpoint{}
	}

	return ea.accessor.Spec.Endpoints
}

func (ea *endpointAccessor) EnsureEndpoint(e *endpoint.Endpoint) error {
	if ea.accessor.Spec.Endpoints == nil {
		ea.accessor.Spec.Endpoints = []*endpoint.Endpoint{e}
		return ea.updateObjectEndpoints()
	}

	found := false
	for i, endpoint := range ea.accessor.Spec.Endpoints {
		if endpoint.Key() == e.Key() {
			ea.accessor.Spec.Endpoints[i] = e
			found = true
			break
		}
	}
	if !found {
		ea.accessor.Spec.Endpoints = append(ea.accessor.Spec.Endpoints, e)
	}

	return ea.updateObjectEndpoints()
}

func (ea *endpointAccessor) RemoveEndpoint(e *endpoint.Endpoint) error {
	for i, endpoint := range ea.accessor.Spec.Endpoints {
		if endpoint.Key() == e.Key() {
			ea.accessor.Spec.Endpoints = append(ea.accessor.Spec.Endpoints[:i], ea.accessor.Spec.Endpoints[i+1:]...)
			break
		}
	}

	return ea.updateObjectEndpoints()
}

// we only update the endpoints to avoid nuking spec fields that are required externally
func (ea *endpointAccessor) updateObjectEndpoints() error {
	newUnstructuredObject, err := runtime.DefaultUnstructuredConverter.ToUnstructured(ea.accessor)
	if err != nil {
		return err
	}
	ea.unstructuredObject.Object["spec"].(map[string]interface{})["endpoints"] = newUnstructuredObject["spec"].(map[string]interface{})["endpoints"]
	return nil
}
