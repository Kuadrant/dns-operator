package registry

import "sigs.k8s.io/external-dns/endpoint"

type nameMapper interface {
	toEndpointName(txtDNSName, version string) (endpointName, recordType, ownerID string)
	toTXTName(string, string, string) string
	recordTypeInAffix() bool
}

type Registry interface {
	OwnerID() string
	FilterEndpointsByOwnerID(ownerID string, list []*endpoint.Endpoint) []*endpoint.Endpoint
}
