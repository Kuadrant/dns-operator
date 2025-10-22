package owners

import (
	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

var DeleteOwnerCMD = &cobra.Command{
	Use:  "delete-owner",
	RunE: deleteOwner,
}

func deleteOwner(_ *cobra.Command, _ []string) error {
	log := logf.Log.WithName("delete-owner")

	log.Info("Deleting owner: TODO")
	return nil
}
