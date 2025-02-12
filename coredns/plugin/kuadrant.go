package kuadrant

import (
	"context"

	"github.com/coredns/coredns/plugin"
	clog "github.com/coredns/coredns/plugin/pkg/log"
	"github.com/miekg/dns"
)

var log = clog.NewWithPlugin(pluginName)

type Kuadrant struct {
	Next plugin.Handler
}

func (k Kuadrant) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	log.Debug("Received response")

	return plugin.NextOrFailure(k.Name(), k.Next, ctx, w, r)
}

func (k Kuadrant) Name() string {
	return pluginName
}
