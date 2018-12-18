package main

import (
	"context"
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

func (e *entry) Build(client *circleci.Client, logger io.Writer, project *circleci.Project, input *circleci.BuildProjectInput) error {
	summary, err := client.BuildProject(project, logger, &circleci.BuildProjectInput{
		Branch:   e.Branch,
		Revision: e.Commit,
		Tag:      e.Tag,
	}, time.Minute)
	if err != nil {
		return err
	}
	return client.WaitForProjectBuild(project, logger, input, summary, 20*time.Minute, time.Minute)
}

func main() {
	token := os.Getenv("CIRCLECI_TOKEN")
	if len(token) == 0 {
		log.Fatal("CIRCLECI_TOKEN environment variable must contain the access key to authenticate to circleci.com")
	}
	buildFilePtr := flag.String("file", "Buildfile", "provide the location of the Buildfile to process")
	flag.Parse()
	if len(*buildFilePtr) == 0 {
		flag.Usage()
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
		log.Printf("Building project %q\n", project.Reponame)
		err = entry.Build(client, os.Stdout, project, &circleci.BuildProjectInput{
			Branch:   entry.Branch,
			Revision: entry.Commit,
			Tag:      entry.Tag,
		})
		if err != nil {
			log.Fatalf("failed to build project: %s -> %v", project.Reponame, err)
		}
		log.Printf("Building project %q completed successfully\n", project.Reponame)
	}
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
