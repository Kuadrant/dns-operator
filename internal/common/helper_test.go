package common

import (
	"testing"
)

func TestFormatRootHost(t *testing.T) {
	tests := []struct {
		name     string
		rootHost string
		want     string
	}{
		{
			name:     "converts short root host to hash with length 8",
			rootHost: "pb.com",
			want:     "jsys0tw1",
		},
		{
			name:     "converts long root host to hash with length 8",
			rootHost: "123456789-123456789-123456789-123456789-123456789-123456789-123456789-123456789.pb.com",
			want:     "d4ns4xlx",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := HashRootHost(tt.rootHost); got != tt.want {
				t.Errorf("HashRootHost() = %v, want %v", got, tt.want)
			}
		})
	}
}
