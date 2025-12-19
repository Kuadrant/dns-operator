//go:build unit

package output

import (
	"testing"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name            string
		verboseness     int
		expectV0Enabled bool
		expectV1Enabled bool
	}{
		{
			name:            "verboseness set to default (info level)",
			verboseness:     0,
			expectV0Enabled: true,
			expectV1Enabled: false,
		},
		{
			name:            "verboseness set to error",
			verboseness:     3,
			expectV0Enabled: false,
			expectV1Enabled: false,
		},
		{
			name:            "verboseness set to debug",
			verboseness:     -1,
			expectV0Enabled: true,
			expectV1Enabled: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.verboseness)

			v0Enabled := logger.V(0).Enabled()
			if v0Enabled != tt.expectV0Enabled {
				t.Errorf("V(0) Info level: got enabled=%v, want enabled=%v", v0Enabled, tt.expectV0Enabled)
			}

			v1Enabled := logger.V(1).Enabled()
			if v1Enabled != tt.expectV1Enabled {
				t.Errorf("V(1) Debug level: got enabled=%v, want enabled=%v", v1Enabled, tt.expectV1Enabled)
			}
		})
	}
}
