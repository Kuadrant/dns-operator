package provider

import (
	"strings"

	"k8s.io/utils/net"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
)

func IsExternalAddress(address string, dnsRecord *v1alpha1.DNSRecord) bool {
	if net.IsIPv4String(address) {
		return true
	}

	return !strings.Contains(address, dnsRecord.Spec.RootHost)
}

func GetExternalAddresses(endpoint *externaldns.Endpoint, dnsRecord *v1alpha1.DNSRecord) (externalAddresses []string) {
	for _, a := range endpoint.Targets {
		if IsExternalAddress(a, dnsRecord) {
			externalAddresses = append(externalAddresses, a)
		}
	}
	return externalAddresses
}
