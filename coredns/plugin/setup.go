package kuadrant

import (
	"context"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
)

const pluginName = "kuadrant"

var log = clog.NewWithPlugin(pluginName)

// init registers this plugin.
func init() { plugin.Register(pluginName, setup) }

// setup is the function that gets called when the config parser see the token "kuadrant". Setup is responsible
// for parsing any extra options the example plugin may have. The first token this function sees is "kuadrant".
func setup(c *caddy.Controller) error {

	k, err := parse(c)
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	err = k.RunKubeController(context.Background())
	if err != nil {
		return plugin.Error(pluginName, err)
	}

	// Add the Plugin to CoreDNS, so Servers can use it in their plugin chain.
	dnsserver.GetConfig(c).AddPlugin(func(next plugin.Handler) plugin.Handler {
		k.Next = next
		return k
	})

	// All OK, return a nil error.
	return nil
}

func parse(c *caddy.Controller) (*Kuadrant, error) {
	k := newKuadrant()

	z := make(map[string]*Zone)
	names := []string{}
	rname := "" // Default empty, will use "hostmaster" if not set

	for c.Next() {
		origins := plugin.OriginsFromArgsOrServerBlock(c.RemainingArgs(), c.ServerBlockKeys)

		for c.NextBlock() {
			key := c.Val()
			args := c.RemainingArgs()
			if len(args) == 0 {
				return k, c.ArgErr()
			}
			switch key {
			case "kubeconfig":
				k.ConfigFile = args[0]
				if len(args) == 2 {
					k.ConfigContext = args[1]
				}
			case "rname":
				rname = args[0]
			default:
				return k, c.Errf("Unknown property '%s'", c.Val())
			}
		}

		// Create zones with rname after parsing config
		for i := range origins {
			z[origins[i]] = NewZone(origins[i], rname)
			names = append(names, origins[i])
		}
	}

	k.Zones = Zones{Z: z, Names: names}

	return k, nil
}
