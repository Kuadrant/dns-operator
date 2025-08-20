package main

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/spf13/cobra"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	host      = "host"
	dnsRecord = "DNSRecord"
)

var rootCmd = &cobra.Command{
	Use:     "zone-records",
	PreRunE: flagValidate,
	RunE:    getZoneRecords,
}
var (
	name                 string
	namespace            string
	resourceType         string
	providerRef          string
	allowedResourceTypes = []string{host, dnsRecord}
)

func flagValidate(_ *cobra.Command, _ []string) error {
	if !slices.Contains(allowedResourceTypes, resourceType) {
		return errors.New("Invalid type given")
	}

	if resourceType == dnsRecord && providerRef != "" {
		return fmt.Errorf("type value of %s and the use of --providerRef are mutually exclusive", dnsRecord)
	}

	parts := strings.Split(providerRef, "/")
	if providerRef != "" && len(parts) != 2 {
		return errors.New("providerRef most be in the format of '<namespace>/<name>'")
	}

	return nil
}

func getZoneRecordsCommand() *cobra.Command {
	return rootCmd
}

func init() {
	noShortHand := ""
	noDefault := ""

	rootCmd.Flags().StringVarP(&name, "name", noShortHand, noDefault, "name for resource")
	if err := rootCmd.MarkFlagRequired("name"); err != nil {
		panic(err)
	}

	rootCmd.Flags().StringVarP(&resourceType, "type", "t", noDefault, fmt.Sprintf("Type of resource being passed. (%s)", strings.Join(allowedResourceTypes, ", ")))
	if err := rootCmd.MarkFlagRequired("type"); err != nil {
		panic(err)
	}

	rootCmd.Flags().StringVarP(&providerRef, "providerRef", noShortHand, noDefault,
		fmt.Sprintf("A provider reference to the secert to use when querying. This can only be used with the type of %s. Format = '<namespace>/<name>'", host))

	rootCmd.Flags().StringVarP(&namespace, "namespace", "n", "dns-operator-system", "namespace where resources exist")
}

func getZoneRecords(_ *cobra.Command, _ []string) error {
	log = logf.Log.WithName("get-zone-records")

	log.Info("Getting zone records: TODO", "name", name, "namespace", namespace, "resourceType", resourceType, "providerRef", providerRef)
	return nil
}
