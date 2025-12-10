package main

import (
	"github.com/spf13/cobra"

	"github.com/kuadrant/dns-operator/cmd/plugin/output"
)

var removeOwnerCMD = &cobra.Command{
	Use:  "remove-owner",
	RunE: removeOwner,
}

func removeOwner(_ *cobra.Command, _ []string) error {
	output.Formatter.Info("Deleting owner: TODO")
	return nil
}
