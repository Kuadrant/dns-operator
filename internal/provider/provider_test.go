//go:build unit

package provider

import (
	"errors"
	"testing"
)

func TestSanitizeError(t *testing.T) {
	testCases := []struct {
		name          string
		err           error
		expectedError string
	}{
		{
			name:          "error message with request id",
			err:           errors.New("An error has occurred, request id: 12345abcd"),
			expectedError: "An error has occurred, ",
		},
		{
			name:          "error message without request id",
			err:           errors.New("An error has occurred"),
			expectedError: "An error has occurred",
		},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			got := SanitizeError(testCase.err)
			if got.Error() != testCase.expectedError {
				t.Errorf("expected '%v' got '%v'", testCase.expectedError, got)
			}
		})
	}
}
