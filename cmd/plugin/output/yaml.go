package output

import "sigs.k8s.io/external-dns/endpoint"

type YAMLOutputFormatter struct {
	logLevel VerboseLevel
}

func NewYAMLOutputFormatter(logLevel VerboseLevel) *YAMLOutputFormatter {
	return &YAMLOutputFormatter{
		logLevel: logLevel,
	}
}
func (f YAMLOutputFormatter) Debug(message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) Info(message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) Warn(message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) DebugErr(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) Error(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) Panic(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) RenderEndpoints(endpoints []*endpoint.Endpoint) {
	//TODO implement me
	panic("implement me")
}
