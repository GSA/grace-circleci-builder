package main

import (
	"flag"
	"log"
	"os"

	"github.com/GSA/grace-circleci-builder/circleci"
)

func main() {
	token := os.Getenv("CIRCLECI_TOKEN")
	if len(token) == 0 {
		log.Fatal("CIRCLECI_TOKEN environment variable must contain the access key to authenticate to circleci.com")
	}
	buildFilePtr := flag.String("file", "Buildfile", "provides the location of the JSON formatted build file to process")
	jobTimeoutPtr := flag.Int("jobtimeout", 20, "specifies the number of minutes that a build job can take before timing out")
	skipDaysPtr := flag.Int("skipdays", 30, "specifies the number of days to consider a previous build relevant for skipping")
	noSkipPtr := flag.Bool("noskip", false, "prevents skipping of previously built entries")
	flag.Parse()

	if len(*buildFilePtr) == 0 {
		flag.Usage()
	}
	if *jobTimeoutPtr < 0 {
		log.Fatal("jobtimeout must be greater than zero")
	}

	entries, err := parseEntries(*buildFilePtr)
	if err != nil {
		log.Fatal(err)
	}

	client := circleci.NewClient(nil, token)
	err = runBuilds(client, *jobTimeoutPtr, *skipDaysPtr, *noSkipPtr, entries)
	if err != nil {
		log.Fatal(err)
	}
}
