//go:build unit

package common

import (
	"testing"
	"time"
)

func TestRandomizeDuration(t *testing.T) {
	testIterations := 100

	tests := []struct {
		name     string
		variance float64
		duration time.Duration
	}{
		{
			name:     "returns valid duration in range",
			variance: 0.5,
			duration: time.Second * 5,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			i := 0
			for i < testIterations {
				if got := RandomizeDuration(tt.variance, tt.duration); !isValidVariance(tt.duration, got, tt.variance) {
					t.Errorf("RandomizeDuration() invalid randomization; got = %v", got.String())
				}
				i++
			}
		})
	}
}

func isValidVariance(duration, randomizedDuration time.Duration, variance float64) bool {
	upperLimit := float64(duration.Milliseconds()) + float64(duration.Milliseconds())*variance
	lowerLimmit := float64(duration.Milliseconds()) - float64(duration.Milliseconds())*variance

	return float64(randomizedDuration.Milliseconds()) >= lowerLimmit &&
		float64(randomizedDuration.Milliseconds()) < upperLimit
}
