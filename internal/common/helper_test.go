//go:build unit

package common

import (
	"testing"
	"time"

	. "github.com/onsi/gomega"
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

func Test_FormatRootHost(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name     string
		RootHost string
		Verify   func(formatted string)
	}{
		{
			Name:     "regular host is not altered",
			RootHost: "pb.com",
			Verify: func(formatted string) {
				Expect(formatted).To(Equal("pb.com"))
			},
		},
		{
			Name:     "long host is shortened",
			RootHost: "123456789-123456789-123456789-123456789-123456789-123456789-123456789-",
			Verify: func(formatted string) {
				Expect(formatted).To(Equal("123456789-123456789-123456789-123456789-123456789-123456789-123"))
			},
		},
		{
			Name:     "wildcards are replace with 'w'",
			RootHost: "*.pb.com",
			Verify: func(formatted string) {
				Expect(formatted).To(Equal("w.pb.com"))
			},
		},
		{
			Name:     "long host with wildcard, wildcard is replaced and host is shortened",
			RootHost: "*.123456789-123456789-123456789-123456789-123456789-123456789-123456789-",
			Verify: func(formatted string) {
				Expect(formatted).To(Equal("w.123456789-123456789-123456789-123456789-123456789-123456789-1"))
			},
		},
		{
			Name:     "long host with wildcard, wildcard is replaced and host is shortened and trailing periods removed",
			RootHost: "*.123456789.......................................................",
			Verify: func(formatted string) {
				Expect(formatted).To(Equal("w.123456789"))
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			scenario.Verify(FormatRootHost(scenario.RootHost))

		})
	}
}

func isValidVariance(duration, randomizedDuration time.Duration, variance float64) bool {
	upperLimit := float64(duration.Milliseconds()) + float64(duration.Milliseconds())*variance
	lowerLimmit := float64(duration.Milliseconds()) - float64(duration.Milliseconds())*variance

	return float64(randomizedDuration.Milliseconds()) >= lowerLimmit &&
		float64(randomizedDuration.Milliseconds()) < upperLimit
}
