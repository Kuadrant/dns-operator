package common

import (
	"errors"
	"strings"
)

type ResourceRef struct {
	Name      string
	Namespace string
}

func ParseProviderRef(providerRef string) (*ResourceRef, error) {
	parts := strings.Split(providerRef, "/")
	if providerRef != "" && len(parts) != 2 {
		return nil, errors.New("providerRef most be in the format of '<namespace>/<name>'")
	}

	return &ResourceRef{Namespace: parts[0], Name: parts[1]}, nil
}
