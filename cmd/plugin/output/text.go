package output

import (
	"fmt"
	"reflect"

	"gopkg.in/yaml.v3"
)

type TextOutputFormatter struct {
}

const (
	minTablePadding = 5
)

func init() {
	RegisterOutputFormatter("text", &TextOutputFormatter{})
}

func (f TextOutputFormatter) Print(message string) {
	fmt.Println(message)
}

func (f TextOutputFormatter) Error(err error, message string) {
	fmt.Println(fmt.Sprintf("%s: %s", message, err.Error()))

}

func (f TextOutputFormatter) PrintObject(object any) {
	reflectType := reflect.TypeOf(object)

	switch reflectType.Kind() {
	case reflect.Array, reflect.Slice:
		f.printArray(object)
	case reflect.Map:
		f.printMap(object)
	case reflect.Struct, reflect.Ptr:
		f.printStruct(object)
	default:
		fmt.Printf("%+v\n", object)
	}
}

func (f TextOutputFormatter) PrintTable(table PrintableTable) {
	columnPadding := make([]int, len(table.Headers))

	// this is not efficient, but we do not expect to process huge data structs here
	for columnIndex, header := range table.Headers {
		columnPadding[columnIndex] = len(header)
	}
	for rowIndex, row := range table.Data {
		if len(row) != len(table.Headers) {
			fmt.Printf("Can't print table. Expecting %d columns but row %d contains %d elements\n", len(table.Headers), rowIndex, len(table.Headers))
			return
		}

		for columnIndex, cell := range row {
			if len(cell) > columnPadding[columnIndex] {
				columnPadding[columnIndex] = len(cell)
			}
		}

	}

	for columnIndex, header := range table.Headers {
		fmt.Printf("%-*s", columnPadding[columnIndex]+minTablePadding, header)
	}
	fmt.Println()

	for _, row := range table.Data {
		for columnIndex, cell := range row {
			fmt.Printf("%-*s", columnPadding[columnIndex]+minTablePadding, cell)
		}
		fmt.Println()
	}
}

func (f TextOutputFormatter) printArray(object any) {
	s := reflect.ValueOf(object)
	for i := 0; i < s.Len(); i++ {
		fmt.Printf("%+v\n", s.Index(i))
	}
}

func (f TextOutputFormatter) printMap(object any) {
	m := reflect.ValueOf(object)
	keys := m.MapKeys()

	// Calculate max key width for alignment
	maxKeyWidth := 0
	for _, key := range keys {
		keyStr := fmt.Sprintf("%v", key.Interface())
		if len(keyStr) > maxKeyWidth {
			maxKeyWidth = len(keyStr)
		}
	}

	// Print with padding
	for _, key := range keys {
		fmt.Printf("%-*s : %+v\n", maxKeyWidth, fmt.Sprintf("%v", key.Interface()), m.MapIndex(key).Interface())
	}
}

func (f TextOutputFormatter) printStruct(object any) {
	out, err := yaml.Marshal(object)
	if err != nil {
		fmt.Printf("%+v\n", object)
		return
	}
	fmt.Println(string(out))
}
