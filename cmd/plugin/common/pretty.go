package common

import (
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"

	externaldns "sigs.k8s.io/external-dns/endpoint"
)

func RenderEndpoints(endpoints []*externaldns.Endpoint) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.SetStyle(table.StyleLight)
	t.AppendHeader(table.Row{"Type", "Record", "Targets", "TTL"})

	for _, e := range endpoints {
		var targets string
		switch e.RecordType {
		case externaldns.RecordTypeA:
			targets = strings.ReplaceAll(e.Targets.String(), ";", "\n")
		case externaldns.RecordTypeNS:
			targets = strings.ReplaceAll(e.Targets.String(), ";", "\n")
		case externaldns.RecordTypeTXT:
			targets = strings.Trim(e.Targets.String(), "\"")
			targets = strings.ReplaceAll(targets, ",", "\n")
		default:
			targets = e.Targets.String()
		}

		t.AppendRow([]any{e.RecordType, e.DNSName, targets, e.RecordTTL})
		t.AppendSeparator()
	}
	t.Render()
}
