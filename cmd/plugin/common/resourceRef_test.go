//go:build unit

package common

import (
	"reflect"
	"testing"
)

func TestParseProviderRef(t *testing.T) {
	tests := []struct {
		name        string
		providerRef string
		want        *ResourceRef
		wantErr     bool
	}{
		{
			name:        "parses correct providerRef",
			providerRef: "namespace/name",
			want: &ResourceRef{
				Namespace: "namespace",
				Name:      "name",
			},
			wantErr: false,
		},
		{
			name:        "does not parse incomplete reference",
			providerRef: "namespace/",
			want:        nil,
			wantErr:     true,
		},
		{
			name:        "does not parse incomplete reference without slash",
			providerRef: "namespace",
			want:        nil,
			wantErr:     true,
		},
		{
			name:        "does not parse empty reference",
			providerRef: "",
			want:        nil,
			wantErr:     true,
		},
		{
			name:        "does not parse extra params",
			providerRef: "name/namespace/cat",
			want:        nil,
			wantErr:     true,
		},
		{
			name:        "does not parse extra params ending with slash",
			providerRef: "name/namespace/",
			want:        nil,
			wantErr:     true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ParseProviderRef(tt.providerRef)
			if (err != nil) != tt.wantErr {
				t.Errorf("ParseProviderRef() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("ParseProviderRef() got = %v, want %v", got, tt.want)
			}
		})
	}
}
