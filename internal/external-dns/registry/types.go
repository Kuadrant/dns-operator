package registry

import "sigs.k8s.io/external-dns/endpoint"

type LabelsPacker interface {
	PackLabels(labelsPerOwner map[string]endpoint.Labels) endpoint.Labels
	UnpackLabels(labels endpoint.Labels) map[string]endpoint.Labels
	LabelsPacked(labels endpoint.Labels) (bool, error)
}

type nameMapper interface {
	toEndpointName(txtDNSName string) (endpointName, recordType, ownerID string)
	toTXTName(string, string, string) string
	recordTypeInAffix() bool
}

type Registry interface {
	GetLabelsPacker() LabelsPacker
	FilterEndpointsByOwnerID(ownerID string, list []*endpoint.Endpoint) []*endpoint.Endpoint
}
