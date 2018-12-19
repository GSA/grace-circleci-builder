package circleci

import (
	"net/url"
	"os"
	"testing"
	"time"
)

func TestNew(t *testing.T) {
	t.Run("should allow nil client", func(t *testing.T) {
		c := NewClient(nil, "")
		if c.client == nil {
			t.Fatal("should allow nil client")
		}
	})
}

type testCase struct {
	title     string
	tester    func(*testing.T)
	requester requestFunc
}

type testCaseCreator func() testCase

func TestBuildProject(t *testing.T) {
	c := &Client{}
	t.Parallel()
	tf := []testCaseCreator{
		func() testCase {
			var tc testCase
			tc.title = "BuildProject should fail if BuildProjectOutput.Status is not 200"
			tc.requester = testBuildProjectRequest(t, 404, "", "", time.Now(), "", "", "")
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
			tc.requester = testBuildProjectRequest(t, 200, "self", "notSelf", time.Now(), "", "", "")
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
			tc.requester = testBuildProjectRequest(t, 200, "self", "self", time.Now(), "", "1", "2")
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
			tc.requester = testBuildProjectRequest(t, 200, "self", "self", time.Now(), "", "1", "2")
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
	var tt []testCase
	for _, tc := range tf {
		tt = append(tt, tc())
	}
	for _, tc := range tt {
		t.Run(tc.title, tc.tester)
	}
}

func testBuildProjectRequest(t *testing.T, bpoStatus int, meUser string, bsoUser string, bsoAfter time.Time, bsoTag string, bsoRevision string, bsoBranch string) requestFunc {
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