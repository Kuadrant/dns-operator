//go:build unit

package provider

import (
	"errors"
	"strings"
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
		name          string
		host          string
		zones         []DNSZone
		denyApex      bool
		wantZone      string
		wantSubdomain string
		wantErr       bool
		expectedErrIs error // Expected sentinel error for errors.Is() check
	}{
		// Success cases
		{
			name: "single zone with match",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{DNSName: "example.com"},
			},
			denyApex:      false,
			wantZone:      "example.com",
			wantSubdomain: "sub.domain.test",
			wantErr:       false,
		},
		{
			name: "multiple zones that all match - returns longest match",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{DNSName: "example.com"},
				{DNSName: "test.example.com"},
				{DNSName: "domain.test.example.com"},
			},
			denyApex:      false,
			wantZone:      "domain.test.example.com",
			wantSubdomain: "sub",
			wantErr:       false,
		},
		{
			name: "multiple zones some match",
			host: "sub.domain.test.example.com",
			zones: []DNSZone{
				{DNSName: "example.com"},
				{DNSName: "test.example.com"},
				{DNSName: "test.otherdomain.com"},
			},
			denyApex:      false,
			wantZone:      "test.example.com",
			wantSubdomain: "sub.domain",
			wantErr:       false,
		},
		{
			name: "handles tld with a dot (e.g., co.uk)",
			host: "sub.domain.test.example.co.uk",
			zones: []DNSZone{
				{DNSName: "example.co.uk"},
			},
			denyApex:      false,
			wantZone:      "example.co.uk",
			wantSubdomain: "sub.domain.test",
			wantErr:       false,
		},
		{
			name: "apex domain allowed when denyApex=false",
			host: "example.com",
			zones: []DNSZone{
				{DNSName: "example.com", ID: "zone-1"},
			},
			denyApex:      false,
			wantZone:      "example.com",
			wantSubdomain: "",
			wantErr:       false,
		},

		// Error cases - ErrNoZoneForHost
		{
			name:          "error when no zones provided",
			host:          "sub.domain.test.example.com",
			zones:         []DNSZone{},
			denyApex:      false,
			wantErr:       true,
			expectedErrIs: ErrNoZoneForHost,
		},
		{
			name: "error when no matching zones",
			host: "sub.domain.test.example2.com",
			zones: []DNSZone{
				{DNSName: "example.com"},
				{DNSName: "test.example.com"},
				{DNSName: "test.otherdomain.com"},
			},
			denyApex:      false,
			wantErr:       true,
			expectedErrIs: ErrNoZoneForHost,
		},
		{
			name: "error when host is a TLD",
			host: "com",
			zones: []DNSZone{
				{DNSName: "example.com"},
			},
			denyApex:      false,
			wantErr:       true,
			expectedErrIs: ErrNoZoneForHost,
		},
		{
			name: "error when host reaches TLD during recursion",
			host: "sub.domain.test.example.co.uk",
			zones: []DNSZone{
				{DNSName: "co.uk"},
			},
			denyApex:      false,
			wantErr:       true,
			expectedErrIs: ErrNoZoneForHost,
		},
		{
			name: "error when host has invalid format (single label)",
			host: "localhost",
			zones: []DNSZone{
				{DNSName: "example.com"},
			},
			denyApex:      false,
			wantErr:       true,
			expectedErrIs: ErrNoZoneForHost,
		},

		// Error cases - ErrApexDomainNotAllowed
		{
			name: "error when apex domain denied",
			host: "example.com",
			zones: []DNSZone{
				{DNSName: "example.com", ID: "zone-1"},
			},
			denyApex:      true,
			wantErr:       true,
			expectedErrIs: ErrApexDomainNotAllowed,
		},
		{
			name: "error when host exactly matches zone with denyApex=true",
			host: "test.example.com",
			zones: []DNSZone{
				{DNSName: "example.com"},
				{DNSName: "test.example.com", ID: "zone-2"},
			},
			denyApex:      true,
			wantErr:       true,
			expectedErrIs: ErrApexDomainNotAllowed,
		},

		// Error cases - ErrMultipleZonesFound
		{
			name: "error when multiple zones have same DNS name",
			host: "domain.test.example.com",
			zones: []DNSZone{
				{DNSName: "example.com", ID: "zone-1"},
				{DNSName: "test.example.com", ID: "zone-2"},
				{DNSName: "test.example.com", ID: "zone-3"},
			},
			denyApex:      false,
			wantErr:       true,
			expectedErrIs: ErrMultipleZonesFound,
		},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			gotZone, gotSubdomain, err := findDNSZoneForHost(tt.host, tt.host, tt.zones, tt.denyApex)

			// Check error expectation
			if (err != nil) != tt.wantErr {
				t.Errorf("findDNSZoneForHost() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			// Verify error cases
			if tt.wantErr {
				// Verify specific error type if specified
				if tt.expectedErrIs != nil {
					if !errors.Is(err, tt.expectedErrIs) {
						t.Errorf("findDNSZoneForHost() error = %v, expectedErrIs %v", err, tt.expectedErrIs)
					}
				}

				// Verify original host is present in error message
				if err != nil && !strings.Contains(err.Error(), tt.host) {
					t.Errorf("findDNSZoneForHost() error message %q does not contain original host %q", err.Error(), tt.host)
				}
			}

			// Check success case results
			if !tt.wantErr {
				if gotZone == nil {
					t.Errorf("findDNSZoneForHost() got nil zone, want %v", tt.wantZone)
					return
				}
				if gotZone.DNSName != tt.wantZone {
					t.Errorf("findDNSZoneForHost() gotZone = %v, want %v", gotZone.DNSName, tt.wantZone)
				}
				if gotSubdomain != tt.wantSubdomain {
					t.Errorf("findDNSZoneForHost() gotSubdomain = %v, want %v", gotSubdomain, tt.wantSubdomain)
				}
			}
		})
	}
}
