package hash

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sort"
	"strconv"
	"strings"

	"github.com/martinlindhe/base36"
)

func ToBase36Hash(s string) string {
	hash := sha256.Sum224([]byte(s))
	// convert the hash to base36 (alphanumeric) to decrease collision probabilities
	return strings.ToLower(base36.EncodeBytes(hash[:]))
}

func ToBase36HashLen(s string, l int) string {
	return ToBase36Hash(s)[:l]
}

// GetCanonicalString creates a stable, sorted string representation of any variable.
// This is the core function that handles the recursive traversal and sorting.
//
// AI generated; Gemimi 2.5 Pro
func GetCanonicalString(v any) string {
	val := reflect.ValueOf(v)

	// Follow pointers
	if val.Kind() == reflect.Ptr {
		if val.IsNil() {
			return "null"
		}
		// Recursively call with the element the pointer points to.
		return GetCanonicalString(val.Elem().Interface())
	}

	// Use a switch to handle different data types.
	switch val.Kind() {
	case reflect.Struct:
		var parts []string
		// Iterate over the fields of the struct.
		for i := 0; i < val.NumField(); i++ {
			field := val.Type().Field(i)
			fieldValue := val.Field(i)

			// Format as "key:value" and recursively process the field's value.
			part := fmt.Sprintf("%s:%s", field.Name, GetCanonicalString(fieldValue.Interface()))
			parts = append(parts, part)
		}
		// Struct fields have a defined order, so we don't need to sort `parts`.
		return fmt.Sprintf("{%s}", strings.Join(parts, ","))

	case reflect.Slice, reflect.Array:
		var parts []string
		// Iterate over the elements of the slice/array.
		for i := 0; i < val.Len(); i++ {
			// Recursively get the canonical string for each element.
			parts = append(parts, GetCanonicalString(val.Index(i).Interface()))
		}
		// IMPORTANT: Sort the string representations of the elements to ensure order doesn't matter.
		sort.Strings(parts)
		return fmt.Sprintf("[%s]", strings.Join(parts, ","))

	case reflect.Map:
		var parts []string
		var keys []string
		// Extract all map keys into a slice.
		for _, key := range val.MapKeys() {
			keys = append(keys, key.String())
		}
		// IMPORTANT: Sort the keys to ensure consistent iteration order.
		sort.Strings(keys)

		// Iterate over the sorted keys.
		for _, key := range keys {
			mapValue := val.MapIndex(reflect.ValueOf(key))
			// Format as "key:value" and recursively process the map's value.
			part := fmt.Sprintf("%s:%s", key, GetCanonicalString(mapValue.Interface()))
			parts = append(parts, part)
		}
		return fmt.Sprintf("{%s}", strings.Join(parts, ","))

	case reflect.String:
		// Quote strings to distinguish them from other types.
		return strconv.Quote(val.String())

	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(val.Int(), 10)

	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(val.Uint(), 10)

	case reflect.Float32, reflect.Float64:
		return strconv.FormatFloat(val.Float(), 'f', -1, 64)

	case reflect.Bool:
		return strconv.FormatBool(val.Bool())

	case reflect.Invalid, reflect.Chan, reflect.Func, reflect.UnsafePointer, reflect.Uintptr:
		// Handle nil or unsupported types gracefully.
		return "null"

	default:
		// Fallback for any other type.
		return fmt.Sprintf("%v", v)
	}
}
