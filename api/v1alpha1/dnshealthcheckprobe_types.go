/*
Copyright 2024.

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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// DNSHealthCheckProbeSpec defines the desired state of DNSHealthCheckProbe
type DNSHealthCheckProbeSpec struct {
	Port                     int                   `json:"port,omitempty"`
	Host                     string                `json:"host,omitempty"`
	Address                  string                `json:"address,omitempty"`
	Path                     string                `json:"path,omitempty"`
	Protocol                 HealthProtocol        `json:"protocol,omitempty"`
	Interval                 metav1.Duration       `json:"interval,omitempty"`
	AdditionalHeadersRef     *AdditionalHeadersRef `json:"additionalHeadersRef,omitempty"`
	FailureThreshold         *int                  `json:"failureThreshold,omitempty"`
	ExpectedResponses        []int                 `json:"expectedResponses,omitempty"`
	AllowInsecureCertificate bool                  `json:"allowInsecureCertificate,omitempty"`
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

func init() {
	SchemeBuilder.Register(&DNSHealthCheckProbe{}, &DNSHealthCheckProbeList{})
}
