package circleci

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"testing"
	"time"

	"gotest.tools/assert"
)

func boolPtr(b bool) *bool {
	return &b
}

//nolint:gochecknoglobals
var apiStub = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	var resp string
	fmt.Printf("RequstURI: %q\n", r.RequestURI)
	switch r.RequestURI {
	case "/project/github/GSA/grace-build/follow?circle-token=":
		resp = `{"following": true}`
	default:
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	_, err := w.Write([]byte(resp))
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
	}
}))

func TestNew(t *testing.T) {
	t.Run("should allow nil client", func(t *testing.T) {
		c := NewClient(nil, "")
		if c.client == nil {
			t.Fatal("should allow nil client")
		}
	})
}

func TestFollow(t *testing.T) {
	var c *Client
	token := os.Getenv("CIRCLECI_TOKEN")
	if len(token) == 0 {
		u, err := url.Parse(apiStub.URL)
		if err != nil {
			t.Fatal(err)
		}
		c = &Client{
			client:    &http.Client{},
			baseURL:   u,
			requester: request,
		}
	} else {
		c = NewClient(nil, token)
	}
	p, err := ProjectFromURL("https://github.com/GSA/grace-build")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Project: %v\n", p)
	err = c.FollowProject(p, os.Stdout)
	if err != nil {
		t.Fatal(err)
	}
}

type testCase struct {
	title     string
	tester    func(*testing.T)
	requester requestFunc
}

type testCaseCreator func() testCase

//nolint:funlen
func TestBuildProject(t *testing.T) {
	c := &Client{}
	t.Parallel()
	tf := []testCaseCreator{
		func() testCase {
			var tc testCase
			tc.title = "BuildProject should fail if BuildProjectOutput.Status is not 200"
			tc.requester = testBuildProjectRequest(t, 404, "", "", "", "", "")
			tc.tester = func(t *testing.T) {
				c.requester = tc.requester
				_, err := c.BuildProject(&Project{
					Vcs:      "",
					Username: "",
					Reponame: "",
				}, os.Stdout, &BuildProjectInput{
					Tag:      "",
					Revision: "",
					Branch:   "",
				}, time.Second)
				if err == nil {
					t.Fatal("expected BuildProject to fail, got no error")
				}
			}
			return tc
		},
		func() testCase {
			var tc testCase
			tc.title = "BuildProject should fail if BuildProjectOutput.User.Username does not match Me.User.Username"
			tc.requester = testBuildProjectRequest(t, 200, "self", "notSelf", "", "", "")
			tc.tester = func(t *testing.T) {
				c.requester = tc.requester
				_, err := c.BuildProject(&Project{
					Vcs:      "",
					Username: "",
					Reponame: "",
				}, os.Stdout, &BuildProjectInput{
					Tag:      "",
					Revision: "",
					Branch:   "",
				}, time.Second)
				if err == nil {
					t.Fatal("expected BuildProject to fail, got no error")
				}
			}
			return tc
		},
		func() testCase {
			var tc testCase
			tc.title = "BuildProject should fail if BuildProjectOutput.Revision does not match ProjectInput.Revision"
			tc.requester = testBuildProjectRequest(t, 200, "self", "self", "", "1", "2")
			tc.tester = func(t *testing.T) {
				c.requester = tc.requester
				_, err := c.BuildProject(&Project{
					Vcs:      "",
					Username: "",
					Reponame: "",
				}, os.Stdout, &BuildProjectInput{
					Tag:      "",
					Revision: "2",
					Branch:   "1",
				}, time.Second)
				if err == nil {
					t.Fatal("expected BuildProject to fail, got no error")
				}
			}
			return tc
		},
		func() testCase {
			var tc testCase
			tc.title = "BuildProject should fail if BuildProjectOutput.Branch does not match ProjectInput.Branch"
			tc.requester = testBuildProjectRequest(t, 200, "self", "self", "", "1", "2")
			tc.tester = func(t *testing.T) {
				c.requester = tc.requester
				_, err := c.BuildProject(&Project{
					Vcs:      "",
					Username: "",
					Reponame: "",
				}, os.Stdout, &BuildProjectInput{
					Tag:      "",
					Revision: "1",
					Branch:   "1",
				}, time.Second)
				if err == nil {
					t.Fatal("expected BuildProject to fail, got no error")
				}
			}
			return tc
		},
	}
	tt := make([]testCase, len(tf))
	for i, tc := range tf {
		tt[i] = tc()
	}
	for _, tc := range tt {
		t.Run(tc.title, tc.tester)
	}
}

//nolint:unparam
func testBuildProjectRequest(t *testing.T, bpoStatus int, meUser string, bsoUser string, bsoTag string, bsoRevision string, bsoBranch string) requestFunc {
	return func(c *Client, method string, path string, params url.Values, input interface{}, output interface{}) error {
		now := time.Now()
		switch val := output.(type) {
		case *buildProjectOutput:
			val.Status = bpoStatus
		case *User:
			val.Username = meUser
		case *[]*BuildSummaryOutput:
			*val = append(*val, &BuildSummaryOutput{
				User:     &User{Username: bsoUser},
				QueuedAt: &now,
				VcsTag:   bsoTag,
				Revision: bsoRevision,
				Branch:   bsoBranch,
			})
		default:
			t.Fatalf("type not supported: %T", val)
		}
		return nil
	}
}

func TestFollowProject(t *testing.T) {
	project := Project{
		Username: "org",
		Reponame: "test1",
		Vcs:      "gh",
		VcsURL:   "https://github.com/org/test1",
	}
	tests := []struct {
		Name      string
		Following bool
		Err       error
		Expected  string
	}{{
		Name:      "successfully follow project",
		Following: true,
		Err:       nil,
		Expected:  "",
	}, {
		Name:      "unsuccessful follow project",
		Following: false,
		Err:       nil,
		Expected:  fmt.Sprintf("attempted to follow %s, following property still false", project.VcsURL),
	}, {
		Name:      "error from requester function",
		Following: true,
		Err:       fmt.Errorf("test error"),
		Expected:  "test error",
	}}
	for _, tt := range tests {
		tc := tt
		t.Run(tc.Name, func(t *testing.T) {
			client := &Client{
				client: &http.Client{},
				requester: func(c *Client, method string, path string, params url.Values, input interface{}, output interface{}) error {
					output.(*followResponse).Following = tc.Following
					return tc.Err
				}}

			err := client.FollowProject(&project, os.Stdout)
			if tc.Expected == "" {
				assert.NilError(t, err)
			} else {
				assert.Error(t, err, tc.Expected)
			}
		})
	}
}

// nolint: funlen
func TestWaitForProjectBuild(t *testing.T) {
	project := Project{
		Username: "org",
		Reponame: "test1",
		Vcs:      "gh",
		VcsURL:   "https://github.com/org/test1",
	}
	tt := map[string]struct {
		jobTimeout  time.Duration
		waitTimeout time.Duration
		build       Build
		user        User
		summary     string
		Err         error
		Expected    string
	}{"job timeout exceeded": {
		jobTimeout:  time.Duration(1) * time.Second,
		waitTimeout: time.Minute,
		build: Build{
			Lifecycle: "not finished",
		},
		Err:      nil,
		Expected: "job timeout exceeded while waiting for build test1 [0] to finish",
	}, "job failed": {
		jobTimeout:  time.Duration(1) * time.Minute,
		waitTimeout: time.Minute,
		build: Build{
			Lifecycle: "finished",
			Failed:    boolPtr(true),
		},
		Err:      nil,
		Expected: "build test1 [0] failed",
	}, "could not obtain workflow details": {
		jobTimeout:  time.Duration(1) * time.Minute,
		waitTimeout: time.Minute,
		build: Build{
			Lifecycle: "finished",
			Failed:    boolPtr(false),
		},
		Err:      nil,
		Expected: "could not obtain workflow details from build 0",
	}, "job succeeded": {
		jobTimeout:  time.Duration(1) * time.Minute,
		waitTimeout: time.Minute,
		build: Build{
			BuildNum:  42,
			Lifecycle: "not finished",
			Workflow:  &BuildWorkflow{WorkflowID: "test"},
		},
		summary: `[{
			"build_num": 42,
			"username": "org",
			"lifecycle": "not finished",
			"reponame": "test1",
			"workflows": {"workflow_id": "test"},
			"user": {"login": "org"}
		}]`,
		Err:      nil,
		Expected: "",
	}}
	for name, tc := range tt {
		tc := tc
		t.Run(name, func(t *testing.T) {
			client := &Client{
				client: &http.Client{},
				requester: func(c *Client, method string, path string, params url.Values, input interface{}, output interface{}) error {
					switch v := output.(type) {
					case *Build:
						output.(*Build).Lifecycle = tc.build.Lifecycle
						output.(*Build).Failed = tc.build.Failed
						output.(*Build).Workflow = tc.build.Workflow
						output.(*Build).BuildNum = tc.build.BuildNum
						//Change values for second query
						tc.build.Lifecycle = "finished"
						tc.build.Failed = boolPtr(false)
					case *User:
						output.(*User).Username = project.Username
					case *[]*BuildSummaryOutput:
						err := json.Unmarshal([]byte(tc.summary), output)
						if err != nil {
							return fmt.Errorf("failed to decode response: %v", err)
						}
						//Change values for second query
						tc.summary = `[{
							"build_num": 42,
							"username": "org",
							"lifecycle": "finished",
							"reponame": "test1",
							"outcome": "success",
							"workflows": {"workflow_id": "test"},
							"user": {"login": "org"}
						}]`
						tc.build.Lifecycle = "finished"
					default:
						return fmt.Errorf("unknown output type: %T", v)
					}
					return tc.Err
				}}
			in := &BuildProjectInput{}
			sum := &BuildSummaryOutput{}
			err := client.WaitForProjectBuild(&project, os.Stdout, in, sum, tc.jobTimeout, tc.waitTimeout, false)
			if tc.Expected == "" {
				assert.NilError(t, err)
			} else {
				assert.Error(t, err, tc.Expected)
			}
		})
	}
}
