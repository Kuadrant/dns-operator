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
			expectedError: "An error has occurred,",
		},
		{
			name:          "error message with status code",
			err:           errors.New("An error has occurred, status code: 400"),
			expectedError: "An error has occurred,",
		},
		{
			name:          "error message with XML parse error",
			err:           errors.New("An error has occurred, InvalidInput: Invalid XML ; javax.xml.stream.XMLStreamException: org.xml.sax.SAXParseException; lineNumber: 1; columnNumber: 1044; cvc-length-valid: Value 'foo' with length = '3'"),
			expectedError: "An error has occurred, InvalidInput:  cvc-length-valid: Value 'foo' with length = '3'",
		},
		{
			name:          "error message with newlines and tabs",
			err:           errors.New("An error has occurred, \nerror\terror"),
			expectedError: "An error has occurred,  error error",
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
