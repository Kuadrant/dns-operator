package output

import (
	"fmt"

	"gopkg.in/yaml.v3"
)

const (
	fileSeparator = "---"

	printTypeText   = "text"
	printTypeObject = "object"
)

type YAMLOutputFormatter struct {
	lastPrintType string
}

var _ OutputFormatter = &YAMLOutputFormatter{}

func init() {
	RegisterOutputFormatter("yaml", &YAMLOutputFormatter{})
}
func (f *YAMLOutputFormatter) Print(message string) {
	if f.lastPrintType == printTypeObject {
		fmt.Println(fileSeparator)
	}
	fmt.Printf("- message: %s\n", message)
	f.lastPrintType = printTypeText

}

func (f *YAMLOutputFormatter) Error(err error, message string) {
	if f.lastPrintType == printTypeObject {
		fmt.Println(fileSeparator)
	}
	fmt.Printf("- error: %s\n  message: %s\n", err.Error(), message)
	f.lastPrintType = printTypeText
}

func (f *YAMLOutputFormatter) PrintObject(object any) {
	fmt.Println(fileSeparator)
	out, err := yaml.Marshal(object)
	if err != nil {
		fmt.Printf("- error marshalling object: %s\n", err)
		f.lastPrintType = printTypeText
		return
	}
	fmt.Println(string(out))
	f.lastPrintType = printTypeObject
}

func (f *YAMLOutputFormatter) PrintTable(table PrintableTable) {
	// validate table
	for rowIndex, row := range table.Data {
		if len(row) != len(table.Headers) {
			if f.lastPrintType != printTypeText {
				fmt.Println(fileSeparator)
			}
			fmt.Printf("- error printing table. Expecting %d columns but row %d contains %d elements\n", len(table.Headers), rowIndex, len(row))
			return
		}
	}

	fmt.Println(fileSeparator)

	// Convert each row into a map keyed by header names
	var items []map[string]string
	for _, row := range table.Data {
		item := make(map[string]string, len(table.Headers))
		for i, header := range table.Headers {
			item[header] = row[i]
		}
		items = append(items, item)
	}

	out, err := yaml.Marshal(items)
	if err != nil {
		fmt.Printf("- error marshalling table: %s\n", err)
		f.lastPrintType = printTypeText
		return
	}
	fmt.Print(string(out))
	f.lastPrintType = printTypeObject

}
