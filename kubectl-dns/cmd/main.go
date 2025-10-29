package main

import (
	"context"
	"fmt"
	"os"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/kuadrant/dns-operator/kubectl-dns/internal/owners"
	"github.com/kuadrant/dns-operator/kubectl-dns/internal/records"
	"github.com/kuadrant/dns-operator/kubectl-dns/internal/secrets"
)

var (
	verbose bool
	gitSHA  string // value injected in compilation-time with go linker
	version string // value injected in compilation-time with go linker
)

var rootCMD = &cobra.Command{
	Use:   "dns",
	Short: "DNS Operator command line utility",
	Long:  "DNS Operator command line utility",
	PersistentPreRun: func(cmd *cobra.Command, args []string) {
		logf.SetLogger(zap.New(zap.UseDevMode(verbose), zap.WriteTo(os.Stdout)))
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
	rootCMD.PersistentFlags().BoolVarP(&verbose, "verbose", "v", true, "verbose output")

	rootCMD.AddCommand(versionCMD,
		secrets.GenerateSecretCMD,
		records.CleanupOldTXTCMD,
		records.GetZoneRecordsCMD,
		owners.DeleteOwnerCMD,
	)
}

func main() {
	if err := rootCMD.Execute(); err != nil {
		os.Exit(1)
	}
}
