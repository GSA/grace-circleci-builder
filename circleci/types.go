package circleci

import (
	"fmt"
	"io"
	"net/url"
	"time"
)

// isolates the request func for hooking up tests
type requestFunc func(*Client, string, string, url.Values, interface{}, interface{}) error

// RequestError contains details about the failed HTTP request
type RequestError struct {
	Code    int
	Message string
}

func (r RequestError) Error() string {
	return r.Message
}

// summaryNotFoundError ... used internally to signify
// a build summary was not found when calling findBuildSummary
type summaryNotFoundError struct {
	Message string
}

func (e *summaryNotFoundError) Error() string {
	return e.Message
}

// timeoutExceededError ... used internally to signify
// a timeout occurred while calling waiter
type timeoutExceededError struct {
	Message string
}

func (e *timeoutExceededError) Error() string {
	return e.Message
}

// ProjectNotFoundError ... a project was not found when calling FindProject
type ProjectNotFoundError struct {
	Message string
}

func (p *ProjectNotFoundError) Error() string {
	return p.Message
}

// BuildProjectInput ... contains data necessary to send a new project build
// https://circleci.com/docs/api/v1-reference/#new-project-build
type BuildProjectInput struct {
	//The branch to build. Cannot be used with tag parameter.
	Branch string `json:"branch,omitempty"`
	//The specific revision to build. If not specified, the HEAD
	//of the branch is used. Cannot be used with tag parameter.
	Revision string `json:"revision,omitempty"`
	//The git tag to build. Cannot be used with branch and revision
	//parameters.
	Tag string `json:"tag,omitempty"`
}

// matchSummary ... returns true if the given *BuildSummaryOutput matches the
// *BuildProjectInput parameters
func (bpi *BuildProjectInput) matchSummary(summary *BuildSummaryOutput) bool {
	if len(bpi.Tag) > 0 && summary.VcsTag != bpi.Tag {
		return false
	}
	if len(bpi.Revision) > 0 && summary.Revision != bpi.Revision {
		return false
	}
	if len(bpi.Branch) > 0 && summary.Branch != bpi.Branch {
		return false
	}
	return true
}

// String ... returns the string formatted version of a BuildProjectInput
func (bpi *BuildProjectInput) String() string {
	return fmt.Sprintf("[Branch: %q, Revision: %q, Tag: %q]", bpi.Branch, bpi.Revision, bpi.Tag)
}

// buildProjectOutput ... represents the JSON structure returned from
// a call to new project build:
// https://circleci.com/docs/api/v1-reference/#new-project-build
type buildProjectOutput struct {
	//http status code returned
	Status int `json:"status,omitempty"`
	//message from circleci
	Body string `json:"body,omitempty"`
}

func (b buildProjectOutput) String() string {
	return fmt.Sprintf("Status: %d, Body: %q", b.Status, b.Body)
}

// BuildSummaryInput ... can be provided to BuildSummary to set
// parameter filters on the request
type BuildSummaryInput struct {
	//The number of builds to return. Maximum 100, defaults to 30.
	Limit int
	//The API returns builds starting from this offset, defaults to 0.
	Offset int
	//Restricts which builds are returned. Set to "completed", "successful", "failed", "running", or defaults to no filter.
	Filter string
}

// User ... represents a genericized form of the user object
// returned by calling /me or /buildNum on the CircleCI API v1.1
// /me: https://circleci.com/docs/api/v1-reference/#user
// /buildNum: https://circleci.com/docs/api/v1-reference/#build
type User struct {
	//login name
	Username string `json:"login"`
	//display name
	DisplayName string `json:"name"`
}

// BuildSummaryOutput ... represents a genericized form of the
// response object returned from a request for recent builds
// of a single project:
// https://circleci.com/docs/api/v1-reference/#recent-builds-project
type BuildSummaryOutput struct {
	BuildNum int    `json:"build_num"`
	Username string `json:"username"`
	// :queued, :scheduled, :not_run, :not_running, :running or :finished
	Lifecycle string `json:"lifecycle"`
	Reponame  string `json:"reponame"`
	// :canceled, :infrastructure_fail, :timedout, :failed, :no_tests or :success
	Outcome string `json:"outcome"`
	// :retried, :canceled, :infrastructure_fail, :timedout, :not_run, :running, :failed, :queued, :scheduled, :not_running, :no_tests, :fixed, :success
	Status    string     `json:"status"`
	Branch    string     `json:"branch"`
	Revision  string     `json:"vcs_revision"`
	User      *User      `json:"user"`
	QueuedAt  *time.Time `json:"usage_queued_at"`
	StoppedAt *time.Time `json:"stop_time"`
	Vcs       string     `json:"vcs_type"`
	VcsTag    string     `json:"vcs_tag"`
	//This may need to change later, CircleCI returns
	//what appears to be an array, as a single object
	Workflow *BuildWorkflow `json:"workflows"`
}

// Project ... a genericized form of the response from calling
// /projects on the CircleCI API v1.1
// https://circleci.com/docs/api/v1-reference/#projects
type Project struct {
	Username string `json:"username"`
	Reponame string `json:"reponame"`
	Vcs      string `json:"vcs_type"`
	VcsURL   string `json:"vcs_url"`
}

type followResponse struct {
	Following bool `json:"following"`
}

// BuildWorkflow ... partially represents the object returned
// in the workflows property of a build or build summary
type BuildWorkflow struct {
	JobName        string   `json:"job_name"`
	JobID          string   `json:"job_id"`
	WorkflowName   string   `json:"workflow_name"`
	WorkflowID     string   `json:"workflow_id"`
	WorkspaceID    string   `json:"workspace_id"`
	UpstreamJobIDs []string `json:"upstream_job_ids"`
}

// Build ... a genericized form of the object returned by
// calling /$buildNum on the CircleCI API v1.1
// https://circleci.com/docs/api/v1-reference/#build
type Build struct {
	BuildNum int    `json:"build_num"`
	Username string `json:"username"`
	// :queued, :scheduled, :not_run, :not_running, :running or :finished
	Lifecycle string `json:"lifecycle"`
	Reponame  string `json:"reponame"`
	// :canceled, :infrastructure_fail, :timedout, :failed, :no_tests or :success
	Outcome string `json:"outcome"`
	// :retried, :canceled, :infrastructure_fail, :timedout, :not_run, :running, :failed, :queued, :scheduled, :not_running, :no_tests, :fixed, :success
	Status    string     `json:"status"`
	Branch    string     `json:"branch"`
	Revision  string     `json:"vcs_revision"`
	Vcs       string     `json:"vcs_type"`
	Failed    *bool      `json:"failed"`
	User      *User      `json:"user"`
	QueuedAt  *time.Time `json:"usage_queued_at"`
	StoppedAt *time.Time `json:"stop_time"`
	//This may need to change later, CircleCI returns
	//what appears to be an array, as a single object
	Workflow *BuildWorkflow `json:"workflows"`
}

// API provides an interface to enable mocking the CircleCI REST client
type API interface {
	BuildProject(*Project, io.Writer, *BuildProjectInput, time.Duration) (*BuildSummaryOutput, error)
	WaitForProjectBuild(*Project, io.Writer, *BuildProjectInput, *BuildSummaryOutput, time.Duration, time.Duration, bool) error
	BuildSummary(*Project, io.Writer, *BuildSummaryInput) ([]*BuildSummaryOutput, error)
	FindBuildSummaries(*Project, io.Writer, *BuildProjectInput) ([]*BuildSummaryOutput, error)
	Projects(io.Writer) ([]*Project, error)
	FollowProject(*Project, io.Writer) error
	UnfollowProject(*Project, io.Writer) error
	FindProject(io.Writer, func(*Project) bool) (*Project, error)
	Me(io.Writer) (*User, error)
	GetBuild(*Project, io.Writer, int) (*Build, error)
}

var _ API = (*Client)(nil)
