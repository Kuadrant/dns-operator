package types

import (
	"testing"

	. "github.com/onsi/gomega"
)

func Test_ValidateGroup(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name   string
		Group  Group
		Verify func(error)
	}{
		{
			Name:  "Valid string",
			Group: "valid",
			Verify: func(err error) {
				Expect(err).To(BeNil())
			},
		},
		{
			Name:  "InValid string (contains: ;)",
			Group: "invalid;",
			Verify: func(err error) {
				Expect(err).NotTo(BeNil())
			},
		},
		{
			Name:  "InValid string (contains: &)",
			Group: "invalid&",
			Verify: func(err error) {
				Expect(err).NotTo(BeNil())
			},
		},
		{
			Name:  "InValid string (contains: [SPACE])",
			Group: "invalid ",
			Verify: func(err error) {
				Expect(err).NotTo(BeNil())
			},
		},
		{
			Name:  "InValid string (contains: \")",
			Group: "invalid\"",
			Verify: func(err error) {
				Expect(err).NotTo(BeNil())
			},
		},
		{
			Name:  "InValid string (contains: ')",
			Group: "invalid'",
			Verify: func(err error) {
				Expect(err).NotTo(BeNil())
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			scenario.Verify(scenario.Group.Validate())
		})
	}
}

func Test_GroupIsSet(t *testing.T) {
	RegisterTestingT(t)

	scenarios := []struct {
		Name   string
		Group  Group
		Verify func(bool)
	}{
		{
			Name:  "IsSet == true",
			Group: "valid",
			Verify: func(b bool) {
				Expect(b).To(BeTrue())
			},
		},
		{
			Name:  "IsSet != true",
			Group: "",
			Verify: func(b bool) {
				Expect(b).NotTo(BeTrue())
			},
		},
	}

	for _, scenario := range scenarios {
		t.Run(scenario.Name, func(t *testing.T) {
			scenario.Verify(scenario.Group.IsSet())
		})
	}
}
