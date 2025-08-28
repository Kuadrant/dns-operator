//go:build e2e

package helpers

import (
	"os"

	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/serializer"
	"k8s.io/client-go/kubernetes/scheme"
)

var testEnvVars map[string]string

func ResourceFromFile(file string, destObject runtime.Object, expandFunc func(string) string) error {
	decode := serializer.NewCodecFactory(scheme.Scheme).UniversalDeserializer().Decode
	stream, err := os.ReadFile(file)
	if err != nil {
		return err
	}
	stream = []byte(os.Expand(string(stream), expandFunc))
	_, _, err = decode(stream, nil, destObject)
	return err
}

func GetTestEnv(key string) string {
	if testEnvVars != nil {
		if v, ok := testEnvVars[key]; ok {
			return v
		}
	}
	return os.Getenv(key)
}

func SetTestEnv(key, value string) {
	if testEnvVars == nil {
		testEnvVars = map[string]string{}
	}
	testEnvVars[key] = value
}
