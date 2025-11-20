package types

import (
	"fmt"
	"strings"

	"github.com/kuadrant/dns-operator/internal/common/slice"
)

const (
	GroupLabelKey   = "group"
	TargetsLabelKey = "targets"
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

// Validate ensure the group set conforms to the required format
func (g *Group) Validate() error {
	invalid := ";&, \"'"
	if strings.ContainsAny(g.String(), invalid) {
		return fmt.Errorf("Group value can not contain: \"%s\"", invalid)
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
	return slice.Contains(g, func(gElem Group) bool { return gElem == group })
}

func (g Groups) String() string {
	activeGroupsStr := []string{}
	for _, group := range g {
		activeGroupsStr = append(activeGroupsStr, string(group))
	}
	return strings.Join(activeGroupsStr, ",")
}
