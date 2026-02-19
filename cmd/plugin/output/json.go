package output

import "io"

type JSONOutputFormatter struct {
	_ io.Writer
}

var _ OutputFormatter = &JSONOutputFormatter{}

func init() {
	//RegisterOutputFormatter("json", &JSONOutputFormatter{})
}

func (f *JSONOutputFormatter) Print(message string) {
	//TODO implement me
	panic("implement me")
}

func (f *JSONOutputFormatter) Error(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f *JSONOutputFormatter) PrintObject(object any) {
	//TODO implement me
	panic("implement me")
}

func (f *JSONOutputFormatter) PrintTable(table PrintableTable) {
	//TODO implement me
	panic("implement me")
}
