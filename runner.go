package main

import (
	"encoding/json"
	"fmt"
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
	//skip on failure
	ContinueOnFail bool `json:"continue_on_fail"`
}

func (e *entry) Build(client circleci.API, logger io.Writer, project *circleci.Project, input *circleci.BuildProjectInput, jobTimeout int) error {
	summary, err := client.BuildProject(project, logger, &circleci.BuildProjectInput{
		Branch:   e.Branch,
		Revision: e.Commit,
		Tag:      e.Tag,
	}, time.Minute)
	if err != nil {
		return err
	}
	return client.WaitForProjectBuild(project, logger, input, summary, time.Duration(jobTimeout)*time.Minute, time.Minute, e.ContinueOnFail)
}

//nolint: gocyclo
func runBuilds(client circleci.API, jobTimeout int, skipDays int, noSkip bool, entries []*entry) error {
	// loop over circleci project entries, resolving each project
	// and executing a full build, if anything fails, return
	for _, entry := range entries {
		if len(entry.URL) == 0 || len(entry.Name) == 0 {
			log.Printf("skipping blank entry...\n")
			continue
		}
		p, err := circleci.ProjectFromURL(entry.URL)
		if err != nil {
			return err
		}
		log.Printf("Following project with url: %s\n", entry.URL)
		err = client.FollowProject(p, os.Stdout)
		if err != nil {
			return fmt.Errorf("failed to follow project with URL: %s -> %v", entry.URL, err)
		}

		log.Printf("Searching for project with url: %s\n", entry.URL)
		entry := entry // pin!
		project, err := client.FindProject(os.Stdout, func(p *circleci.Project) bool {
			return p.VcsURL == entry.URL
		})
		if err != nil {
			return err
		}
		input := &circleci.BuildProjectInput{
			Branch:   entry.Branch,
			Revision: entry.Commit,
			Tag:      entry.Tag,
		}
		if !noSkip {
			var skip bool
			log.Printf("Searching for builds in project %q, matching %s within %d days to skip\n", project.Reponame, input, skipDays)
			skip, err = shouldSkip(client, project, input, skipDays)
			if err != nil {
				return fmt.Errorf("failed to query information about previous project builds for project %s -> %v", project.Reponame, err)
			}
			if skip {
				log.Printf("Skipping project %q, a previous build was found within %d days for %s\n", project.Reponame, skipDays, input)
				continue
			}
		}
		log.Printf("Building project %q\n", project.Reponame)
		err = entry.Build(client, os.Stdout, project, input, jobTimeout)
		if err != nil {
			return fmt.Errorf("failed to build project: %s -> %v", project.Reponame, err)
		}
		log.Printf("Building project %q, completed successfully\n", project.Reponame)
	}
	return nil
}

func shouldSkip(client circleci.API, project *circleci.Project, input *circleci.BuildProjectInput, skipDays int) (bool, error) {
	// this may need to be optimized to accept an 'after' date
	// so we can stop iterating over old/stale job data
	rawBuilds, err := client.FindBuildSummaries(project, os.Stdout, input)
	if err != nil {
		return false, err
	}
	filteredBuilds := circleci.FilterBuildSummariesByWorkflowStatus(rawBuilds, "success")
	var lastSuccess *time.Time
	// loop over all successful build summaries
	for _, b := range filteredBuilds {
		// if StoppedAt is not set, skip the summary
		if b.StoppedAt == nil {
			continue
		}
		// if this is the first summary that made it this far
		// set lastSuccess to the stop_time of this summary
		if lastSuccess == nil {
			lastSuccess = b.StoppedAt
			continue
		}
		// if this summary's stop_time is newer than the value
		// stored in lastSuccess, update lastSuccess to this value
		if b.StoppedAt.After(*lastSuccess) {
			lastSuccess = b.StoppedAt
		}
	}
	// if we found the stop_time of at least one successful job
	if lastSuccess != nil {
		// if it has ever been successful, skip it
		if skipDays == -1 {
			return true, nil
		}
		// set skipCutoff to the negative of skipDays in hours
		skipCutoff := time.Now().Add(time.Duration((skipDays*24)*-1) * time.Hour)
		// return true if lastSuccess is newer than skipCutoff
		return lastSuccess.After(skipCutoff), nil
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
