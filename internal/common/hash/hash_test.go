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
