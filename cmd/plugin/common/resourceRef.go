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
	if providerRef == "" {
		return nil, errors.New("empty providerRef")
	}
	parts := strings.Split(providerRef, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return nil, errors.New("providerRef most be in the format of '<namespace>/<name>'")
	}

	return &ResourceRef{Namespace: parts[0], Name: parts[1]}, nil
}
