package main

import (
	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

func getZoneRecordsCommand() *cobra.Command {
	return &cobra.Command{
		Use:  "zone-records",
		RunE: getZoneRecords,
	}
}

func getZoneRecords(_ *cobra.Command, _ []string) error {
	log = logf.Log.WithName("get-zone-records")

	log.Info("Getting zone records: TODO")
	return nil
}
