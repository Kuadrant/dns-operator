package v1alpha1

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	externaldns "sigs.k8s.io/external-dns/endpoint"
	gatewayapiv1 "sigs.k8s.io/gateway-api/apis/v1"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

const (
	SimpleRoutingStrategy       RoutingStrategy = "simple"
	LoadBalancedRoutingStrategy RoutingStrategy = "loadbalanced"

	DefaultTTL      = 60
	DefaultCnameTTL = 300

	ClusterIDLength = 6

	LabelLBAttributeGeoCode = "kuadrant.io/lb-attribute-geo-code"
)

var (
	ErrUnknownRoutingStrategy = fmt.Errorf("unknown routing strategy")
)

// RoutingStrategy specifies a strategy to be used: simple or load-balanced
// +kubebuilder:validation:Enum=simple;loadbalanced
// +kubebuilder:validation:XValidation:rule="self == oldSelf",message="RoutingStrategy is immutable"
// +kubebuilder:default=loadbalanced
type RoutingStrategy string

type CustomWeight struct {
	Weight   int
	Selector v1.LabelSelector
}

// Routing holds all necessary information to generate endpoints
type Routing struct {
	Strategy RoutingStrategy
	// Default geo from policy
	GeoCode       string
	DefaultWeight int
	CustomWeights []CustomWeight
	ClusterID     string
}

func NewRouting(strategy RoutingStrategy, geoCode string, defaultWeight int, customWeights []CustomWeight, clusterID string) *Routing {
	return &Routing{Strategy: strategy, GeoCode: geoCode, DefaultWeight: defaultWeight, CustomWeights: customWeights, ClusterID: clusterID}
}

func GenerateEndpoints(gateway *gatewayapiv1.Gateway, dnsRecord *DNSRecord, listener gatewayapiv1.Listener, routing Routing) ([]*externaldns.Endpoint, error) {
	gwListenerHost := string(*listener.Hostname)
	var endpoints []*externaldns.Endpoint

	//Health Checks currently modify endpoints, so we have to keep existing ones in order to not lose health check ids
	currentEndpoints := make(map[string]*externaldns.Endpoint, len(dnsRecord.Spec.Endpoints))
	for _, endpoint := range dnsRecord.Spec.Endpoints {
		currentEndpoints[getSetID(endpoint)] = endpoint
	}

	switch routing.Strategy {
	case SimpleRoutingStrategy:
		endpoints = getSimpleEndpoints(gateway, gwListenerHost, currentEndpoints)
	case LoadBalancedRoutingStrategy:
		endpoints = getLoadBalancedEndpoints(gateway, routing, gwListenerHost, currentEndpoints)
	default:
		return nil, fmt.Errorf("%w : %s", ErrUnknownRoutingStrategy, routing.Strategy)
	}

	sort.Slice(endpoints, func(i, j int) bool {
		return getSetID(endpoints[i]) < getSetID(endpoints[j])
	})

	return endpoints, nil
}

// getSimpleEndpoints returns the endpoints for the given GatewayTarget using the simple routing strategy
func getSimpleEndpoints(gateway *gatewayapiv1.Gateway, hostname string, currentEndpoints map[string]*externaldns.Endpoint) []*externaldns.Endpoint {
	var (
		endpoints  []*externaldns.Endpoint
		ipValues   []string
		hostValues []string
	)

	for _, gwa := range gateway.Status.Addresses {
		if *gwa.Type == gatewayapiv1.IPAddressType {
			ipValues = append(ipValues, gwa.Value)
		} else {
			hostValues = append(hostValues, gwa.Value)
		}
	}

	if len(ipValues) > 0 {
		endpoint := createOrUpdateEndpoint(hostname, ipValues, ARecordType, "", DefaultTTL, currentEndpoints)
		endpoints = append(endpoints, endpoint)
	}

	if len(hostValues) > 0 {
		endpoint := createOrUpdateEndpoint(hostname, hostValues, CNAMERecordType, "", DefaultTTL, currentEndpoints)
		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

// getLoadBalancedEndpoints returns the endpoints for the given Gateway using the loadbalanced routing strategy
//
// Builds an array of externaldns.Endpoint resources. The endpoints expected are calculated using the Gateway
//and the Routing.
//
// A CNAME record is created for the target host (DNSRecord.name), pointing to a generated gateway lb host.
// A CNAME record for the gateway lb host is created with appropriate Geo information from Gateway
// A CNAME record for the geo specific host is created with weight information for that target added,
// pointing to a target cluster hostname.
// An A record for the target cluster hostname is created for any IP targets retrieved for that cluster.
//
// Example(Weighted only)
//
// www.example.com CNAME lb-1ab1.www.example.com
// lb-1ab1.www.example.com CNAME geolocation * default.lb-1ab1.www.example.com
// default.lb-1ab1.www.example.com CNAME weighted 100 1bc1.lb-1ab1.www.example.com
// default.lb-1ab1.www.example.com CNAME weighted 100 aws.lb.com
// 1bc1.lb-1ab1.www.example.com A 192.22.2.1
//
// Example(Geo, default IE)
//
// shop.example.com CNAME lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com CNAME geolocation ireland ie.lb-a1b2.shop.example.com
// lb-a1b2.shop.example.com geolocation default ie.lb-a1b2.shop.example.com (set by the default geo option)
// ie.lb-a1b2.shop.example.com CNAME weighted 100 ab1.lb-a1b2.shop.example.com
// ie.lb-a1b2.shop.example.com CNAME weighted 100 aws.lb.com
// ab1.lb-a1b2.shop.example.com A 192.22.2.1 192.22.2.2

func getLoadBalancedEndpoints(gateway *gatewayapiv1.Gateway, routing Routing, hostname string, currentEndpoints map[string]*externaldns.Endpoint) []*externaldns.Endpoint {
	cnameHost := hostname
	if isWildCardHost(hostname) {
		cnameHost = strings.Replace(hostname, "*.", "", -1)
	}

	var endpoint *externaldns.Endpoint
	endpoints := make([]*externaldns.Endpoint, 0)

	lbName := strings.ToLower(fmt.Sprintf("klb.%s", cnameHost))
	geoCode := getGeoFromLabel(gateway)
	geoLbName := strings.ToLower(fmt.Sprintf("%s.%s", geoCode, lbName))

	var ipValues []string
	var hostValues []string
	for _, gwa := range gateway.Status.Addresses {
		if *gwa.Type == gatewayapiv1.IPAddressType {
			ipValues = append(ipValues, gwa.Value)
		} else {
			hostValues = append(hostValues, gwa.Value)
		}
	}

	if len(ipValues) > 0 {
		clusterLbName := strings.ToLower(fmt.Sprintf("%s-%s.%s", getShortCode(routing.ClusterID), getShortCode(fmt.Sprintf("%s-%s", gateway.Name, gateway.Namespace)), lbName))
		endpoint = createOrUpdateEndpoint(clusterLbName, ipValues, ARecordType, "", DefaultTTL, currentEndpoints)
		endpoints = append(endpoints, endpoint)
		hostValues = append(hostValues, clusterLbName)
	}

	for _, hostValue := range hostValues {
		endpoint = createOrUpdateEndpoint(geoLbName, []string{hostValue}, CNAMERecordType, hostValue, DefaultTTL, currentEndpoints)
		endpoint.SetProviderSpecificProperty(ProviderSpecificWeight, strconv.Itoa(routing.getWeight(gateway)))
		endpoints = append(endpoints, endpoint)
	}

	// nothing to do
	if len(endpoints) == 0 {
		return endpoints
	}

	//Create lbName CNAME (lb-a1b2.shop.example.com -> <geoCode>.lb-a1b2.shop.example.com)
	endpoint = createOrUpdateEndpoint(lbName, []string{geoLbName}, CNAMERecordType, geoCode, DefaultCnameTTL, currentEndpoints)
	endpoint.SetProviderSpecificProperty(ProviderSpecificGeoCode, geoCode)
	endpoints = append(endpoints, endpoint)

	//Add a default geo (*) endpoint if the current geoCode is equal to the defaultGeo set in the policy spec
	//default geo is the default geo from spec
	if geoCode == routing.GeoCode {
		endpoint = createOrUpdateEndpoint(lbName, []string{geoLbName}, CNAMERecordType, "default", DefaultCnameTTL, currentEndpoints)
		endpoint.SetProviderSpecificProperty(ProviderSpecificGeoCode, WildcardGeo)
		endpoints = append(endpoints, endpoint)
	}

	if len(endpoints) > 0 {
		//Create gwListenerHost CNAME (shop.example.com -> lb-a1b2.shop.example.com)
		endpoint = createOrUpdateEndpoint(hostname, []string{lbName}, CNAMERecordType, "", DefaultCnameTTL, currentEndpoints)
		endpoints = append(endpoints, endpoint)
	}

	return endpoints
}

func createOrUpdateEndpoint(dnsName string, targets externaldns.Targets, recordType DNSRecordType, setIdentifier string,
	recordTTL externaldns.TTL, currentEndpoints map[string]*externaldns.Endpoint) (endpoint *externaldns.Endpoint) {
	ok := false
	endpointID := dnsName + setIdentifier
	if endpoint, ok = currentEndpoints[endpointID]; !ok {
		endpoint = &externaldns.Endpoint{}
		if setIdentifier != "" {
			endpoint.SetIdentifier = setIdentifier
		}
	}
	endpoint.DNSName = dnsName
	endpoint.RecordType = string(recordType)
	endpoint.Targets = targets
	endpoint.RecordTTL = recordTTL
	return endpoint
}

func getSetID(endpoint *externaldns.Endpoint) string {
	return endpoint.DNSName + endpoint.SetIdentifier
}

func isWildCardHost(host string) bool {
	return strings.HasPrefix(host, "*")
}

func getShortCode(name string) string {
	return hash.ToBase36HashLen(name, ClusterIDLength)
}

func getGeoFromLabel(gateway *gatewayapiv1.Gateway) string {
	// lb strategy
	if geoCode, found := gateway.GetLabels()[LabelLBAttributeGeoCode]; found {
		return geoCode
	}
	//simple strategy
	return DefaultGeo
}

func (r Routing) getWeight(gateway *gatewayapiv1.Gateway) int {
	weight := r.DefaultWeight
	for _, customWeight := range r.CustomWeights {
		selector, err := v1.LabelSelectorAsSelector(&customWeight.Selector)
		if err != nil {
			return weight
		}
		if selector.Matches(labels.Set(gateway.GetLabels())) {
			weight = customWeight.Weight
			break
		}
	}
	return weight
}
