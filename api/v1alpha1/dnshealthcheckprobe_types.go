/*
Copyright 2023 The MultiCluster Traffic Controller Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1alpha1

import (
	"fmt"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DNSHealthCheckProbeSpec defines the desired state of DNSHealthCheckProbe
type DNSHealthCheckProbeSpec struct {
	// Port to connect to the host on. Must be either 80, 443 or 1024-49151
	// +kubebuilder:validation:XValidation:rule="self in [80, 443] || (self >= 1024 && self <= 49151)",message="Only ports 80, 443, 1024-49151 are allowed"
	Port int `json:"port,omitempty"`
	// Hostname is the value sent in the host header, to route the request to the correct service
	// +kubebuilder:validation:Pattern=`^[a-z][a-z0-9\-]+\.([a-z][a-z0-9\-]+\.)*[a-z][a-z0-9\-]+$`
	Hostname string `json:"hostname,omitempty"`
	// Address to connect to the host on (IP Address (A Record) or hostname (CNAME)).
	// +kubebuilder:validation:Pattern=`^([1-9][0-9]?[0-9]?\.[0-9][0-9]?[0-9]?\.[0-9][0-9]?[0-9]?\.[0-9][0-9]?[0-9]|[a-z][a-z0-9\-]+\.([a-z][a-z0-9\-]+\.)*[a-z][a-z0-9\-]+)?$`
	Address string `json:"address,omitempty"`
	// Path is the path to append to the host to reach the expected health check.
	// Must start with "?" or "/", contain only valid URL characters and end with alphanumeric char or "/". For example "/" or "/healthz" are common
	// +kubebuilder:validation:Pattern=`^(?:\?|\/)[\w\-.~:\/?#\[\]@!$&'()*+,;=]+(?:[a-zA-Z0-9]|\/){1}$`
	Path string `json:"path,omitempty"`
	// Protocol to use when connecting to the host, valid values are "HTTP" or "HTTPS"
	// +kubebuilder:validation:XValidation:rule="self in ['HTTP','HTTPS']",message="Only HTTP or HTTPS protocols are allowed"
	Protocol Protocol `json:"protocol,omitempty"`
	// Interval defines how frequently this probe should execute
	Interval metav1.Duration `json:"interval,omitempty"`
	// AdditionalHeadersRef refers to a secret that contains extra headers to send in the probe request, this is primarily useful if an authentication
	// token is required by the endpoint.
	AdditionalHeadersRef *AdditionalHeadersRef `json:"additionalHeadersRef,omitempty"`
	// FailureThreshold is a limit of consecutive failures that must occur for a host to be considered unhealthy
	// +kubebuilder:validation:XValidation:rule="self > 0",message="Failure threshold must be greater than 0"
	FailureThreshold int `json:"failureThreshold,omitempty"`
	// AllowInsecureCertificate will instruct the health check probe to not fail on a self-signed or otherwise invalid SSL certificate
	// this is primarily used in development or testing environments
	AllowInsecureCertificate bool `json:"allowInsecureCertificate,omitempty"`
}

type AdditionalHeadersRef struct {
	Name string `json:"name"`
}

type AdditionalHeaders []AdditionalHeader

type AdditionalHeader struct {
	Name  string `json:"name"`
	Value string `json:"value"`
}

// DNSHealthCheckProbeStatus defines the observed state of DNSHealthCheckProbe
type DNSHealthCheckProbeStatus struct {
	LastCheckedAt       metav1.Time `json:"lastCheckedAt"`
	ConsecutiveFailures int         `json:"consecutiveFailures,omitempty"`
	Reason              string      `json:"reason,omitempty"`
	Status              int         `json:"status,omitempty"`
	Healthy             *bool       `json:"healthy"`
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:printcolumn:name="Healthy",type="boolean",JSONPath=".status.healthy",description="DNSHealthCheckProbe healthy."
//+kubebuilder:printcolumn:name="Last Checked",type="date",JSONPath=".status.lastCheckedAt",description="Last checked at."

// DNSHealthCheckProbe is the Schema for the dnshealthcheckprobes API
type DNSHealthCheckProbe struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   DNSHealthCheckProbeSpec   `json:"spec,omitempty"`
	Status DNSHealthCheckProbeStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// DNSHealthCheckProbeList contains a list of DNSHealthCheckProbe
type DNSHealthCheckProbeList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []DNSHealthCheckProbe `json:"items"`
}

func (p *DNSHealthCheckProbe) Default() {
	if p.Spec.Protocol == "" {
		p.Spec.Protocol = HttpProtocol
	}
}

func (p *DNSHealthCheckProbe) ToString() string {
	return fmt.Sprintf("%v://%v:%v%v", p.Spec.Protocol, p.Spec.Hostname, p.Spec.Port, p.Spec.Path)
}

func init() {
	SchemeBuilder.Register(&DNSHealthCheckProbe{}, &DNSHealthCheckProbeList{})
}
