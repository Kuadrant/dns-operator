package registry

import (
	"errors"
	"fmt"
	"strings"

	"sigs.k8s.io/external-dns/endpoint"
)

type TXTLabelsPacker struct{}

var _ LabelsPacker = TXTLabelsPacker{}

const (
	keyAndValueSeparator = "="
	labelsSeparator      = ","
)

func NewTXTLabelsPacker() *TXTLabelsPacker {
	return &TXTLabelsPacker{}
}

// UnpackLabels
// gets
// owner1: key1:value1,key2:value2
//
// returns
// owner1:
//
//	key1: value1
//	key2: value2
func (p TXTLabelsPacker) UnpackLabels(labels endpoint.Labels) map[string]endpoint.Labels {
	ownersMap := make(map[string]endpoint.Labels)

	if packed, _ := p.LabelsPacked(labels); !packed {
		return nil
	}

	for ownerID, labelsPerOwner := range labels {
		ownersMap[ownerID] = map[string]string{}
		labelsPerOwnerArray := strings.Split(labelsPerOwner, labelsSeparator)
		for _, labelPerOwner := range labelsPerOwnerArray {
			mapEntry := strings.Split(labelPerOwner, keyAndValueSeparator)
			if len(mapEntry) == 2 {
				ownersMap[ownerID][mapEntry[0]] = mapEntry[1]
			}
		}
	}

	return ownersMap
}

// PackLabels
// gets
// owner1:
//
//	key1: value1
//	key2: value2
//
// returns
// owner1: key1:value1,key2:value2
func (p TXTLabelsPacker) PackLabels(labelsPerOwner map[string]endpoint.Labels) endpoint.Labels {
	endpointMap := endpoint.Labels{}

	for owner, labels := range labelsPerOwner {
		labelsArray := make([]string, 0)
		for key, value := range labels {
			labelsArray = append(labelsArray, fmt.Sprintf("%s%s%s", key, keyAndValueSeparator, value))
		}
		endpointMap[owner] = strings.Join(labelsArray, labelsSeparator)
	}

	return endpointMap
}

// LabelsPacked investigates labels and returns
// true if labels of format:
// key1=value1
// key2=value2
//
// false if of format
// owner1: key1=value1, key2=value2
// owner2: key1=value1, key2=value2
// returns error otherwise
func (p TXTLabelsPacker) LabelsPacked(labels endpoint.Labels) (bool, error) {
	// checked - is true after first loop
	// packed - key/value consistently of a packed format
	var keyPacked, keyChecked, valuePacked, valueChecked bool

	// no labels to work on
	if len(labels) == 0 {
		return false, errors.New("no labels found")
	}
	for key, value := range labels {
		// the key should be owner ID or empty string
		// it could also happen that random key has length of ownerID

		if !keyChecked {
			keyPacked = keyOfPackedFormat(key)
			keyChecked = true
		}

		// all new keys must agree with the first key
		if keyPacked != keyOfPackedFormat(key) {
			// mismatch of keys!
			return false, errors.New("unknown format")
		}

		if !valueChecked {
			valuePacked = valueOfPackedFormat(value)
			valueChecked = true
		}

		if valuePacked != valueOfPackedFormat(value) {
			return false, errors.New("unknown format")
		}

		// key and value should be consistent
		if keyPacked != valuePacked {
			// exception
			// it could happen that unpacked key is of the owner ID length
			// in this case we will assume we are dealing with packed key
			// this is a problem since we can also happens to have broken label
			// explicitly checking value for key/value separator does not guarantee it will work
			if len(key) == ownerIDLen && !strings.Contains(value, keyAndValueSeparator) {
				continue
			}

			return false, errors.New("unknown format")
		}
	}
	// everything matches return value in case of key length exception
	return valuePacked, nil
}

func keyOfPackedFormat(key string) bool {
	return len(key) == ownerIDLen || key == ""
}

func valueOfPackedFormat(value string) bool {
	pairs := strings.Split(value, labelsSeparator)
	for _, pair := range pairs {
		// pair should contain =
		if !strings.Contains(pair, keyAndValueSeparator) {
			// if it is empty we are packed but have no labels
			if pair == "" {
				continue
			}
			return false
		}
		pairSplit := strings.Split(pair, keyAndValueSeparator)
		if len(pairSplit) != 2 {
			return false
		}
	}
	return true
}
