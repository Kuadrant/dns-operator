//go:build unit

package v1alpha1

import (
	"testing"

	"sigs.k8s.io/external-dns/endpoint"
)

func TestValidate(t *testing.T) {
	tests := []struct {
		name     string
		rootHost string
		dnsNames []string
		wantErr  bool
	}{
		{
			name:     "invalid domain",
			rootHost: "example",
			wantErr:  true,
		},
		{
			name:     "no endpoints",
			rootHost: "example.com",
			wantErr:  true,
		},
		{
			name:     "invalid domain",
			rootHost: "example.com",
			dnsNames: []string{
				"example.com",
				"a.exmple.com",
			},
			wantErr: true,
		},
		{
			name:     "valid domain",
			rootHost: "example.com",
			dnsNames: []string{
				"example.com",
				"a.b.example.com",
				"b.a.example.com",
				"a.example.com",
				"b.example.com",
			},
			wantErr: false,
		},
		{
			name:     "valid wildcard domain",
			rootHost: "*.example.com",
			dnsNames: []string{
				"*.example.com",
				"a.b.example.com",
				"b.a.example.com",
				"a.example.com",
				"b.example.com",
			},
			wantErr: false,
		},
		{
			name:     "valid wildcard domain no endpoint",
			rootHost: "*.example.com",
			dnsNames: []string{
				"a.b.example.com",
				"b.a.example.com",
				"a.example.com",
				"b.example.com",
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			record := &DNSRecord{
				Spec: DNSRecordSpec{
					RootHost: tt.rootHost,
				},
			}
			for idx := range tt.dnsNames {
				record.Spec.Endpoints = append(record.Spec.Endpoints, &endpoint.Endpoint{DNSName: tt.dnsNames[idx]})
			}
			err := record.Validate()
			if (err != nil) != tt.wantErr {
				t.Errorf("Validate() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
		})
	}
}
