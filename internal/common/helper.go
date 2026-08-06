package common

import (
	"time"

	"k8s.io/apimachinery/pkg/util/rand"

	"github.com/kuadrant/dns-operator/internal/common/hash"
)

// RandomizeDuration randomizes duration for a given variance.
// variance is expected to be of a format 0.1 for 10%, 0.5 for 50% and so on
func RandomizeDuration(variance, duration float64) time.Duration {
	upperLimit := duration * (1.0 + variance)
	lowerLimit := duration * (1.0 - variance)

	return time.Millisecond * time.Duration(rand.Int63nRange(
		int64(lowerLimit),
		int64(upperLimit)))
}

// HashRootHost generates a hash value of the given root host with a fixed length of 8
func HashRootHost(rootHost string) string {
	return hash.ToBase36HashLen(rootHost, 8)
}
