package output

import "sigs.k8s.io/external-dns/endpoint"

type JSONOutputFormatter struct {
	logLevel VerboseLevel
}

func NewJSONOutputFormatter(logLevel VerboseLevel) *JSONOutputFormatter {
	return &JSONOutputFormatter{
		logLevel: logLevel,
	}
}
func (f JSONOutputFormatter) Debug(message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) Info(message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) Warn(message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) DebugErr(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) Error(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) Panic(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) RenderEndpoints(endpoints []*endpoint.Endpoint) {
	//TODO implement me
	panic("implement me")
}
