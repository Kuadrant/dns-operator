//go:build unit

package aws

import (
	"errors"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go/aws/awserr"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func TestRemoveMetaData(t *testing.T) {
	tests := []struct {
		Name     string
		Error    error
		Validate func(*testing.T, error)
	}{
		{
			Name: "AWS error is trimmed",
			Error: awserr.NewRequestFailure(
				awserr.New(
					"InvalidInput",
					"Invalid XML ; javax.xml.stream.XMLStreamException: org.xml.sax.SAXParseException; lineNumber: 1; columnNumber: 140; cvc-datatype-valid.1.2.3: 'test.external.com' is not a valid value of union type 'IPAddress'",
					nil),
				400,
				"a887212e-60fe-4dab-8075-ea7b09f604dc"),
			Validate: func(t *testing.T, err error) {
				expected := "test.external.com is not a valid value of union type IPAddress"
				if err.Error() != expected {
					t.Fatalf("expected '%v', got '%v'", expected, err.Error())
				}
			},
		},
		{
			Name:  "non AWS error is unaffected",
			Error: errors.New("unrelated error"),
			Validate: func(t *testing.T, err error) {
				if err.Error() != "unrelated error" {
					t.Fatalf("expected '%v' got '%v'", "unrelated error", err.Error())
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			err := removeMetaData(tt.Error)
			tt.Validate(t, err)
		})
	}
}

func TestGetTransitionTime(t *testing.T) {
	tests := []struct {
		Name       string
		Conditions []metav1.Condition
		Type       string
		Status     metav1.ConditionStatus
		Validate   func(*testing.T, metav1.Time)
	}{
		{
			Name:       "Transition time is now with no existing condition",
			Conditions: []metav1.Condition{},
			Type:       "expectedType",
			Status:     metav1.ConditionStatus("expectedStatus"),
			Validate: func(t *testing.T, timeVal metav1.Time) {
				if timeVal.Unix() != metav1.Now().Unix() {
					t.Fatalf("expected '%v' got '%v'", metav1.Now(), timeVal)
				}
			},
		},
		{
			Name: "Transition time is preserved with existing condition",
			Conditions: []metav1.Condition{
				{
					Type:               "expectedType",
					Status:             "expectedStatus",
					LastTransitionTime: metav1.NewTime(time.Now().Add(time.Duration(-5) * time.Second)),
				},
			},
			Type:   "expectedType",
			Status: metav1.ConditionStatus("expectedStatus"),
			Validate: func(t *testing.T, timeVal metav1.Time) {
				if timeVal.Unix() != metav1.NewTime(time.Now().Add(time.Duration(-5)*time.Second)).Unix() {
					t.Fatalf("expected '%v' got '%v'", metav1.NewTime(time.Now().Add(time.Duration(-5)*time.Second)), timeVal)
				}
			},
		},
		{
			Name: "Transition time is updated with existing condition and changed status",
			Conditions: []metav1.Condition{
				{
					Type:               "expectedType",
					Status:             "unexpectedStatus",
					LastTransitionTime: metav1.NewTime(time.Now().Add(time.Duration(-5) * time.Second)),
				},
			},
			Type:   "expectedType",
			Status: metav1.ConditionStatus("expectedStatus"),
			Validate: func(t *testing.T, timeVal metav1.Time) {
				if timeVal.Unix() != metav1.Now().Unix() {
					t.Fatalf("expected '%v' got '%v'", metav1.NewTime(time.Now().Add(time.Duration(-5)*time.Second)), timeVal)
				}
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.Name, func(t *testing.T) {
			time := getTransitionTime(tt.Conditions, tt.Type, tt.Status)
			tt.Validate(t, time)
		})
	}
}
