package output

type YAMLOutputFormatter struct {
}

func init() {
	//RegisterOutputFormatter("yaml", &YAMLOutputFormatter{})
}
func (f YAMLOutputFormatter) Print(message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) Error(err error, message string) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) PrintObject(object any) {
	//TODO implement me
	panic("implement me")
}

func (f YAMLOutputFormatter) PrintTable(array PrintableTable) {
	//TODO implement me
	panic("implement me")
}
