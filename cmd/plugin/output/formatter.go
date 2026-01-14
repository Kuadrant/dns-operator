package output

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
	registeredFormatters = make(map[string]OutputFormatter)
)

func init() {
	Formatter = &TextOutputFormatter{}
}

// RegisterOutputFormatter registers formatter under the specific option. E.g. "json" for JsonFormatter
func RegisterOutputFormatter(option string, formatter OutputFormatter) {
	registeredFormatters[option] = formatter
}

// SetOutputFormatter will override default formatter with one of the registered providers
func SetOutputFormatter(option string) {
	formatter, ok := registeredFormatters[option]
	if ok {
		Formatter = formatter
	}
}
