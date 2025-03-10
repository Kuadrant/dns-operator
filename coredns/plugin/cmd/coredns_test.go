package main

import (
	"testing"

	"github.com/coredns/coredns/core/dnsserver"
)

func Test_init(t *testing.T) {

	var found bool

	for _, included := range dnsserver.Directives {
		if included == "kuadrant" {
			found = true
			break
		}
	}

	if !found {
		t.Errorf("'kuadrant' plugin is not found in the list")
	}
}
