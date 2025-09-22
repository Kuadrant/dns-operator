package coredns_test

import (
	"context"
	"testing"

	v1 "k8s.io/api/core/v1"

	"github.com/kuadrant/dns-operator/internal/provider"
	"github.com/kuadrant/dns-operator/internal/provider/coredns"
)

func TestCoreDNSProvider_DNSZoneForHost(t *testing.T) {
	testCases := []struct {
		Name             string
		Secret           *v1.Secret
		Host             string
		ExpectedZoneRoot string
		ExpectedErr      func(err error) bool
	}{
		{
			Name: "test correct zone returned",
			Host: "api.k.example.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES": []byte("k.example.com,example.com"),
				},
			},
			ExpectedZoneRoot: "k.example.com",
			ExpectedErr: func(err error) bool {
				return err == nil
			},
		},
		{
			Name: "test correct zone returned",
			Host: "api.k.example.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES": []byte("example.com,k.other.com"),
				},
			},
			ExpectedZoneRoot: "example.com",
			ExpectedErr: func(err error) bool {
				return err == nil
			},
		},
		{
			Name: "test error when no zone found",
			Host: "api.k.other.com",
			Secret: &v1.Secret{
				Data: map[string][]byte{
					"ZONES": []byte("k.example.com,example.com"),
				},
			},
			ExpectedZoneRoot: "",
			ExpectedErr: func(err error) bool {
				return err == provider.ErrNoZoneForHost
			},
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.Name, func(t *testing.T) {
			p, err := coredns.NewCoreDNSProviderFromSecret(context.Background(), testCase.Secret, provider.Config{})
			if err != nil {
				t.Fatalf("failed to create new core dns provider %s", err)
			}
			z, err := p.DNSZoneForHost(context.Background(), testCase.Host)
			if err != nil && !testCase.ExpectedErr(err) {
				t.Fatalf("error was not as expected %s ", err)
			}
			if err == nil && z.DNSName != testCase.ExpectedZoneRoot {
				t.Fatalf("expected the zone to be %s but got %s", testCase.ExpectedZoneRoot, z.DNSName)
			}

		})
	}
}
