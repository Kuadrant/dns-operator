package common

import (
	"fmt"
	"io"
	"slices"
	"time"

	"k8s.io/apimachinery/pkg/util/rand"
	externaldnsendpoint "sigs.k8s.io/external-dns/endpoint"
)

// RandomizeValidationDuration randomizes duration for a given variance with a min value of 1 sec
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeValidationDuration(variance float64, duration time.Duration) time.Duration {
	// do not allow less than a second requeue
	if duration.Milliseconds() < 1000 {
		duration = time.Second * 1
	}
	// we won't go smaller than a second - using milliseconds to have a relatively big number to randomize
	return RandomizeDuration(variance, float64(duration.Milliseconds()))
}

// RandomizeDuration randomizes duration for a given variance.
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeDuration(variance, duration float64) time.Duration {
	upperLimit := duration * (1.0 + variance)
	lowerLimit := duration * (1.0 - variance)

	return time.Millisecond * time.Duration(rand.Int63nRange(
		int64(lowerLimit),
		int64(upperLimit)))
}

func WriteEndpoints(s io.Writer, endpoints []*externaldnsendpoint.Endpoint, title string) {
	fmt.Fprintf(s, "\n====== %s ======\n", title)
	for _, ep := range endpoints {
		fmt.Fprintf(s, "	endpoint: %v > %+v with labels: %+v and flags: %+v\n", ep.DNSName, ep.Targets, ep.Labels, ep.ProviderSpecific)
	}
	fmt.Fprintf(s, "====== %s ======\n\n", title)
}

func EndpointsMatch(e1, e2 *externaldnsendpoint.Endpoint) bool {
	if e1.DNSName != e2.DNSName {
		return false
	}
	if e1.RecordTTL != e2.RecordTTL {
		return false
	}
	if e1.RecordType != e2.RecordType {
		return false
	}

	if len(e1.Targets) != len(e2.Targets) {
		return false
	}

	if len(e1.Labels) != len(e2.Labels) {
		return false
	}

	if len(e1.ProviderSpecific) != len(e2.ProviderSpecific) {
		return false
	}

	for _, t := range e1.Targets {
		if !slices.Contains(e2.Targets, t) {
			return false
		}
	}

	for l, e1v := range e1.Labels {
		if e2v, ok := e2.Labels[l]; !ok {
			return false
		} else if e1v != e2v {
			return false
		}
	}

	for _, p := range e1.ProviderSpecific {
		if !slices.ContainsFunc(e2.ProviderSpecific, func(e externaldnsendpoint.ProviderSpecificProperty) bool {
			return e.Name == p.Name && e.Value == p.Value
		}) {
			return false
		}
	}

	return true
}

func RemoveLabelFromEndpoint(label string, endpoint *externaldnsendpoint.Endpoint) *externaldnsendpoint.Endpoint {
	if endpoint.Labels == nil {
		return endpoint
	}

	delete(endpoint.Labels, label)
	return endpoint
}

func RemoveLabelFromEndpoints(label string, endpoints []*externaldnsendpoint.Endpoint) []*externaldnsendpoint.Endpoint {
	for _, e := range endpoints {
		RemoveLabelFromEndpoint(label, e)
	}
	return endpoints
}
func RemoveLabelsFromEndpoints(labels []string, endpoints []*externaldnsendpoint.Endpoint) []*externaldnsendpoint.Endpoint {
	for _, l := range labels {
		RemoveLabelFromEndpoints(l, endpoints)
	}

	return endpoints
}
