package output

import (
	"fmt"
	"strconv"

	externaldns "sigs.k8s.io/external-dns/endpoint"
)

type OutputFormatter interface {
	Debug(message string)
	Info(message string)
	Warn(message string)
	DebugErr(err error, message string)
	Error(err error, message string)
	Panic(err error, message string)
	RenderEndpoints(endpoints []*externaldns.Endpoint)
}

type VerboseLevel int8

func (v *VerboseLevel) String() string {
	return fmt.Sprintf("%d", v)
}

func (v *VerboseLevel) Set(s string) error {
	num, err := strconv.ParseInt(s, 10, 8)
	*v = VerboseLevel(num)
	return err
}

func (v *VerboseLevel) Type() string {
	return "verboseLevel"
}

const (
	DebugLevel VerboseLevel = iota - 1
	InfoLevel
	WarnLevel
	DebugErrorLevel
	ErrorLevel
	PanicLevel

	MinLevel     = DebugLevel
	MaxLevel     = PanicLevel
	DefaultLevel = int(InfoLevel)
)

var (
	Formatter OutputFormatter
)

func init() {
	Formatter = NewTextOutputFormatter(0)
}

func SetOutputFormatter(option string, verboseness int) {
	switch option {
	case "json":
		fmt.Println("TODO: JSON Output Formatter")
		//Formatter = NewJSONOutputFormatter(GetLevel(verboseness))
	case "yaml":
		fmt.Println("TODO: YAML Output Formatter")
		//Formatter = NewYAMLOutputFormatter(GetLevel(verboseness))
	default:
		Formatter = NewTextOutputFormatter(GetLevel(verboseness))
	}
}
