package main

import (
	"fmt"
	"runtime/debug"

	"github.com/coredns/caddy"
	"github.com/coredns/coredns/core/dnsserver"
	"github.com/coredns/coredns/coremain"
	clog "github.com/coredns/coredns/plugin/pkg/log"

	_ "github.com/kuadrant/coredns-kuadrant"
	"github.com/kuadrant/coredns-kuadrant/cmd/plugin"
)

var (
	gitSHA        string // must be string as passed in by ldflag
	pluginVersion string // must be string as passed in by ldflag
)

func init() {
	dnsserver.Directives = plugin.Directives
}

func main() {
	clog.Info(fmt.Sprintf("plugin version: %s, gitSha: %s", pluginVersion, gitSHA))
	buildInfo, ok := debug.ReadBuildInfo()
	if ok {
		clog.Info(fmt.Sprintf("go version: %s", buildInfo.GoVersion))
		clog.Info(fmt.Sprintf("arch: %s", getSetting("GOARCH", buildInfo.Settings)))
	}

	// extend CoreDNS version with plugin details
	caddy.AppVersion = fmt.Sprintf("%s+kuadrant-%s", coremain.CoreVersion, pluginVersion)
	coremain.Run()
}

// Helper function to find a specific setting.
func getSetting(key string, settings []debug.BuildSetting) string {
	for _, setting := range settings {
		if setting.Key == key {
			return setting.Value
		}
	}
	return "N/A"
}
