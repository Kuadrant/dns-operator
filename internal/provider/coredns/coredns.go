package coredns

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"

	"github.com/go-logr/logr"
	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/miekg/dns"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/external-dns/endpoint"
	"sigs.k8s.io/external-dns/plan"
)

type CoreDNSProvider struct {
	logger         logr.Logger
	nameservers    []*string
	availableZones []string
}

var p provider.Provider = &CoreDNSProvider{}

// Register this Provider with the provider factory
func init() {
	provider.RegisterProvider(p.Name().String(), NewCoreDNSProviderFromSecret, true)
}

func NewCoreDNSProviderFromSecret(ctx context.Context, s *v1.Secret, c provider.Config) (provider.Provider, error) {
	logger := log.FromContext(ctx).WithName(p.Name().String())
	p := &CoreDNSProvider{
		logger: logger,
	}
	if _, ok := s.Data["NAMESERVERS"]; ok {
		nameservers := []*string{}
		nservers := strings.Split(strings.TrimSpace(string(s.Data["NAMESERVERS"])), ",")
		for _, ns := range nservers {
			nameservers = append(nameservers, &ns)
		}
		p.nameservers = nameservers
	}
	if _, ok := s.Data["ZONES"]; ok {
		p.availableZones = strings.Split(strings.TrimSpace(string(s.Data["ZONES"])), ",")
	}
	p.availableZones = append(p.availableZones, provider.KuadrantTLD)
	return p, nil
}

func (p CoreDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderCoreDNS
}

// DNSZones returns a list of dns zones accessible for this provider
func (p *CoreDNSProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	return []provider.DNSZone{}, nil
}

// DNSZoneForHost returns the DNSZone that best matches the given host in the providers list of zones
func (p *CoreDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	assignedZone := ""
	for _, z := range p.availableZones {
		if strings.HasSuffix(host, z) {
			if len(assignedZone) < len(z) {
				assignedZone = z
			}
		}
	}
	if assignedZone == "" {
		return nil, fmt.Errorf("no available zone")
	}
	return &provider.DNSZone{
		ID:          assignedZone,
		DNSName:     assignedZone, // todo this might need to be added to the dns provider secret
		NameServers: p.nameservers,
		RecordCount: 0, //todo
	}, nil
}

func (p *CoreDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *CoreDNSProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	// get local m. records and also the host records
	return []*endpoint.Endpoint{}, nil
}

// TODO centralise
const kuadrantTLD = "kdrnt"

func (p *CoreDNSProvider) RecordsForHost(ctx context.Context, host string) ([]*endpoint.Endpoint, error) {
	// if host prefix matches our local prefix query nameservers to get other records and return merged endpoints
	// if host doesn't match prefix get the records from the resource directly.
	kudrantHost := fmt.Sprintf("%s.%s", host, kuadrantTLD)
	hosts := []string{kudrantHost, fmt.Sprintf("w.%s", kudrantHost)}
	var endpoints []*endpoint.Endpoint
	// do our queries and gather up the answers
	answers := map[string]map[string]*dns.Msg{}
	for _, nServer := range p.nameservers {
		nsAnswer, err := dnsQuery(hosts, *nServer)
		if err != nil {
			//TODO prob need to handle dns errors better here
			return endpoints, err
		}
		answers[*nServer] = nsAnswer
	}
	//merge the answers into a single set of endpoints
	for _, answer := range answers {
		// merge the actual dns record answers
		if _, ok := answer["dns"]; !ok {
			continue
		}
		for _, rr := range answer["dns"].Answer {
			//need to remove duplicates where the dns name is the same and the targets are the same but keep duplicates where the targets are different
			endpoints = p.mergeDNSEndpoints(rr, endpoints)
		}

		for _, rr := range answer["weight"].Answer {
			// these will be unique dns names
			addWeight(rr, endpoints)
		}
	}
	populateGEO(endpoints)
	json, _ := json.MarshalIndent(endpoints, "", " ")
	fmt.Println("merged endpoints", string(json))
	// TODO validate that the endpoints are coherent and all logically end without going to dead ends
	return endpoints, nil
}

func sanitize(host string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(host, ".")), "."+kuadrantTLD)
}

func (p *CoreDNSProvider) mergeDNSEndpoints(rr dns.RR, endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	fmt.Println("merging endpoints ")
	ep := &endpoint.Endpoint{
		DNSName:   sanitize(rr.Header().Name),
		Targets:   []string{},
		RecordTTL: endpoint.TTL(rr.Header().Ttl),
	}
	fmt.Println("rr", rr)
	switch rec := rr.(type) {
	case *dns.A:
		ep.RecordType = "A"
		ep.RecordTTL = endpoint.TTL(rec.Header().Ttl)
		ep.Targets = append(ep.Targets, string(rec.A.String()))
	case *dns.CNAME:
		ep.RecordType = "CNAME"
		ep.Targets = append(ep.Targets, sanitize(rec.Target))
	case *dns.TXT:
		ep.RecordType = "TXT"
		ep.Targets = rec.Txt
	default:
		p.logger.Info("not handling record of type %v ", rec)
	}

	alreadyExists := false
	for _, exising := range endpoints {
		if exising.DNSName == ep.DNSName && slices.Equal(exising.Targets, ep.Targets) {
			alreadyExists = true
			break
		}
	}
	if !alreadyExists {
		fmt.Println("adding ep ", ep)
		endpoints = append(endpoints, ep)
	} else {
		fmt.Println("not adding ep as it exists ", ep)
	}

	return endpoints
}

func populateGEO(endpoints []*endpoint.Endpoint) {
	// very basic look for endpoints targeting endpoints starting with geo- add add provider specific data to those
	for _, ep := range endpoints {
		for _, target := range ep.Targets {
			if strings.HasPrefix(target, "geo-") {
				fmt.Println("found geo target adding geo provider specific data")
				parts := strings.Split(target, ".")
				ep.ProviderSpecific = append(ep.ProviderSpecific, endpoint.ProviderSpecificProperty{
					Name:  "geo-code",
					Value: strings.ToUpper(parts[0]),
				})
			}
		}
	}
}

func addWeight(rr dns.RR, endpoints []*endpoint.Endpoint) {

	txt := rr.(*dns.TXT)

	if len(txt.Txt) == 1 {
		values := strings.Split(txt.Txt[0], ",")
		weight := values[0]
		dnsName := sanitize(values[1])
		fmt.Println("adding weights", txt.Txt, dnsName, weight)
		for _, ep := range endpoints {
			fmt.Println("dnsname ", ep.DNSName)
			if sanitize(ep.DNSName) == sanitize(dnsName) {
				fmt.Println("found match adding providerspecific weight for ", ep)
				ep.ProviderSpecific = []endpoint.ProviderSpecificProperty{
					{
						Name:  "weight",
						Value: weight,
					},
				}
			}
		}
	}
}

func dnsQuery(hosts []string, nameserver string) (map[string]*dns.Msg, error) {
	queryType := dns.TypeA
	answers := map[string]*dns.Msg{}
	key := "dns"
	for _, host := range hosts {
		fmt.Println("querying host ", host, "@ ns", nameserver)
		if strings.HasPrefix(host, "w.") {
			queryType = dns.TypeTXT
			key = "weight"
		}
		if strings.HasPrefix(host, "d.") {
			queryType = dns.TypeTXT
			key = "defaultGeo"
		}

		dnsMsg := new(dns.Msg)
		fqdn := fmt.Sprintf("%s.", host) // Convert to true FQDN with dot at the end
		dnsMsg.SetQuestion(fqdn, queryType)
		msg, err := dns.Exchange(dnsMsg, nameserver)
		if err != nil {
			return answers, fmt.Errorf("%w failed to do dns exchange with nameserver %s ", err, nameserver)
		}
		answers[key] = msg

	}
	return answers, nil
}

func (p *CoreDNSProvider) ApplyChanges(ctx context.Context, changes *plan.Changes) error {
	return fmt.Errorf("core dns does not use a plan")
}

// AdjustEndpoints canonicalizes a set of candidate endpoints.
// It is called with a set of candidate endpoints obtained from the various sources.
// It returns a set modified as required by the provider. The provider is responsible for
// adding, removing, and modifying the ProviderSpecific properties to match
// the endpoints that the provider returns in `Records` so that the change plan will not have
// unnecessary (potentially failing) changes. It may also modify other fields, add, or remove
// Endpoints. It is permitted to modify the supplied endpoints.
func (p *CoreDNSProvider) AdjustEndpoints(endpoints []*endpoint.Endpoint) ([]*endpoint.Endpoint, error) {
	return []*endpoint.Endpoint{}, nil
}
func (p *CoreDNSProvider) GetDomainFilter() endpoint.DomainFilter {
	return endpoint.DomainFilter{}
}
