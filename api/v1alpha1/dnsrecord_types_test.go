package v1alpha1

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"sigs.k8s.io/external-dns/endpoint"
)

func TestDNSRecord_GetRootDomain(t *testing.T) {
	tests := []struct {
		name     string
		dnsNames []string
		want     string
		wantErr  bool
	}{
		{
			name: "single endpoint",
			dnsNames: []string{
				"test.example.com",
			},
			want:    "test.example.com",
			wantErr: false,
		},
		{
			name: "multiple endpoints matching",
			dnsNames: []string{
				"bar.baz.test.example.com",
				"bar.test.example.com",
				"test.example.com",
				"foo.bar.baz.test.example.com",
			},
			want:    "test.example.com",
			wantErr: false,
		},
		{
			name:     "no endpoints",
			dnsNames: []string{},
			want:     "",
			wantErr:  true,
		},
		{
			name: "multiple endpoints mismatching",
			dnsNames: []string{
				"foo.bar.test.example.com",
				"bar.test.example.com",
				"baz.example.com",
			},
			want:    "",
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &DNSRecord{
				TypeMeta: metav1.TypeMeta{
					Kind:       "DNSRecord",
					APIVersion: GroupVersion.String(),
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "testRecord",
					Namespace: "testNS",
				},
				Spec: DNSRecordSpec{
					Endpoints: []*endpoint.Endpoint{},
				},
			}
			for idx := range tt.dnsNames {
				s.Spec.Endpoints = append(s.Spec.Endpoints, &endpoint.Endpoint{DNSName: tt.dnsNames[idx]})
			}
			got, err := s.GetRootDomain()
			if (err != nil) != tt.wantErr {
				t.Errorf("GetRootDomain() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got != tt.want {
				t.Errorf("GetRootDomain() got = %v, want %v", got, tt.want)
			}
		})
	}
}
