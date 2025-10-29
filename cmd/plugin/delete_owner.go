package main

import (
	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var removeOwnerCMD = &cobra.Command{
	Use:  "remove-owner",
	RunE: removeOwner,
}

func removeOwner(_ *cobra.Command, _ []string) error {
	log = logf.Log.WithName("remove-owner")

	log.Info("Deleting owner: TODO")
	return nil
}
