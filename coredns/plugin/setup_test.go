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
