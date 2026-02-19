package output

import (
	"io"
	"os"
)

type OutputFormatter interface {
	Print(message string)
	Error(err error, message string)
	PrintObject(object any)
	PrintTable(array PrintableTable)
}

type PrintableTable struct {
	Headers []string
	Data    [][]string
}

var (
	Formatter            OutputFormatter
	registeredFormatters = make(map[string]func(io.Writer) OutputFormatter)
)

func init() {
	Formatter = &TextOutputFormatter{w: os.Stdout}
}

// RegisterOutputFormatter registers a formatter factory under the specific option. E.g. "json" for JsonFormatter
func RegisterOutputFormatter(option string, factory func(io.Writer) OutputFormatter) {
	registeredFormatters[option] = factory
}

// SetOutputFormatter will override default formatter with one of the registered providers
func SetOutputFormatter(option string, w io.Writer) {
	factory, ok := registeredFormatters[option]
	if ok {
		Formatter = factory(w)
	}
}
