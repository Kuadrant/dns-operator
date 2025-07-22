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

func Test_findDNSZoneForHost(t *testing.T) {
	testCases := []struct {
		name      string
		host      string
		zones     []DNSZone
		allowApex bool
		want      string
		want1     string
		wantErr   bool
	}{
		{
			name:      "no zones",
			host:      "sub.domain.test.example.com",
			zones:     []DNSZone{},
			allowApex: true,
			want:      "",
			want1:     "",
			wantErr:   true,
		},
		{
			name: "single zone with match",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{
					DNSName: "example.com",
				},
			},
			allowApex: true,
			want:      "example.com",
			want1:     "sub.domain.test",
			wantErr:   false,
		},
		{
			name: "does not match exact dns name",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{
					DNSName: "sub.domain.test.example.com",
				},
			},
			allowApex: true,
			want:      "",
			want1:     "",
			wantErr:   true,
		},
		{
			name: "multiple zones that all match",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{
					DNSName: "example.com",
				},
				{
					DNSName: "test.example.com",
				},
				{
					DNSName: "domain.test.example.com",
				},
			},
			allowApex: true,
			want:      "domain.test.example.com",
			want1:     "sub",
			wantErr:   false,
		},
		{
			name: "multiple zones some match",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{
					DNSName: "example.com",
				},
				{
					DNSName: "test.example.com",
				},
				{
					DNSName: "test.otherdomain.com",
				},
			},
			allowApex: true,
			want:      "test.example.com",
			want1:     "sub.domain",
			wantErr:   false,
		},
		{
			name: "multiple zones no match",
			host: "sub.domain.test.example2.com",
			zones: []DNSZone{
				{
					DNSName: "example.com",
				},
				{
					DNSName: "test.example.com",
				},
				{
					DNSName: "test.otherdomain.com",
				},
			},
			allowApex: true,
			want:      "",
			want1:     "",
			wantErr:   true,
		},
		{
			name: "handles tld with a dot",
			host: "sub.domain.test.example.co.uk",
			zones: []DNSZone{
				{
					DNSName: "example.co.uk",
				},
			},
			allowApex: true,
			want:      "example.co.uk",
			want1:     "sub.domain.test",
			wantErr:   false,
		},
		{
			name: "tld with a dot will not match against a zone of the tld",
			host: "sub.domain.test.example.co.uk",
			zones: []DNSZone{
				{
					DNSName: "co.uk",
				},
			},
			allowApex: true,
			want:      "",
			want1:     "",
			wantErr:   true,
		},
		{
			name: "multiple zones with multiple matches for the same dns name",
			host: "domain.test.example.com",
			zones: []DNSZone{
				{
					DNSName: "example.com",
				},
				{
					DNSName: "test.example.com",
				},
				{
					DNSName: "test.example.com",
				},
			},
			allowApex: true,
			want:      "",
			want1:     "",
			wantErr:   true,
		},
		{
			name: "apex domain",
			host: "test.example.com",
			zones: []DNSZone{
				{
					DNSName: "example.com",
				},
				{
					DNSName: "test.example.com",
				},
			},
			allowApex: true,
			want:      "",
			want1:     "",
			wantErr:   true,
		},
	}
	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			got, got1, err := findDNSZoneForHost(tt.host, tt.host, tt.zones, tt.allowApex)
			if (err != nil) != tt.wantErr {
				t.Errorf("findDNSZoneForHost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}
			if got == nil {
				if tt.want != "" {
					t.Errorf("findDNSZoneForHost() got = unexpetd nil value")
				}
			} else if got.DNSName != tt.want {
				t.Errorf("findDNSZoneForHost() got = %v, want %v", got, tt.want)
			}
			if got1 != tt.want1 {
				t.Errorf("findDNSZoneForHost() got1 = %v, want %v", got1, tt.want1)
			}
		})
	}
}
