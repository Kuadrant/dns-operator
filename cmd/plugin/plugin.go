package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	verbose bool
	gitSHA  string // value injected in compilation-time with go linker
	version string // value injected in compilation-time with go linker
	log     = logf.Log
)

func main() {
	root := &cobra.Command{
		Use:   "dns",
		Short: "DNS Operator command line utility",
		Long:  "DNS Operator command line utility",
		PersistentPreRun: func(cmd *cobra.Command, args []string) {
			logf.SetLogger(zap.New(zap.UseDevMode(verbose), zap.WriteTo(os.Stdout)))
			cmd.SetContext(context.Background())
		},
	}

	root.SetArgs(os.Args[1:])

	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", true, "verbose output")

	root.AddCommand(versionCommand())
	root.AddCommand(deleteOwnerCommand())
	root.AddCommand(getZoneRecordsCommand())
	root.AddCommand(secretGenerationCommand())

	if err := root.Execute(); err != nil {
		os.Exit(1)
	}
}

func versionCommand() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version number of cli",
		Long:  "Print the version number of cli",
		RunE: func(cmd *cobra.Command, args []string) error {
			fmt.Printf("dns cli version: %s (%s)\n", version, gitSHA)
			return nil
		},
	}
}
