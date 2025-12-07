package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/kuadrant/dns-operator/cmd/plugin/common"
	"github.com/kuadrant/dns-operator/cmd/plugin/failover"
)

var (
	verbose int
	gitSHA  string // value injected in compilation-time with go linker
	version string // value injected in compilation-time with go linker
	log     = logf.Log
)

var rootCMD = &cobra.Command{
	Use:   "kuadrant-dns",
	Short: "DNS Operator command line utility",
	Long:  "DNS Operator command line utility",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logf.SetLogger(common.NewLogger(verbose))
		cmd.SetContext(context.Background())
	},
}

var versionCMD = &cobra.Command{
	Use:   "version",
	Short: "Print the version number of cli",
	Long:  "Print the version number of cli",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Printf("dns cli version: %s (%s)\n", version, gitSHA)
		return nil
	},
}

func init() {
	rootCMD.SetArgs(os.Args[1:])
	rootCMD.PersistentFlags().IntVarP(&verbose, "verbose", "v", 0, "verbosity level: 0 (errors only), 1 (+ info), 2 (+ debug)")
	rootCMD.AddCommand(versionCMD, cleanupOldTXTCMD, getZoneRecordsCMD, addClusterSecretCMD, removeOwnerCMD)
	rootCMD.AddCommand(failover.AddActiveGroupCMD, failover.GetActiveGroupsCMD, failover.RemoveActiveGroupCMD)
}

func main() {
	if err := rootCMD.Execute(); err != nil {
		os.Exit(1)
	}
}
