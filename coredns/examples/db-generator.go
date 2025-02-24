package main

/*
import (
	"log"
	"net"
	"os"

	"github.com/maxmind/mmdbwriter"
	"github.com/maxmind/mmdbwriter/inserter"
	"github.com/maxmind/mmdbwriter/mmdbtype"
)

const (
	cdirIE = "127.0.100.100/24"
	cidrUS = "127.0.200.200/24"
)

// Create new mmdb database fixtures in this directory.
func main() {
	createCityDB("GeoLite2-City-demo.mmdb", "DBIP-City-Lite")
}

func createCityDB(dbName, dbType string) {
	// Load a database writer.
	writer, err := mmdbwriter.New(mmdbwriter.Options{
		DatabaseType:            dbType,
		IncludeReservedNetworks: true,
	})
	if err != nil {
		log.Fatal(err)
	}

	// Define and insert the new data.
	_, ipIE, err := net.ParseCIDR(cdirIE)
	if err != nil {
		log.Fatal(err)
	}

	// Define and insert the new data.
	_, ipUS, err := net.ParseCIDR(cidrUS)
	if err != nil {
		log.Fatal(err)
	}

	recordIE := mmdbtype.Map{
		"continent": mmdbtype.Map{
			"code":       mmdbtype.String("EU"),
			"geoname_id": mmdbtype.Uint64(1),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("Europe"),
			},
		},
		"country": mmdbtype.Map{
			"iso_code":   mmdbtype.String("IE"),
			"geoname_id": mmdbtype.Uint64(11),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("Ireland"),
			},
		},
	}

	recordUS := mmdbtype.Map{
		"continent": mmdbtype.Map{
			"code":       mmdbtype.String("NA"),
			"geoname_id": mmdbtype.Uint64(2),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("North America"),
			},
		},
		"country": mmdbtype.Map{
			"iso_code":   mmdbtype.String("US"),
			"geoname_id": mmdbtype.Uint64(22),
			"names": mmdbtype.Map{
				"en": mmdbtype.String("United States"),
			},
		},
	}

	if err := writer.InsertFunc(ipIE, inserter.TopLevelMergeWith(recordIE)); err != nil {
		log.Fatal(err)
	}
	if err := writer.InsertFunc(ipUS, inserter.TopLevelMergeWith(recordUS)); err != nil {
		log.Fatal(err)
	}

	// Write the DB to the filesystem.
	fh, err := os.Create(dbName)
	if err != nil {
		log.Fatal(err)
	}
	_, err = writer.WriteTo(fh)
	if err != nil {
		log.Fatal(err)
	}
}*/
