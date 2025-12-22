package output

type JSONOutputFormatter struct {
}

func init() {
	//RegisterOutputFormatter("json", &JSONOutputFormatter{})
}

func (f JSONOutputFormatter) Print(message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) Error(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) PrintObject(object any) {
	//TODO implement me
	panic("implement me")
}

func (f JSONOutputFormatter) PrintTable(array PrintableTable) {
	//TODO implement me
	panic("implement me")
}
