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
			result1, _ := GetCanonicalString(tt.input1)
			result2, _ := GetCanonicalString(tt.input2)
			if result1 != result2 {
				t.Errorf("GetCanonicalString() consistency failed:\nInput1: %v -> %s\nInput2: %v -> %s",
					tt.input1, result1, tt.input2, result2)
			}
		})
	}
}
