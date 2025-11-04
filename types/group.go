package types

import (
	"fmt"
	"strings"
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
