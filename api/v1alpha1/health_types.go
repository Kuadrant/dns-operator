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

import "strings"

// HealthProtocol represents the protocol to use when making a health check request
type HealthProtocol string

const (
	HttpProtocol  HealthProtocol = "HTTP"
	HttpsProtocol HealthProtocol = "HTTPS"
)

func NewHealthProtocol(p string) HealthProtocol {
	switch strings.ToUpper(p) {
	case "HTTPS":
		return HttpsProtocol
	case "HTTP":
		return HttpProtocol
	}
	return HttpProtocol
}

func (p HealthProtocol) ToScheme() string {
	switch p {
	case HttpProtocol:
		return "http"
	case HttpsProtocol:
		return "https"
	default:
		return "http"
	}
}

func (p HealthProtocol) IsHttp() bool {
	return p == HttpProtocol
}

func (p HealthProtocol) IsHttps() bool {
	return p == HttpsProtocol
}
