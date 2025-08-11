package common

import (
	"strings"
	"time"

	"k8s.io/apimachinery/pkg/util/rand"
)

// RandomizeValidationDuration randomizes duration for a given variance with a min value of 1 sec
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeValidationDuration(variance float64, duration time.Duration) time.Duration {
	// do not allow less than a second requeue
	if duration.Milliseconds() < 1000 {
		duration = time.Second * 1
	}
	// we won't go smaller than a second - using milliseconds to have a relatively big number to randomize
	return RandomizeDuration(variance, float64(duration.Milliseconds()))
}

// RandomizeDuration randomizes duration for a given variance.
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeDuration(variance, duration float64) time.Duration {
	upperLimit := duration * (1.0 + variance)
	lowerLimit := duration * (1.0 - variance)

	return time.Millisecond * time.Duration(rand.Int63nRange(
		int64(lowerLimit),
		int64(upperLimit)))
}

func FormatRootHost(rootHost string) string {
	formatted := strings.Replace(rootHost, "*", "w", 1)
	if len([]byte(formatted)) > 63 {
		formatted = string([]byte(formatted)[:63])
	}

	for []byte(formatted)[len([]byte(formatted))-1] == []byte(".")[0] {
		formatted = string([]byte(formatted)[:len([]byte(formatted))-2])
	}

	return formatted
}
