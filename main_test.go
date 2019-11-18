package main

import (
	"errors"
	"io"
	"os"
	"testing"
	"time"

	"github.com/GSA/grace-circleci-builder/circleci"
	"github.com/GSA/grace-circleci-builder/circleci/circleciiface"
)

type mockClient struct {
	circleciiface.CIRCLECIAPI
	Project circleci.Project
}

func (m mockClient) FollowProject(p *circleci.Project, w io.Writer) error {
	return nil
}

func (m mockClient) FindProject(w io.Writer, fn func(*circleci.Project) bool) (*circleci.Project, error) {
	return &m.Project, nil
}

// nolint: gomnd
func (m mockClient) FindBuildSummaries(p *circleci.Project, w io.Writer, in *circleci.BuildProjectInput) ([]*circleci.BuildSummaryOutput, error) {
	buildTime := time.Now().AddDate(0, 0, -2)
	resp := []*circleci.BuildSummaryOutput{{
		BuildNum:  42,
		Username:  m.Project.Username,
		Reponame:  m.Project.Reponame,
		StoppedAt: &buildTime,
		Status:    "success",
		Workflow:  &circleci.BuildWorkflow{WorkflowID: "test"},
	}}
	return resp, nil
}

// nolint: gomnd
func (m mockClient) BuildProject(p *circleci.Project, w io.Writer, in *circleci.BuildProjectInput, _ time.Duration) (*circleci.BuildSummaryOutput, error) {
	resp := &circleci.BuildSummaryOutput{
		BuildNum: 42,
		Username: m.Project.Username,
		Reponame: m.Project.Reponame,
	}
	return resp, nil
}

func (m mockClient) WaitForProjectBuild(
	p *circleci.Project,
	w io.Writer,
	in *circleci.BuildProjectInput,
	o *circleci.BuildSummaryOutput,
	_ time.Duration,
	_ time.Duration,
	_ bool) error {
	return nil
}

//nolint: gomnd
func TestParseEntries(t *testing.T) {
	tests := []struct {
		Name         string
		File         string
		Err          error
		Expect       []*entry
		ExpectLength int
	}{{
		Name: "file does not exist",
		File: "test_data/not_here.json",
		Err: &os.PathError{
			Op:   "stat",
			Path: "test_data/not_here.json",
			Err:  errors.New("no such file or directory"),
		},
		Expect:       []*entry{},
		ExpectLength: 0,
	},
		{
			Name:         "invalid json",
			File:         "test_data/invalid.json",
			Err:          errors.New("EOF"),
			Expect:       []*entry{},
			ExpectLength: 0,
		},
		{
			Name: "two entries",
			File: "test_data/test.json",
			Err:  nil,
			Expect: []*entry{{
				Name:           "test1",
				URL:            "https://github.com/org/test1",
				Branch:         "master",
				Tag:            "",
				Commit:         "test000001",
				ContinueOnFail: false,
			},
				{
					Name:           "test2",
					URL:            "https://github.com/org/test2",
					Branch:         "master",
					Tag:            "",
					Commit:         "test000002",
					ContinueOnFail: false}},
			ExpectLength: 2,
		}}
	for _, st := range tests {
		tc := st
		t.Run(tc.Name, func(t *testing.T) {
			got, err := parseEntries(tc.File)
			if err == nil && tc.Err != nil {
				t.Errorf("parseEntries() failed: expected error %v (%T)\nGot: %v (%T)\n", tc.Err, tc.Err, err, err)
			} else if err != nil && tc.Err == nil {
				t.Errorf("parseEntries() failed: did not expect error %v (%T)\n", err, err)
			}
			if tc.ExpectLength != len(got) {
				t.Errorf("parseEntries() failed: expected %v (%T)\nGot: %v (%T)\n", tc.Expect, tc.Expect, got, got)
			}
		})
	}
}

func TestRunBuilds(t *testing.T) {
	client := mockClient{
		Project: circleci.Project{
			Username: "tester",
			Reponame: "github.com/org/test1",
			Vcs:      "test",
			VcsURL:   "test",
		}}
	entries, err := parseEntries("test_data/test.json")
	if err != nil {
		t.Fatalf("RunBuilds() failed: %v", err)
	}
	err = runBuilds(client, 90, 1, false, entries)
	if err != nil {
		t.Fatalf("RunBuilds() failed: %v", err)
	}
}

// nolint: gomnd
func TestShouldSkip(t *testing.T) {
	project := circleci.Project{
		Username: "tester",
		Reponame: "github.com/org/test1",
		Vcs:      "test",
		VcsURL:   "test",
	}
	client := mockClient{Project: project}
	input := &circleci.BuildProjectInput{}
	tests := []struct {
		Name     string
		SkipDays int
		Expect   bool
	}{{
		Name:     "always skip if successful",
		SkipDays: -1,
		Expect:   true,
	}, {
		Name:     "do not skip days less than last success",
		SkipDays: 1,
		Expect:   false,
	}, {
		Name:     "skip days greater than last success",
		SkipDays: 3,
		Expect:   true,
	}}
	for _, st := range tests {
		tc := st
		t.Run(tc.Name, func(t *testing.T) {
			got, err := shouldSkip(&client, &project, input, tc.SkipDays)
			if err != nil {
				t.Errorf("shouldSkip() failed: %v\n", err)
			}
			if tc.Expect != got {
				t.Errorf("shouldSkip() failed: Expected: %v\nGot: %v", tc.Expect, got)
			}
		})
	}
}
