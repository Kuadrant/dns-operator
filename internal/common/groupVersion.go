package common

import (
	"errors"
	"regexp"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

var gvkRegexStr string = `(?P<group>[a-zA-Z][a-zA-Z0-9\-_\.]*)[\/_](?P<version>[a-zA-Z][a-zA-Z0-9\-_]*)\.(?P<kind>[a-zA-Z][a-zA-Z0-9\-_\.]*)`
var gvrRegexStr string = `(?P<group>[a-zA-Z][a-zA-Z0-9\-_\.]*)[\/_](?P<version>[a-zA-Z][a-zA-Z0-9\-_]*)\.(?P<resource>[a-zA-Z][a-zA-Z0-9\-_\.]*)`

func ParseGVKString(gvkStr string) (schema.GroupVersionKind, error) {
	gvkRegex, err := regexp.Compile(gvkRegexStr)
	if err != nil {
		return schema.GroupVersionKind{}, err
	}

	groupIndex := gvkRegex.SubexpIndex("group")
	versionIndex := gvkRegex.SubexpIndex("version")
	kindIndex := gvkRegex.SubexpIndex("kind")

	matches := gvkRegex.FindStringSubmatch(gvkStr)

	if len(matches) < 3 {
		return schema.GroupVersionKind{}, errors.New("provided GVK is an invalid format")
	}

	return schema.GroupVersionKind{Group: matches[groupIndex], Version: matches[versionIndex], Kind: matches[kindIndex]}, nil

}

func ParseGVRString(gvkStr string) (schema.GroupVersionResource, error) {
	gvkRegex, err := regexp.Compile(gvrRegexStr)
	if err != nil {
		return schema.GroupVersionResource{}, err
	}

	groupIndex := gvkRegex.SubexpIndex("group")
	versionIndex := gvkRegex.SubexpIndex("version")
	resourceIndex := gvkRegex.SubexpIndex("resource")

	matches := gvkRegex.FindStringSubmatch(gvkStr)

	if len(matches) < 3 {
		return schema.GroupVersionResource{}, errors.New("provided GVR is an invalid format")
	}

	return schema.GroupVersionResource{Group: matches[groupIndex], Version: matches[versionIndex], Resource: matches[resourceIndex]}, nil
}
