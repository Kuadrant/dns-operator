package coredns

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
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
	DNSQueryFunc   QueryFunc
}

type QueryFunc func(hosts []string, nameserver string) (map[string]*dns.Msg, error)

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
			if err := validateNSAddress(ns); err != nil {
				return p, err
			}
			nameservers = append(nameservers, &ns)
		}
		p.nameservers = nameservers
	}
	if _, ok := s.Data["ZONES"]; ok {
		p.availableZones = strings.Split(strings.TrimSpace(string(s.Data["ZONES"])), ",")
	}
	p.availableZones = append(p.availableZones, provider.KuadrantTLD)
	p.DNSQueryFunc = p.dnsQuery
	return p, nil
}

func validateNSAddress(address string) error {
	// currently we only can work with an IP and port number
	nsParts := strings.Split(address, ":")
	if len(nsParts) != 2 {
		return fmt.Errorf("expected an IP and Port number in format 1.2.4.5:53 got %s", address)
	}
	if nil == net.ParseIP(nsParts[0]) {
		return fmt.Errorf("expected an IPAddress as nameserver address but got %s", nsParts[0])
	}
	if _, err := strconv.ParseUint(nsParts[1], 10, 64); err != nil {
		return fmt.Errorf("port number expected at end of nameserver but got %s", nsParts[1])
	}
	return nil
}

func (p CoreDNSProvider) Name() provider.DNSProviderName {
	return provider.DNSProviderCoreDNS
}

// DNSZones returns a list of dns zones accessible for this provider
func (p *CoreDNSProvider) DNSZones(ctx context.Context) ([]provider.DNSZone, error) {
	zones := []provider.DNSZone{}
	for id, zone := range p.availableZones {
		zones = append(zones, provider.DNSZone{
			ID:          fmt.Sprintf("id-%d", id),
			DNSName:     zone,
			NameServers: p.nameservers,
		})
	}
	return zones, nil
}

// DNSZoneForHost returns the DNSZone that best matches the given host in the providers list of zones
func (p *CoreDNSProvider) DNSZoneForHost(ctx context.Context, host string) (*provider.DNSZone, error) {
	var assignedZone *provider.DNSZone
	zones, _ := p.DNSZones(ctx)
	for _, z := range zones {
		if strings.HasSuffix(host, z.DNSName) {
			if assignedZone == nil {
				assignedZone = &z
			}
			if len(assignedZone.DNSName) < len(z.DNSName) {
				assignedZone = &z
			}
		}
	}
	if assignedZone == nil {
		return nil, provider.ErrNoZoneForHost
	}
	return assignedZone, nil
}

func (p *CoreDNSProvider) ProviderSpecific() provider.ProviderSpecificLabels {
	return provider.ProviderSpecificLabels{}
}

func (p *CoreDNSProvider) Records(ctx context.Context) ([]*endpoint.Endpoint, error) {
	// get local m. records and also the host records
	return []*endpoint.Endpoint{}, fmt.Errorf("no impl for core dns")
}

// TODO centralise
const kuadrantTLD = "kdrnt"

func (p *CoreDNSProvider) RecordsForHost(ctx context.Context, host string) ([]*endpoint.Endpoint, error) {
	// if host prefix matches our local prefix query nameservers to get other records and return merged endpoints
	// if host doesn't match prefix get the records from the resource directly.
	kudrantHost := fmt.Sprintf("%s.%s", host, kuadrantTLD)
	hosts := []string{kudrantHost, fmt.Sprintf("w.%s", kudrantHost), fmt.Sprintf("g.%s", kudrantHost)}
	var endpoints []*endpoint.Endpoint
	// do our queries and gather up the answers
	answers := map[string]map[string]*dns.Msg{}
	p.logger.Info("RecordsForHost", "total nameservers", len(p.nameservers), "hosts", hosts)
	for _, nServer := range p.nameservers {
		p.logger.Info("checking nameserver ", "address", *nServer)
		nsAnswer, err := p.DNSQueryFunc(hosts, *nServer)
		if err != nil {
			//TODO prob need to handle dns errors better here
			return endpoints, err
		}
		answers[*nServer] = nsAnswer
	}
	//merge the answers into a single set of endpoints
	for server, answer := range answers {
		dns := answer["dns"]

		if dns == nil {
			return nil, fmt.Errorf("expected a dns response but got none from  %s", server)
		}

		// merge the actual dns record answers
		if _, ok := answer["dns"]; !ok {
			continue
		}
		for _, rr := range answer["dns"].Answer {
			//need to remove duplicates where the dns name is the same and the targets are the same but keep duplicates where the targets are different
			endpoints = p.mergeDNSEndpoints(rr, endpoints)
		}
		if answer["weight"].Answer != nil {
			for _, rr := range answer["weight"].Answer {
				// these will be unique dns names
				addWeight(rr, endpoints)
			}
		}
		if answer["geo"].Answer != nil {
			for _, rr := range answer["geo"].Answer {
				if err := populateGEO(rr, endpoints); err != nil {
					return endpoints, err
				}
			}
		}
	}

	return endpoints, nil
}

func sanitize(host string) string {
	return strings.TrimSuffix(strings.TrimSpace(strings.TrimSuffix(host, ".")), "."+kuadrantTLD)
}

func (p *CoreDNSProvider) mergeDNSEndpoints(rr dns.RR, endpoints []*endpoint.Endpoint) []*endpoint.Endpoint {
	ep := &endpoint.Endpoint{
		DNSName:   sanitize(rr.Header().Name),
		Targets:   []string{},
		RecordTTL: endpoint.TTL(rr.Header().Ttl),
	}
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
		endpoints = append(endpoints, ep)
	}

	return endpoints
}

func populateGEO(rr dns.RR, endpoints []*endpoint.Endpoint) error {
	// very basic look for endpoints targeting endpoints starting with geo- add add provider specific data to those
	txt := rr.(*dns.TXT)
	if len(txt.Txt) != 3 {
		// return an error we expect 3 pieces of info
		return fmt.Errorf("expected 3 lines in the geo txt record but got %d", len(txt.Txt))
	}
	geo := getGeoData(txt)
	for _, ep := range endpoints {
		for _, target := range ep.Targets {
			if strings.HasPrefix(target, "geo-") {
				if geo.Default {
					ep.SetIdentifier = "default"
				}
				ep.ProviderSpecific = append(ep.ProviderSpecific, endpoint.ProviderSpecificProperty{
					Name:  "geo-code",
					Value: geo.Code,
				})

			}
		}
	}
	return nil
}

type geoData struct {
	Code    string
	Default bool
}

func getGeoData(txt *dns.TXT) geoData {
	gd := geoData{}
	for _, val := range txt.Txt {
		if strings.HasPrefix(val, "geo=") {
			gd.Code = strings.Replace(val, "geo=", "", -1)
		}
		if strings.HasPrefix(val, "default=") {
			val = strings.Replace(val, "default=", "", -1)
			gd.Default = strings.ToLower(strings.TrimSpace(val)) == "true"
		}
	}
	return gd
}

func addWeight(rr dns.RR, endpoints []*endpoint.Endpoint) {

	txt := rr.(*dns.TXT)

	if len(txt.Txt) == 1 {
		values := strings.Split(txt.Txt[0], ",")
		weight := values[0]
		dnsName := sanitize(values[1])
		for _, ep := range endpoints {
			if sanitize(ep.DNSName) == sanitize(dnsName) {
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

func (p *CoreDNSProvider) dnsQuery(hosts []string, nameserver string) (map[string]*dns.Msg, error) {
	queryType := dns.TypeA
	answers := map[string]*dns.Msg{}
	key := "dns"
	for _, host := range hosts {
		if strings.HasPrefix(host, "g.") {
			queryType = dns.TypeTXT
			key = "geo"
		}
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
