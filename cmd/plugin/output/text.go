package output

import (
	"fmt"
	"os"
	"strings"

	"github.com/jedib0t/go-pretty/v6/table"

	externaldns "sigs.k8s.io/external-dns/endpoint"
)

type TextOutputFormatter struct {
	logLevel VerboseLevel
}

func NewTextOutputFormatter(logLevel VerboseLevel) *TextOutputFormatter {
	return &TextOutputFormatter{
		logLevel: logLevel,
	}
}

func (f TextOutputFormatter) Debug(message string) {
	if f.logLevel <= DebugLevel {
		fmt.Println(message)
	}
}

func (f TextOutputFormatter) Info(message string) {
	if f.logLevel <= InfoLevel {
		fmt.Println(message)
	}
}

func (f TextOutputFormatter) Warn(message string) {
	if f.logLevel <= WarnLevel {
		fmt.Println(message)
	}
}

func (f TextOutputFormatter) DebugErr(err error, message string) {
	if f.logLevel <= DebugErrorLevel {
		fmt.Println(fmt.Sprintf("%s: %s", message, err.Error()))
	}
}

func (f TextOutputFormatter) Error(err error, message string) {
	if f.logLevel <= ErrorLevel {
		fmt.Println(fmt.Sprintf("%s: %s", message, err.Error()))
	}
}

func (f TextOutputFormatter) Panic(err error, message string) {
	panic(fmt.Sprintf("%s: %s", message, err.Error()))
}

func (f TextOutputFormatter) RenderEndpoints(endpoints []*externaldns.Endpoint) {

	// only render for debug and info
	if f.logLevel > InfoLevel {
		return
	}

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
