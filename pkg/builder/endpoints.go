package builder

import (
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	externaldns "sigs.k8s.io/external-dns/endpoint"

	"github.com/kuadrant/dns-operator/api/v1alpha1"
	"github.com/kuadrant/dns-operator/internal/common/hash"
)

const (
	IPAddressType       AddressType = "IPAddress"
	HostnameAddressType AddressType = "Hostname"
	// HostnameRegex checks for at least two groups of allowed for URLs characters separated by "."
	HostnameRegex        = "^(?:[\\w\\-.~:\\/?#[\\]@!$&'()*+,;=]+)\\.(?:[\\w\\-.~:\\/?#[\\]@!$&'()*+,;=]+)$"
	WildcardGeo   string = "*"

	DefaultTTL      = 60
	DefaultCnameTTL = 300
	IDLength        = 6
)

// Target wraps a kubernetes ingress traffic resource e.g.Gateway, Ingress, Route etc.. but can wrap any resources
// that has the desired geo and weight labels being applied, and can provide the required target addresses data.
// This should be implemented as required for each type of ingress resource i.e. Gateway
type Target interface {
	metav1.Object
	GetAddresses() []TargetAddress
}

// EndpointsBuilder builds an endpoints array.
type EndpointsBuilder struct {
	// target kubernetes resource that may have geo/weight labels applied and provides target addresses.
	target Target

	// hostname to be used for creation of endpoints. There could be multiple hostname values for a
	// single target. This builder delegates burden of determining valid hostnames and managing
	// an array of endpoints for each of hostname values to the consumer of this API
	hostname string

	// loadBalancing specification (Optional),
	// If set the builder will create a loadbalanced set of endpoints for the target resource.
	// If unset, the builder will create a simple set of endpoints for the target resource.
	loadBalancing *LoadBalancing
}

type AddressType string

type TargetAddress struct {
	Type  AddressType
	Value string
}

type LoadBalancing struct {
	// Weight is the record weight to use when no other can be determined for a dns target cluster.
	// The maximum value accepted is determined by the target dns provider.
	Weight int

	// Geo is the country/continent/region code to use when no other can be determined for a dns target cluster.
	// The values accepted are determined by the target dns provider, please refer to the appropriate docs below.
	//
	// Route53: https://docs.aws.amazon.com/Route53/latest/DeveloperGuide/resource-record-sets-values-geo.html
	// Google: https://cloud.google.com/compute/docs/regions-zones
	Geo string

	// IsDefaultGeo specifies if this is the default geo for providers that support setting a default catch all geo endpoint such as Route53
	IsDefaultGeo bool

	// Id is a way to distinguish endpoints created for the same target
	// with the same hostname for a different cluster
	Id string
}

// NewEndpointsBuilder returns a new endpoints builder
func NewEndpointsBuilder(target Target, hostname string) *EndpointsBuilder {
	return &EndpointsBuilder{
		target:   target,
		hostname: hostname,
	}
}

// WithLoadBalancing provides builder with necessary parameters to generate a load-balancing set of endpoints.
// If not used, the builder will provide a simple set of endpoints
func (builder *EndpointsBuilder) WithLoadBalancing(loadbalancing *LoadBalancing) *EndpointsBuilder {
	builder.loadBalancing = loadbalancing
	return builder
}

// WithLoadBalancingFor performs identically to WithLoadBalancing but without the need to parse params in LoadBalancing struct
func (builder *EndpointsBuilder) WithLoadBalancingFor(id string, weight int, geo string, isDefaultGeo bool) *EndpointsBuilder {
	return builder.WithLoadBalancing(&LoadBalancing{
		Weight:       weight,
		Geo:          geo,
		IsDefaultGeo: isDefaultGeo,
		Id:           id,
	})
}

// Build returns a list of endpoints created based on the current configuration of the builder
func (builder *EndpointsBuilder) Build() ([]*externaldns.Endpoint, error) {
	if err := builder.Validate(); err != nil {
		return nil, err
	}

	var endpoints []*externaldns.Endpoint

	// no load-balancing provided, inferring simple strategy
	if builder.loadBalancing == nil {
		endpoints = builder.getSimpleEndpoints()
	} else {
		// load-balancing present, inferring load-balanced strategy
		endpoints = builder.getLoadBalancedEndpoints()
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return getSetID(endpoints[i]) < getSetID(endpoints[j])
	})
	return endpoints, nil
}

// Validate set of parameters passed inside the builder.
// Does not ensure validity of values, but checks for a correct set of inputs.
// e.g. will not check for invalid address type, but ensure they are not nil
func (builder *EndpointsBuilder) Validate() error {
	if matched, err := regexp.MatchString(HostnameRegex, builder.hostname); !matched {
		// This only possible if HostnameRegex is modified.
		// Leave it here as a precaution
		if err != nil {
			return fmt.Errorf("error parsing regexp to match hostname: %w", err)
		}
		return fmt.Errorf("invalid hostname")
	}

	if builder.target == nil {
		return fmt.Errorf("must provide target")
	}

	if builder.target.GetAddresses() == nil {
		return fmt.Errorf("must provide addresses")
	}

	// following only applicable for load-balancing strategy
	if builder.loadBalancing != nil {
		// Id must not be an empty string
		if builder.loadBalancing.Id == "" {
			return fmt.Errorf("ID is required")
		}

		// default weight and geo are required
		if builder.loadBalancing.Weight < 0 {
			return fmt.Errorf("invalid default weight")
		}
		if builder.loadBalancing.Geo == "" {
			return fmt.Errorf("default geocode is required")
		}
	}
	return nil
}

// getSimpleEndpoints returns the endpoints for the given GatewayTarget using the simple routing strategy
func (builder *EndpointsBuilder) getSimpleEndpoints() []*externaldns.Endpoint {
	var endpoints []*externaldns.Endpoint

	ipValues, hostValues := targetsFromAddresses(builder.target.GetAddresses())

	if len(ipValues) > 0 {
		endpoint := createEndpoint(builder.hostname, ipValues, v1alpha1.ARecordType, "", DefaultTTL)
		endpoints = append(endpoints, endpoint)
	}

	if len(hostValues) > 0 {
		endpoint := createEndpoint(builder.hostname, hostValues, v1alpha1.CNAMERecordType, "", DefaultTTL)
		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

// getLoadBalancedEndpoints returns the endpoints for the given Gateway using the loadbalanced routing strategy
//
// Builds an array of externaldns.Endpoint resources. The endpoints expected are calculated using the Gateway
// and the Routing.
//
// A CNAME record is created for the target host (DNSRecord.name), pointing to a generated gateway lb host.
// A CNAME record for the gateway lb host is created with appropriate Geo information from Gateway
// A CNAME record for the geo specific host is created with weight information for that target added,
// pointing to a target cluster hostname.
// An A record for the target cluster hostname is created for any IP targets retrieved for that cluster.
//
// # Example
//
// shop.example.com CNAME lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com CNAME geolocation ireland ie.lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com geolocation default ie.lb-a1b2.shop.example.com (set by the default geo option)
// ie.lb-a1b2.shop.example.com CNAME weighted 100 ab1.lb-a1b2.shop.example.com
// ie.lb-a1b2.shop.example.com CNAME weighted 100 aws.lb.com
// ab1.lb-a1b2.shop.example.com A 192.22.2.1 192.22.2.2
func (builder *EndpointsBuilder) getLoadBalancedEndpoints() []*externaldns.Endpoint {
	cnameHost := builder.hostname
	if isWildCardHost(builder.hostname) {
		cnameHost = strings.Replace(builder.hostname, "*.", "", -1)
	}

	var endpoint *externaldns.Endpoint
	endpoints := make([]*externaldns.Endpoint, 0)

	lbName := strings.ToLower(fmt.Sprintf("klb.%s", cnameHost))
	geoCode := builder.loadBalancing.Geo
	geoLbName := strings.ToLower(fmt.Sprintf("%s.%s", geoCode, lbName))

	ipValues, hostValues := targetsFromAddresses(builder.target.GetAddresses())

	if len(ipValues) > 0 {
		aRecordLbName := strings.ToLower(fmt.Sprintf("%s-%s.%s", getShortCode(builder.loadBalancing.Id), getShortCode(fmt.Sprintf("%s-%s", builder.target.GetName(), builder.target.GetNamespace())), lbName))
		endpoint = createEndpoint(aRecordLbName, ipValues, v1alpha1.ARecordType, "", DefaultTTL)
		endpoints = append(endpoints, endpoint)
		hostValues = append(hostValues, aRecordLbName)
	}

	for _, hostValue := range hostValues {
		endpoint = createEndpoint(geoLbName, []string{hostValue}, v1alpha1.CNAMERecordType, hostValue, DefaultTTL)
		endpoint.SetProviderSpecificProperty(v1alpha1.ProviderSpecificWeight, strconv.Itoa(int(builder.loadBalancing.Weight)))
		endpoints = append(endpoints, endpoint)
	}

	// nothing to do
	if len(endpoints) == 0 {
		return endpoints
	}

	//Create lbName CNAME (lb-a1b2.shop.example.com -> <geoCode>.lb-a1b2.shop.example.com)
	endpoint = createEndpoint(lbName, []string{geoLbName}, v1alpha1.CNAMERecordType, geoCode, DefaultCnameTTL)
	endpoint.SetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode, geoCode)
	endpoints = append(endpoints, endpoint)

	//Add a default geo (*) endpoint if the current geoCode is a default geo
	if builder.loadBalancing.IsDefaultGeo {
		endpoint = createEndpoint(lbName, []string{geoLbName}, v1alpha1.CNAMERecordType, "default", DefaultCnameTTL)
		endpoint.SetProviderSpecificProperty(v1alpha1.ProviderSpecificGeoCode, WildcardGeo)
		endpoints = append(endpoints, endpoint)
	}

	if len(endpoints) > 0 {
		//Create gwListenerHost CNAME (shop.example.com -> lb-a1b2.shop.example.com)
		endpoint = createEndpoint(builder.hostname, []string{lbName}, v1alpha1.CNAMERecordType, "", DefaultCnameTTL)
		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

func createEndpoint(dnsName string, targets externaldns.Targets, recordType v1alpha1.DNSRecordType, setIdentifier string,
	recordTTL externaldns.TTL) (endpoint *externaldns.Endpoint) {
	return &externaldns.Endpoint{
		DNSName:       dnsName,
		Targets:       targets,
		RecordType:    string(recordType),
		SetIdentifier: setIdentifier,
		RecordTTL:     recordTTL,
	}
}

func getSetID(endpoint *externaldns.Endpoint) string {
	return endpoint.DNSName + endpoint.SetIdentifier
}

func isWildCardHost(host string) bool {
	return strings.HasPrefix(host, "*")
}

func getShortCode(name string) string {
	return hash.ToBase36HashLen(name, IDLength)
}

func targetsFromAddresses(addresses []TargetAddress) ([]string, []string) {
	var ipValues []string
	var hostValues []string

	for _, address := range addresses {
		if address.Type == IPAddressType {
			ipValues = append(ipValues, address.Value)
		} else {
			hostValues = append(hostValues, address.Value)
		}
	}

	return ipValues, hostValues
}
