package main

import (
	"encoding/json"
	"flag"
	"io"
	"log"
	"os"
	"path/filepath"
	"time"

	"github.com/GSA/grace-circleci-builder/circleci"
)

// entry ... contains necessary information to build a circleci project
type entry struct {
	//circleci project name
	Name string `json:"name"`
	//version control system url
	URL string `json:"repository"`
	//version control system branch to build
	Branch string `json:"branch"`
	//version control system tag to build (cannot be used with branch or commit)
	Tag string `json:"tag"`
	//version control system commit to build
	Commit string `json:"commit"`
}

func (e *entry) Build(client *circleci.Client, logger io.Writer, project *circleci.Project, input *circleci.BuildProjectInput, jobTimeout int) error {
	summary, err := client.BuildProject(project, logger, &circleci.BuildProjectInput{
		Branch:   e.Branch,
		Revision: e.Commit,
		Tag:      e.Tag,
	}, time.Minute)
	if err != nil {
		return err
	}
	return client.WaitForProjectBuild(project, logger, input, summary, time.Duration(jobTimeout)*time.Minute, time.Minute)
}

func main() {
	token := os.Getenv("CIRCLECI_TOKEN")
	if len(token) == 0 {
		log.Fatal("CIRCLECI_TOKEN environment variable must contain the access key to authenticate to circleci.com")
	}
	buildFilePtr := flag.String("file", "Buildfile", "provides the location of the JSON formatted build file to process")
	jobTimeout := flag.Int("jobtimeout", 20, "specifies the number of minutes that a build job can take before timing out")
	skipDaysPtr := flag.Int("skipdays", 30, "specifies the number of days to consider a previous build relevant for skipping")
	noSkipPtr := flag.Bool("noskip", false, "prevents skipping of previously built entries")
	flag.Parse()
	if len(*buildFilePtr) == 0 {
		flag.Usage()
	}
	if *jobTimeout < 0 {
		log.Fatal("jobtimeout must be greater than zero")
	}

	entries, err := parseEntries(*buildFilePtr)
	if err != nil {
		log.Fatal(err)
	}

	// loop over circleci project entries, resolving each project
	// and executing a full build, if anything fails, exit
	for _, entry := range entries {
		client := circleci.NewClient(nil, token)
		log.Printf("Searching for project with url: %s\n", entry.URL)
		project, err := client.FindProject(func(p *circleci.Project) bool {
			return p.VcsURL == entry.URL
		})
		if err != nil {
			log.Fatal(err)
		}
		input := &circleci.BuildProjectInput{
			Branch:   entry.Branch,
			Revision: entry.Commit,
			Tag:      entry.Tag,
		}
		if *noSkipPtr == false {
			log.Printf("Searching for builds in project %q, matching %s within %d to skip\n", project.Reponame, input, *skipDaysPtr)
			skip, err := shouldSkip(client, project, input, *skipDaysPtr)
			if err != nil {
				log.Fatalf("failed to query information about previous project builds for project %s -> %v", project.Reponame, err)
			}
			if skip {
				log.Printf("Skipping project %q, a previous build was found within %d days for %s\n", project.Reponame, *skipDaysPtr, input)
				continue
			}
			// remove when debugged
			continue
		}
		log.Printf("Building project %q\n", project.Reponame)
		err = entry.Build(client, os.Stdout, project, input, *jobTimeout)
		if err != nil {
			log.Fatalf("failed to build project: %s -> %v", project.Reponame, err)
		}
		log.Printf("Building project %q, completed successfully\n", project.Reponame)
	}
}

func shouldSkip(client *circleci.Client, project *circleci.Project, input *circleci.BuildProjectInput, skipDays int) (bool, error) {
	// this may need to be optimized to accept an 'after' date
	// so we can stop iterating over old/stale job data
	rawBuilds, err := client.FindBuildSummaries(project, input)
	if err != nil {
		return false, err
	}
	filteredBuilds := circleci.FilterBuildSummariesByWorkflowStatus(rawBuilds, "success")
	var lastSuccess *time.Time
	for _, b := range filteredBuilds {
		if b.StoppedAt == nil {
			continue
		}
		if lastSuccess == nil {
			lastSuccess = b.StoppedAt
			continue
		}
		if b.StoppedAt.After(*lastSuccess) {
			lastSuccess = b.StoppedAt
		}
	}
	if lastSuccess != nil {
		// if it has ever been successful, skip it
		if skipDays == -1 {
			return true, nil
		}
		skipCutoff := time.Now().Add(time.Duration((skipDays*24)*-1) * time.Hour)
		return skipCutoff.After(*lastSuccess), nil
	}
	return false, nil
}

func parseEntries(file string) (entries []*entry, err error) {
	var f *os.File
	if _, err = os.Stat(filepath.Clean(file)); os.IsNotExist(err) {
		return
	}
	f, err = os.Open(filepath.Clean(file))
	if err != nil {
		return
	}
	err = json.NewDecoder(f).Decode(&entries)
	return
}
