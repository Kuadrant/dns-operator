package hash

import "testing"

func TestToBase36Hash(t *testing.T) {
	tests := []struct {
		name  string
		input string
		want  string
	}{
		{
			name:  "converts string to base36 hash",
			input: "9c8f876c-4ddc-44a3-9842-460f97e6c037",
			want:  "32ah7xkbrefse005knttdnvhaybhyeezwh0d6cln7r6l",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToBase36Hash(tt.input); got != tt.want {
				t.Errorf("ToBase36Hash() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestToBase36HashLen(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		length int
		want   string
	}{
		{
			name:   "converts string to base36 hash with length",
			input:  "9c8f876c-4ddc-44a3-9842-460f97e6c037",
			length: 8,
			want:   "32ah7xkb",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ToBase36HashLen(tt.input, tt.length); got != tt.want {
				t.Errorf("ToBase36HashLen() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetCanonicalString tests the GetCanonicalString function for all supported data types.
// AI generated; Claude Sonnet 4
func TestGetCanonicalString(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		// Basic types
		{
			name:  "string value",
			input: "hello",
			want:  `"hello"`,
		},
		{
			name:  "empty string",
			input: "",
			want:  `""`,
		},
		{
			name:  "integer value",
			input: 42,
			want:  "42",
		},
		{
			name:  "negative integer",
			input: -10,
			want:  "-10",
		},
		{
			name:  "float value",
			input: 3.14,
			want:  "3.14",
		},
		{
			name:  "boolean true",
			input: true,
			want:  "true",
		},
		{
			name:  "boolean false",
			input: false,
			want:  "false",
		},

		// Pointers
		{
			name:  "nil pointer",
			input: (*string)(nil),
			want:  "null",
		},
		{
			name:  "pointer to string",
			input: func() *string { s := "test"; return &s }(),
			want:  `"test"`,
		},
		{
			name:  "pointer to int",
			input: func() *int { i := 100; return &i }(),
			want:  "100",
		},

		// Slices - order should not matter due to sorting
		{
			name:  "string slice",
			input: []string{"b", "a", "c"},
			want:  `["a","b","c"]`,
		},
		{
			name:  "int slice",
			input: []int{3, 1, 2},
			want:  "[1,2,3]",
		},
		{
			name:  "empty slice",
			input: []string{},
			want:  "[]",
		},

		// Arrays
		{
			name:  "string array",
			input: [3]string{"z", "a", "m"},
			want:  `["a","m","z"]`,
		},

		// Maps - key order should not matter due to sorting
		{
			name:  "string map",
			input: map[string]int{"beta": 2, "alpha": 1, "gamma": 3},
			want:  `{alpha:1,beta:2,gamma:3}`,
		},
		{
			name:  "empty map",
			input: map[string]int{},
			want:  "{}",
		},

		// Structs
		{
			name: "simple struct",
			input: struct {
				Name string
				Age  int
			}{Name: "John", Age: 30},
			want: `{Name:"John",Age:30}`,
		},
		{
			name: "struct with slice",
			input: struct {
				Items []string
				Count int
			}{Items: []string{"b", "a"}, Count: 2},
			want: `{Items:["a","b"],Count:2}`,
		},

		// Complex nested structures
		{
			name: "nested struct",
			input: struct {
				User struct {
					Name string
					ID   int
				}
				Active bool
			}{
				User: struct {
					Name string
					ID   int
				}{Name: "Alice", ID: 1},
				Active: true,
			},
			want: `{User:{Name:"Alice",ID:1},Active:true}`,
		},
		{
			name: "map with struct values",
			input: map[string]struct {
				Value int
				Valid bool
			}{
				"second": {Value: 2, Valid: true},
				"first":  {Value: 1, Valid: false},
			},
			want: `{first:{Value:1,Valid:false},second:{Value:2,Valid:true}}`,
		},
		{
			name:  "slice of maps",
			input: []map[string]int{{"b": 2, "a": 1}, {"d": 4, "c": 3}},
			want:  `[{a:1,b:2},{c:3,d:4}]`,
		},

		// Edge cases and different types
		{
			name:  "uint value",
			input: uint64(42),
			want:  "42",
		},
		{
			name:  "float32 value",
			input: float32(2.5),
			want:  "2.5",
		},
		// Additional edge cases
		{
			name:  "zero values",
			input: 0,
			want:  "0",
		},
		{
			name:  "zero float",
			input: 0.0,
			want:  "0",
		},
		{
			name:  "negative float",
			input: -3.14159,
			want:  "-3.14159",
		},
		{
			name:  "very large int",
			input: int64(9223372036854775807),
			want:  "9223372036854775807",
		},
		{
			name:  "string with special characters",
			input: "hello\nworld\t\"quoted\"",
			want:  `"hello\nworld\t\"quoted\""`,
		},
		{
			name:  "mixed type slice gets converted to strings",
			input: []any{"string", 42, true},
			want:  `["string",42,true]`,
		},
		{
			name: "struct with zero values",
			input: struct {
				Name   string
				Age    int
				Active bool
			}{},
			want: `{Name:"",Age:0,Active:false}`,
		},
		{
			name:  "nil slice",
			input: []string(nil),
			want:  "[]",
		},
		{
			name:  "nil map",
			input: map[string]int(nil),
			want:  "{}",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCanonicalString(tt.input); got != tt.want {
				t.Errorf("GetCanonicalString() = %v, want %v", got, tt.want)
			}
		})
	}
}

// TestGetCanonicalString_Consistency verifies that the same data produces identical
// canonical strings regardless of input ordering (critical for hash stability).
// AI generated; Claude Sonnet 4
func TestGetCanonicalString_Consistency(t *testing.T) {
	// Test that the same data produces the same canonical string regardless of order
	tests := []struct {
		name   string
		input1 any
		input2 any
	}{
		{
			name:   "slice order independence",
			input1: []string{"a", "b", "c"},
			input2: []string{"c", "a", "b"},
		},
		{
			name:   "map order independence",
			input1: map[string]int{"x": 1, "y": 2, "z": 3},
			input2: map[string]int{"z": 3, "x": 1, "y": 2},
		},
		{
			name: "complex nested order independence",
			input1: struct {
				Tags  []string
				Props map[string]bool
			}{
				Tags:  []string{"tag1", "tag2", "tag3"},
				Props: map[string]bool{"enabled": true, "visible": false},
			},
			input2: struct {
				Tags  []string
				Props map[string]bool
			}{
				Tags:  []string{"tag3", "tag1", "tag2"},
				Props: map[string]bool{"visible": false, "enabled": true},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result1 := GetCanonicalString(tt.input1)
			result2 := GetCanonicalString(tt.input2)
			if result1 != result2 {
				t.Errorf("GetCanonicalString() consistency failed:\nInput1: %v -> %s\nInput2: %v -> %s",
					tt.input1, result1, tt.input2, result2)
			}
		})
	}
}

// TestGetCanonicalString_InvalidTypes tests error handling for unsupported data types.
// AI generated; Claude Sonnet 4
func TestGetCanonicalString_InvalidTypes(t *testing.T) {
	tests := []struct {
		name  string
		input any
		want  string
	}{
		{
			name:  "nil interface",
			input: nil,
			want:  "null",
		},
		{
			name:  "channel type",
			input: make(chan int),
			want:  "null",
		},
		{
			name:  "function type",
			input: func() {},
			want:  "null",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := GetCanonicalString(tt.input); got != tt.want {
				t.Errorf("GetCanonicalString() = %v, want %v", got, tt.want)
			}
		})
	}
}
