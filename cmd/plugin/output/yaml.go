package output

import (
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

const (
	fileSeparator = "---"
)

type YAMLOutputFormatter struct {
	w io.Writer
}

var _ OutputFormatter = &YAMLOutputFormatter{}

func init() {
	RegisterOutputFormatter("yaml", func(w io.Writer) OutputFormatter {
		return &YAMLOutputFormatter{w: w}
	})
}

func (f *YAMLOutputFormatter) Print(message string) {
	fmt.Fprintln(f.w, fileSeparator)
	out, err := yaml.Marshal(map[string]string{"message": message})
	if err != nil {
		fmt.Fprintf(f.w, "message: %s\n", message)
		return
	}
	fmt.Fprint(f.w, string(out))
}

func (f *YAMLOutputFormatter) Error(err error, message string) {
	fmt.Fprintln(f.w, fileSeparator)
	out, marshalErr := yaml.Marshal(map[string]string{"error": err.Error(), "message": message})
	if marshalErr != nil {
		fmt.Fprintf(f.w, "error: %s\nmessage: %s\n", err.Error(), message)
		return
	}
	fmt.Fprint(f.w, string(out))
}

func (f *YAMLOutputFormatter) PrintObject(object any) {
	fmt.Fprintln(f.w, fileSeparator)
	out, err := yaml.Marshal(object)
	if err != nil {
		fmt.Fprintf(f.w, "error marshalling object: %s\n", err)
		return
	}
	fmt.Fprint(f.w, string(out))
}

func (f *YAMLOutputFormatter) PrintTable(table PrintableTable) {
	fmt.Fprintln(f.w, fileSeparator)

	// validate table
	for rowIndex, row := range table.Data {
		if len(row) != len(table.Headers) {
			fmt.Fprintf(f.w, "error printing table. Expecting %d columns but row %d contains %d elements\n", len(table.Headers), rowIndex, len(row))
			return
		}
	}

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
		fmt.Fprintf(f.w, "error marshalling table: %s\n", err)
		return
	}
	fmt.Fprint(f.w, string(out))
}
