package main

import (
	"fmt"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/coremain"

	_ "github.com/kuadrant/coredns-kuadrant"
	"github.com/kuadrant/coredns-kuadrant/cmd/plugin"
)

const pluginVersion = "0.0.0"

func init() {
	dnsserver.Directives = plugin.Directives
}

func main() {
	// extend CoreDNS version with plugin details
	caddy.AppVersion = fmt.Sprintf("%s+kuadrant-%s", coremain.CoreVersion, pluginVersion)
	coremain.Run()
}
