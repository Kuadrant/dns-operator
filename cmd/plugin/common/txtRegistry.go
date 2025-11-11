package common

import (
	"context"
	"time"

	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/internal/external-dns/registry"
	"github.com/kuadrant/dns-operator/internal/provider"
)

// this is a duplicate of the TXT registry setup we use in the dns operator
// the only difference is that we can assume identity of any owner.

const (
	txtRegistryPrefix              = "kuadrant-"
	txtRegistrySuffix              = ""
	txtRegistryWildcardReplacement = "wildcard"
	txtRegistryEncryptEnabled      = false
	txtRegistryEncryptAESKey       = ""
	txtRegistryCacheInterval       = time.Duration(0)
)

var (
	managedDNSRecordTypes = []string{externaldnsendpoint.RecordTypeA, externaldnsendpoint.RecordTypeAAAA, externaldnsendpoint.RecordTypeCNAME}
	excludeDNSRecordTypes []string
)

func GetDefaultTXTRegistry(ctx context.Context, dnsProvider provider.Provider, ownerID string) (*registry.TXTRegistry, error) {
	return registry.NewTXTRegistry(ctx, dnsProvider, txtRegistryPrefix, txtRegistrySuffix,
		ownerID, txtRegistryCacheInterval, txtRegistryWildcardReplacement, managedDNSRecordTypes,
		excludeDNSRecordTypes, txtRegistryEncryptEnabled, []byte(txtRegistryEncryptAESKey))
}
