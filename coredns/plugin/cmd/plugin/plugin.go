package plugin

// Cut down version of https://github.com/coredns/coredns/blob/master/core/plugin/zplugin.go
// Only import plugins we want to enable in our build of CoreDNS.
// Note: Any plugin removed here should have it's Directive removed also.

import (
	_ "github.com/coredns/coredns/plugin/cache"
	_ "github.com/coredns/coredns/plugin/cancel"
	_ "github.com/coredns/coredns/plugin/debug"
	_ "github.com/coredns/coredns/plugin/errors"
	_ "github.com/coredns/coredns/plugin/file"
	_ "github.com/coredns/coredns/plugin/forward"
	_ "github.com/coredns/coredns/plugin/geoip"
	_ "github.com/coredns/coredns/plugin/header"
	_ "github.com/coredns/coredns/plugin/health"
	_ "github.com/coredns/coredns/plugin/hosts"
	_ "github.com/coredns/coredns/plugin/local"
	_ "github.com/coredns/coredns/plugin/log"
	_ "github.com/coredns/coredns/plugin/loop"
	_ "github.com/coredns/coredns/plugin/metadata"
	_ "github.com/coredns/coredns/plugin/metrics"
	_ "github.com/coredns/coredns/plugin/minimal"
	_ "github.com/coredns/coredns/plugin/multisocket"
	_ "github.com/coredns/coredns/plugin/nsid"
	_ "github.com/coredns/coredns/plugin/ready"
	_ "github.com/coredns/coredns/plugin/reload"
	_ "github.com/coredns/coredns/plugin/rewrite"
	_ "github.com/coredns/coredns/plugin/root"
	_ "github.com/coredns/coredns/plugin/timeouts"
	_ "github.com/coredns/coredns/plugin/tls"
	_ "github.com/coredns/coredns/plugin/view"
	_ "github.com/coredns/coredns/plugin/whoami"
)

// Directives are registered in the order they should be
// executed.
//
// Ordering is VERY important. Every plugin will
// feel the effects of all other plugin below
// (after) them during a request, but they must not
// care what plugin above them are doing.
var Directives = []string{
	"root",
	"metadata",
	"geoip",
	"cancel",
	"tls",
	"timeouts",
	"multisocket",
	"reload",
	"nsid",
	"debug",
	"ready",
	"health",
	"prometheus",
	"errors",
	"log",
	"local",
	"cache",
	"rewrite",
	"header",
	"minimal",
	"hosts",
	"kuadrant",
	"file",
	"loop",
	"forward",
	"whoami",
	"view",
}
