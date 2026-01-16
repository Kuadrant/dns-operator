package types

import (
	"fmt"
	"slices"
	"strings"
)

const (
	GroupLabelKey      = "group"
	TargetsLabelKey    = "targets"
	MaxGroupNameLength = 16
)

// Group string type used for DNS Failover
type Group string

func (g *Group) String() string {
	return string(*g)
}

func (g *Group) Set(val string) error {
	temp := Group(val)
	err := temp.Validate()
	if err != nil {
		return err
	}
	*g = temp
	return nil
}

func (g *Group) Validate() error {
	name := g.String()

	if len(name) == 0 {
		return nil // Empty is valid (ungrouped)
	}

	if len(name) > MaxGroupNameLength {
		return fmt.Errorf("group name exceeds maximum length of %d characters", MaxGroupNameLength)
	}

	const invalid = ";&, \"'"
	if strings.ContainsAny(name, invalid) {
		return fmt.Errorf("group name cannot contain any of these characters: %s", invalid)
	}

	return nil
}

// IsSet checks if group is not empty
func (g *Group) IsSet() bool {
	if len(g.String()) > 0 {
		return true
	}
	return false
}

func (g *Group) Labels() map[string]string {
	return map[string]string{GroupLabelKey: g.String()}
}

type Groups []Group

func (g Groups) HasGroup(group Group) bool {
	return slices.Contains(g, group)
}

func (g Groups) String() string {
	groupsSlice := []string{}
	for _, group := range g {
		groupsSlice = append(groupsSlice, string(group))
	}
	return strings.Join(groupsSlice, ",")
}
