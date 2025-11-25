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
				if got := RandomizeValidationDuration(tt.variance, tt.duration); !isValidVariance(tt.duration, got, tt.variance) {
					t.Errorf("RandomizeValidationDuration() invalid randomization; got = %v", got.String())
				}
				i++
			}
		})
	}
}

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

func isValidVariance(duration, randomizedDuration time.Duration, variance float64) bool {
	upperLimit := float64(duration.Milliseconds()) + float64(duration.Milliseconds())*variance
	lowerLimmit := float64(duration.Milliseconds()) - float64(duration.Milliseconds())*variance

	return float64(randomizedDuration.Milliseconds()) >= lowerLimmit &&
		float64(randomizedDuration.Milliseconds()) < upperLimit
}
