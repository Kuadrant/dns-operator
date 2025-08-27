package hash

import (
	"crypto/sha256"
	"encoding/json"
	"slices"
	"strings"

	"github.com/martinlindhe/base36"
)

func ToBase36Hash(s string) string {
	hash := sha256.Sum224([]byte(s))
	// convert the hash to base36 (alphanumeric) to decrease collision probabilities
	return strings.ToLower(base36.EncodeBytes(hash[:]))
}

func ToBase36HashLen(s string, l int) string {
	return ToBase36Hash(s)[:l]
}

// GetCanonicalString creates a stable, sorted string representation of any variable.
// The generated string may not be human readable
func GetCanonicalString(v any) (string, error) {
	vString, err := json.Marshal(v)
	if err != nil {
		return "", err
	}

	slices.Sort(vString)
	return string(vString), err
}
