package main

import (
	"fmt"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	_ "github.com/coredns/coredns/core/plugin"
	"github.com/coredns/coredns/coremain"

	_ "github.com/kuadrant/coredns-kuadrant"
)

var dropPlugins = map[string]bool{
	"kubernetes":   true,
	"k8s_external": true,
}

const pluginVersion = "0.0.0"

// https://github.com/ori-edge/k8s_gateway/blob/master/cmd/coredns.go
func init() {
	var directives []string
	var alreadyAdded bool

	for _, name := range dnsserver.Directives {

		if dropPlugins[name] {
			if !alreadyAdded {
				directives = append(directives, "kuadrant")
				alreadyAdded = true
			}
			continue
		}
		directives = append(directives, name)
	}

	dnsserver.Directives = directives

}

func main() {
	// extend CoreDNS version with plugin details
	caddy.AppVersion = fmt.Sprintf("%s+kuadrant-%s", coremain.CoreVersion, pluginVersion)
	coremain.Run()
}
