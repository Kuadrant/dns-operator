package common

import (
	"fmt"
	"io"
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
