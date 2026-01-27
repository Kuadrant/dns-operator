package kuadrant

import (
	"testing"

	"github.com/coredns/caddy"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseArguments(t *testing.T) {
	var tests = []struct {
		name             string
		configuration    string
		expectedError    bool
		expectedKuadrant *Kuadrant
	}{
		{
			"valid args",
			`
						kuadrant {
							kubeconfig foo.kubeconfig bar
						}`,
			false,
			&Kuadrant{
				Zones: Zones{
					Z:     map[string]*Zone{},
					Names: make([]string, 0),
				},
				ConfigFile:    "foo.kubeconfig",
				ConfigContext: "bar",
			},
		},
		{
			"empty args",
			`
						kuadrant {
						}`,
			false,
			&Kuadrant{
				Zones: Zones{
					Z:     map[string]*Zone{},
					Names: make([]string, 0),
				},
			},
		},
		{
			"valid args with rname",
			`
						kuadrant {
							rname admin@example.com
						}`,
			false,
			&Kuadrant{
				Zones: Zones{
					Z:     map[string]*Zone{},
					Names: make([]string, 0),
				},
			},
		},
		{
			"invalid args",
			`
						kuadrant {
							foo bar
						}`,
			true,
			&Kuadrant{},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", test.configuration)
			kuadrant, err := parse(c)
			require.Equal(t, test.expectedError, err != nil)
			assert.Equal(t, test.expectedKuadrant, kuadrant)
		})
	}
}

func TestSetup_ZoneWithRNAME(t *testing.T) {
	tests := []struct {
		name          string
		configuration string
		zoneName      string
		expectedRNAME string
	}{
		{
			name: "zone with custom rname",
			configuration: `kuadrant example.com {
							rname dns.admin@example.com
						}`,
			zoneName:      "example.com.",
			expectedRNAME: "dns\\.admin.example.com.",
		},
		{
			name:          "zone without rname uses default",
			configuration: `kuadrant example.com`,
			zoneName:      "example.com.",
			expectedRNAME: "hostmaster.example.com.",
		},
		{
			name: "zone with simple rname",
			configuration: `kuadrant test.org {
							rname admin@test.org
						}`,
			zoneName:      "test.org.",
			expectedRNAME: "admin.test.org.",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			c := caddy.NewTestController("dns", test.configuration)
			kuadrant, err := parse(c)
			require.NoError(t, err)
			require.NotNil(t, kuadrant)

			// Get the zone from the parsed configuration
			zone, ok := kuadrant.Zones.Z[test.zoneName]
			require.True(t, ok, "Zone should exist in parsed configuration")
			require.NotNil(t, zone)

			// Get the SOA record from the zone
			zone.RLock()
			soaRecord := zone.Apex.SOA
			zone.RUnlock()

			require.NotNil(t, soaRecord, "SOA record should exist")
			assert.Equal(t, test.expectedRNAME, soaRecord.Mbox, "SOA RNAME should match expected value")
		})
	}
}
