package common

import (
	"time"

	"k8s.io/apimachinery/pkg/util/rand"
)

// RandomizeDuration randomizes duration for a given variance.
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeDuration(variance float64, duration time.Duration) time.Duration {
	// we won't go smaller than a second - using milliseconds to have a relatively big number to randomize
	millisecond := float64(duration.Milliseconds())

	upperLimit := millisecond * (1.0 + variance)
	lowerLimit := millisecond * (1.0 - variance)

	return time.Millisecond * time.Duration(rand.Int63nRange(
		int64(lowerLimit),
		int64(upperLimit)))
}
