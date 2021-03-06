package circleci

import (
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

// Client ... contains necessary data to communicate with circleci
type Client struct {
	//initialized http client, if not provided, will be empty client
	client *http.Client
	//circleci access key used for all requests
	Token     string
	baseURL   *url.URL
	requester requestFunc
}

// NewClient ... returns a *circleci.Client
func NewClient(client *http.Client, token string) *Client {
	c := &Client{Token: token}
	if client == nil {
		c.client = &http.Client{}
	}
	c.requester = request
	// baseURL ... used internally to represent the base URL path for CircleCI API v1.1
	c.baseURL = &url.URL{Scheme: "https", Host: "circleci.com", Path: "/api/v1.1/"}
	return c
}

// summaryNotFoundError ... used internally to signify
// a build summary was not found when calling findBuildSummary
type summaryNotFoundError struct {
	Message string
}

func (e *summaryNotFoundError) Error() string {
	return e.Message
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

// BuildProject ... attempts to trigger a new project build,
// waits the next build job to start, then returns the *BuildSummaryObject
// for that build job
func (c *Client) BuildProject(project *Project, logger io.Writer, input *BuildProjectInput, waitTimeout time.Duration) (*BuildSummaryOutput, error) {
	var output buildProjectOutput
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		url := fmt.Sprintf("project/%s/%s/%s/build", project.Vcs, project.Username, project.Reponame)
		err := c.requester(c, "POST", url, nil, input, &output)
		if err != nil {
			logf(logger, "BuildProject failed, POST /%s -> %v", url, err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	if output.Status != http.StatusOK {
		return nil, fmt.Errorf("failed to start project build: %s", output)
	}
	//nolint:godox
	// 12/14/2018 - BLA
	// TODO: Fix this if CircleCI ever fixes their API
	// CircleCI currently doesn't return the buildNum or anything valuable when
	// starting a project build, so we must wait a while and try to find
	// a matching build that was created around this time
	// if we wait longer than 1 minute we'll give up
	after := time.Now().Add(-3 * time.Second)
	var summary *BuildSummaryOutput
	err = waiter(time.Second, time.Now().Add(waitTimeout), func(count int) (bool, error) {
		if count%10 == 0 {
			logf(logger, "waiting for a build summary matching the project: %s\n", project.Reponame)
		}
		summary, err = c.findBuildSummary(project, logger, input, after)
		if err != nil {
			if _, ok := err.(*summaryNotFoundError); ok {
				return false, nil
			}
			// should we care about this error if it happens within 1 minute
			// from starting the build? returning to the caller for now - BLA
			return false, err
		}
		return true, nil
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// findBuildSummary ... used internally to locate a BuildSummary that was executed
// by the current user and was queued after the provided 'after' time.Time
func (c *Client) findBuildSummary(project *Project, logger io.Writer, input *BuildProjectInput, after time.Time) (*BuildSummaryOutput, error) {
	summaries, err := c.BuildSummary(project, logger, nil)
	if err != nil {
		return nil, err
	}
	// don't store response but leave call to c.Me() for
	// restoration later after CircleCI restores the username
	// property inside the build summary response
	me, err := c.Me(logger)
	if err != nil {
		return nil, err
	}
	for _, summary := range summaries {
		if summary.QueuedAt == nil {
			continue
		}
		if input.matchSummary(summary) &&
			summary.User.Username == me.Username &&
			summary.QueuedAt.Sub(after) > 0 {
			return summary, nil
		}
	}
	return nil, &summaryNotFoundError{fmt.Sprintf("BuildSummary not found matching this project: %s", project)}
}

// WaitForProjectBuild ... waits for all build jobs within the given project
// to complete, if a build job fails, will return an error immediately
// waitTimeout is the time to wait for the next build, before giving up
// jobTimeout is the duration to wait for the build to complete, before giving up
func (c *Client) WaitForProjectBuild(
	project *Project,
	logger io.Writer,
	input *BuildProjectInput,
	summary *BuildSummaryOutput,
	jobTimeout time.Duration,
	waitTimeout time.Duration,
	continueOnFail bool) error {
	buildNum := summary.BuildNum
	for {
		build, err := c.waitForBuild(project, logger, buildNum, jobTimeout)
		if err != nil {
			return err
		}
		if *build.Failed {
			if continueOnFail {
				logf(logger, "build %s [%d] failed, continue on failure is enabled for this project\n", project.Reponame, buildNum)
				return nil
			}
			return fmt.Errorf("build %s [%d] failed", project.Reponame, buildNum)
		}
		if build.Workflow == nil {
			return fmt.Errorf("could not obtain workflow details from build %d", buildNum)
		}
		s, err := c.waitForNextBuild(project, logger, input, build.Workflow.WorkflowID, waitTimeout)
		if err != nil {
			if _, ok := err.(*timeoutExceededError); ok {
				// Assuming all builds are completed and the last
				// waiter call returned no results, which is expected
				// after the last build completes
				return finalWorkflowStatus(c, project, logger, input, build.Workflow.WorkflowID)
			}
			return err
		}
		buildNum = s.BuildNum
	}
}

// waitForNextBuild ... used internally to wait for the next build job within a given project
// and matches the provided workflowID, waitTimeout is the duration to wait before giving up
// nolint: gocyclo
func (c *Client) waitForNextBuild(project *Project, logger io.Writer, input *BuildProjectInput, workflowID string, waitTimeout time.Duration) (*BuildSummaryOutput, error) {
	var summary *BuildSummaryOutput
	me, err := c.Me(logger)
	if err != nil {
		return nil, err
	}
	err = waiter(time.Second, time.Now().Add(waitTimeout), func(count int) (bool, error) {
		if count%10 == 0 {
			logf(logger, "waiting for the next build summary matching the project: %s and workflowId: %s\n", project.Reponame, workflowID)
		}
		var summaries []*BuildSummaryOutput
		summaries, err = c.BuildSummary(project, logger, nil)
		if err != nil {
			// should this be returned to the caller, logging for now - BLA
			log.Printf("failed to enumerate build summaries: %v\n", err)
		}

		for _, s := range summaries {
			if input.matchSummary(s) &&
				s.User.Username == me.Username &&
				s.Lifecycle != lifecycleFinished &&
				s.Workflow != nil &&
				s.Workflow.WorkflowID == workflowID {
				summary = s
				return true, nil
			}
		}
		return false, nil
	})
	if err != nil {
		return nil, err
	}
	return summary, nil
}

// waitForBuild ... used internally to wait for the build matching the given
// buildNum to complete, does not validate that the build was successful
// jobTimeout is the duration to wait before giving up
func (c *Client) waitForBuild(project *Project, logger io.Writer, buildNum int, jobTimeout time.Duration) (*Build, error) {
	const sleepSec = 2
	var (
		count   int
		endTime = time.Now().Add(jobTimeout)
	)
	for {
		if time.Now().After(endTime) {
			return nil, fmt.Errorf("job timeout exceeded while waiting for build %s [%d] to finish", project.Reponame, buildNum)
		}
		if count%10 == 0 {
			logf(logger, "waiting for build %s [%d] to finish\n", project.Reponame, buildNum)
		}
		time.Sleep(sleepSec * time.Second)
		build, err := c.GetBuild(project, logger, buildNum)
		if err != nil {
			//should we return this error? logging for now - BLA
			logf(logger, "failed to get build %s [%d] -> %v\n", project.Reponame, buildNum, err)
			continue
		}
		// Lifecycle options:
		//:queued, :scheduled, :not_run, :not_running, :running or :finished
		if build.Lifecycle == lifecycleFinished {
			return build, nil
		}
		count++
	}
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

// BuildSummary ... requests build summaries for all recent builds
// in the given project
// https://circleci.com/docs/api/v1-reference/#recent-builds-project
func (c *Client) BuildSummary(project *Project, logger io.Writer, input *BuildSummaryInput) ([]*BuildSummaryOutput, error) {
	params := url.Values{}
	if input != nil {
		if input.Limit > 0 {
			params.Set("limit", strconv.Itoa(input.Limit))
		}
		if input.Offset > 0 {
			params.Set("offset", strconv.Itoa(input.Offset))
		}
		if len(input.Filter) > 0 {
			params.Set("filter", input.Filter)
		}
	}
	var output []*BuildSummaryOutput
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		url := fmt.Sprintf("project/%s/%s/%s", project.Vcs, project.Username, project.Reponame)
		err := c.requester(c, "GET", url, params, input, &output)
		if err != nil {
			logf(logger, "BuildSummary failed, GET /%s -> %v", url, err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return output, nil
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

// FindBuildSummaries ... returns all build summaries matching in the project and
// the details in the build project input, that were initiated by the current user
func (c *Client) FindBuildSummaries(project *Project, logger io.Writer, input *BuildProjectInput) ([]*BuildSummaryOutput, error) {
	var (
		selector BuildSummaryInput
		output   []*BuildSummaryOutput
	)
	me, err := c.Me(logger)
	if err != nil {
		return nil, err
	}
	selector.Limit = 100
	// while we receive 100 records in response, continue polling for more records
	for resultNum := selector.Limit; resultNum == selector.Limit; selector.Offset += selector.Limit {
		results, err := c.BuildSummary(project, logger, &selector)
		if err != nil {
			return nil, err
		}
		resultNum = len(results)
		// collect all matching jobs, regardless of status
		for _, result := range results {
			if input.matchSummary(result) &&
				result.Reponame == project.Reponame &&
				result.Lifecycle == lifecycleFinished &&
				result.User.Username == me.Username {
				// push this into output for further filtering based on status
				// and workflowID
				output = append(output, result)
			}
		}
	}
	return output, nil
}

// Projects ... requests all projects visible to the current user
// https://circleci.com/docs/api/v1-reference/#projects
func (c *Client) Projects(logger io.Writer) ([]*Project, error) {
	var projects []*Project
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		err := c.requester(c, "GET", "projects", nil, nil, &projects)
		if err != nil {
			logf(logger, "Projects failed, GET /projects -> %v", err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return projects, nil
}

type followResponse struct {
	Following bool `json:"following"`
}

// FollowProject ... attempts to follow a project that isn't visible
// to the current user
// https://circleci.com/docs/api/v1-reference/#follow-project
func (c *Client) FollowProject(project *Project, logger io.Writer) error {
	var resp followResponse
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		url := fmt.Sprintf("project/%s/%s/%s/follow", project.Vcs, project.Username, project.Reponame)
		err := c.requester(c, "POST", url, nil, nil, &resp)
		if err != nil {
			logf(logger, "FollowProject failed, POST /%s -> %v", url, err)
		}
		return err
	})
	if err != nil {
		return err
	}
	if !resp.Following {
		return fmt.Errorf("attempted to follow %s, following property still false", project.VcsURL)
	}
	return nil
}

// UnfollowProject ... attempts to unfollow a project that isn't visible
// to the current user
// https://circleci.com/docs/api/v1-reference/#follow-project
func (c *Client) UnfollowProject(project *Project, logger io.Writer) error {
	var resp followResponse
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		url := fmt.Sprintf("project/%s/%s/%s/unfollow", project.Vcs, project.Username, project.Reponame)
		err := c.requester(c, "POST", url, nil, nil, &resp)
		if err != nil {
			logf(logger, "UnfollowProject failed, POST /%s -> %v", url, err)
		}
		return err
	})
	if err != nil {
		return err
	}
	if resp.Following {
		return fmt.Errorf("attempted to unfollow %s, following property still true", project.VcsURL)
	}
	return nil
}

// ProjectNotFoundError ... a project was not found when calling FindProject
type ProjectNotFoundError struct {
	Message string
}

func (p *ProjectNotFoundError) Error() string {
	return p.Message
}

// FindProject ... requests all projects visible to the current user
// then calls the provided matcher on each project until the first match
// is found or returns an error
func (c *Client) FindProject(logger io.Writer, matcher func(*Project) bool) (*Project, error) {
	projects, err := c.Projects(logger)
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if matcher(p) {
			return p, nil
		}
	}
	return nil, &ProjectNotFoundError{Message: "failed to locate a project using the given matcher"}
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

// Me ... returns the current user
// https://circleci.com/docs/api/v1-reference/#user
func (c *Client) Me(logger io.Writer) (*User, error) {
	var me User
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		err := c.requester(c, "GET", "me", nil, nil, &me)
		if err != nil {
			logf(logger, "Me failed, GET /me -> %v", err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &me, nil
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

// GetBuild ... returns a *Build for the given buildNum, or an
// error if the request to CircleCI failed
func (c *Client) GetBuild(project *Project, logger io.Writer, buildNum int) (*Build, error) {
	var build Build
	err := retrier(retrierIntervalSecs, retrierAttempts, func() error {
		url := fmt.Sprintf("project/%s/%s/%s/%d", project.Vcs, project.Username, project.Reponame, buildNum)
		err := c.requester(c, "GET", url, nil, nil, &build)
		if err != nil {
			logf(logger, "GetBuild failed, GET /%s -> %v", url, err)
		}
		return err
	})
	if err != nil {
		return nil, err
	}
	return &build, nil
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
