package circleci

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	lifecycleFinished = "finished"
	jobSuccess        = "success"
)

// isolates the request func for hooking up tests
type requestFunc func(*Client, string, string, url.Values, interface{}, interface{}) error

// Client ... contains necessary data to communicate with circleci
type Client struct {
	//initialized http client, if not provided, will be empty client
	client *http.Client
	//circleci access key used for all requests
	Token string
	//requester
	requester requestFunc
}

// NewClient ... returns a *circleci.Client
func NewClient(client *http.Client, token string) *Client {
	c := &Client{Token: token}
	if client == nil {
		c.client = &http.Client{}
	}
	c.requester = request
	return c
}

// baseURL ... used internally to represent the base URL path for CircleCI API v1.1
func (c *Client) baseURL() *url.URL {
	return &url.URL{Scheme: "https", Host: "circleci.com", Path: "/api/v1.1/"}
}

// request ... used internally to process requests to CircleCI
// nolint: gocyclo
func request(c *Client, method string, path string, params url.Values, input interface{}, output interface{}) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("circle-token", c.Token)

	u := c.baseURL().ResolveReference(&url.URL{Path: path, RawQuery: params.Encode()})

	req, err := http.NewRequest(method, u.String(), nil)
	if err != nil {
		return err
	}

	if input != nil {
		var buf bytes.Buffer
		err = json.NewEncoder(&buf).Encode(input)
		if err != nil {
			return err
		}
		req.Body = ioutil.NopCloser(&buf)
	}

	req.Header.Add("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}
	defer func() {
		err = resp.Body.Close()
		if err != nil {
			log.Printf("failed to close response body -> %v\n", err)
		}
	}()

	if resp.StatusCode >= 300 || resp.StatusCode < 200 {
		return fmt.Errorf("Non-success status code returned %s", resp.Status)
	}

	err = json.NewDecoder(resp.Body).Decode(output)
	if err != nil {
		if val, ok := err.(*json.UnmarshalTypeError); ok {
			return val
		}
		return fmt.Errorf("failed to decode response: %v", err)
	}

	return nil
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
	err := c.requester(c, "POST", fmt.Sprintf("project/%s/%s/%s/build", project.Vcs, project.Username, project.Reponame), nil, input, &output)
	if err != nil {
		return nil, err
	}
	if output.Status != 200 {
		return nil, fmt.Errorf("failed to start project build: %s", output)
	}
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
			_, err = fmt.Fprintf(logger, "waiting for a build summary matching the project: %s\n", project.Reponame)
			if err != nil {
				return false, err
			}
		}
		summary, err = c.findBuildSummary(project, input, after)
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

// summaryNotFoundError ... used internally to signify
// a build summary was not found when calling findBuildSummary
type summaryNotFoundError struct {
	Message string
}

func (e *summaryNotFoundError) Error() string {
	return e.Message
}

// timeoutExceededError ... used internally to signify
// a timeout ocurred while calling waiter
type timeoutExceededError struct {
	Message string
}

func (e *timeoutExceededError) Error() string {
	return e.Message
}

// waiter ... calls checker func every interval until checker returns bool, nil
// or endTime is reached, if endTime is reached a timeoutExceededError will be
// returned
func waiter(interval time.Duration, endTime time.Time, checker func(int) (bool, error)) error {
	var count int
	for {
		if time.Now().After(endTime) {
			return &timeoutExceededError{Message: "time expired while running the checker"}
		}
		time.Sleep(interval)
		done, err := checker(count)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
		count++
	}
}

// findBuildSummary ... used internally to locate a BuildSummary that was executed
// by the current user and was queued after the provided 'after' time.Time
func (c *Client) findBuildSummary(project *Project, input *BuildProjectInput, after time.Time) (*BuildSummaryOutput, error) {
	summaries, err := c.BuildSummary(project, nil)
	if err != nil {
		return nil, err
	}
	me, err := c.Me()
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
func (c *Client) WaitForProjectBuild(project *Project, logger io.Writer, input *BuildProjectInput, summary *BuildSummaryOutput, jobTimeout time.Duration, waitTimeout time.Duration) error {
	buildNum := summary.BuildNum
	for {
		build, err := c.waitForBuild(project, logger, buildNum, jobTimeout)
		if err != nil {
			return err
		}
		if *build.Failed {
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
				return nil
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
	me, err := c.Me()
	if err != nil {
		return nil, err
	}
	err = waiter(time.Second, time.Now().Add(waitTimeout), func(count int) (bool, error) {
		if count%10 == 0 {
			_, err = fmt.Fprintf(logger, "waiting for the next build summary matching the project: %s and workflowId: %s\n", project.Reponame, workflowID)
			if err != nil {
				log.Printf("failed to write to logger: %v\n", err)
			}
		}
		var summaries []*BuildSummaryOutput
		summaries, err = c.BuildSummary(project, nil)
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
	var (
		count   int
		endTime = time.Now().Add(jobTimeout)
	)
	for {
		if time.Now().After(endTime) {
			return nil, fmt.Errorf("job timeout exceeded while waiting for build %s [%d] to finish", project.Reponame, buildNum)
		}
		if count%10 == 0 {
			_, err := fmt.Fprintf(logger, "waiting for build %s [%d] to finish\n", project.Reponame, buildNum)
			if err != nil {
				log.Printf("failed to write to logger -> %v\n", err)
			}
		}
		time.Sleep(2 * time.Second)
		build, err := c.GetBuild(project, buildNum)
		if err != nil {
			//should we return this error? logging for now - BLA
			_, err = fmt.Fprintf(logger, "failed to get build %s [%d] -> %v\n", project.Reponame, buildNum, err)
			if err != nil {
				log.Printf("failed to write to logger -> %v\n", err)
			}
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

// BuildSummary ... requests build summaries for all recent builds
// in the given project
// https://circleci.com/docs/api/v1-reference/#recent-builds-project
func (c *Client) BuildSummary(project *Project, input *BuildSummaryInput) ([]*BuildSummaryOutput, error) {
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
	err := c.requester(c, "GET", fmt.Sprintf("project/%s/%s/%s", project.Vcs, project.Username, project.Reponame), params, input, &output)
	if err != nil {
		return nil, err
	}
	return output, nil
}

// FindBuildSummaries ... returns all build summaries matching in the project and
// the details in the build project input, that were initiated by the current user
func (c *Client) FindBuildSummaries(project *Project, input *BuildProjectInput) ([]*BuildSummaryOutput, error) {
	var (
		selector BuildSummaryInput
		output   []*BuildSummaryOutput
	)
	me, err := c.Me()
	if err != nil {
		return nil, err
	}
	selector.Limit = 100
	for resultNum := selector.Limit; resultNum == selector.Limit; selector.Offset += selector.Limit {
		results, err := c.BuildSummary(project, &selector)
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

// FilterBuildSummariesByWorkflowStatus ... takes a slice of build summaries, collects
// the workflow status per workflow ID seen in the slice, then filters based on the
// final status of the workflow execution, returns a new slice containing the filtered
// objects
// nolint: gocyclo
func FilterBuildSummariesByWorkflowStatus(input []*BuildSummaryOutput, status string) (output []*BuildSummaryOutput) {
	workflows := make(map[string]string)
	for _, b := range input {
		var (
			currentStatus string
			ok            bool
		)
		if b.Workflow == nil {
			continue
		}
		if currentStatus, ok = workflows[b.Workflow.WorkflowID]; !ok {
			workflows[b.Workflow.WorkflowID] = b.Status
			continue
		}
		// only update the status if it is not success
		if b.Status != currentStatus && b.Status != jobSuccess {
			workflows[b.Workflow.WorkflowID] = b.Status
		}
	}
	for i, s := range workflows {
		if s != status {
			continue
		}
		//push all summaries matching this workflowId
		//onto the output slice
		for _, b := range input {
			if b.Workflow == nil {
				continue
			}
			if b.Workflow.WorkflowID == i {
				output = append(output, b)
			}
		}
	}
	return
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

// Projects ... requests all projects visible to the current user
// https://circleci.com/docs/api/v1-reference/#projects
func (c *Client) Projects() ([]*Project, error) {
	var projects []*Project
	err := c.requester(c, "GET", "projects", nil, nil, &projects)
	if err != nil {
		return nil, err
	}
	return projects, nil
}

// FindProject ... requests all projects visible to the current user
// then calls the provided matcher on each project until the first match
// is found or returns an error
func (c *Client) FindProject(matcher func(*Project) bool) (*Project, error) {
	projects, err := c.Projects()
	if err != nil {
		return nil, err
	}
	for _, p := range projects {
		if matcher(p) {
			return p, nil
		}
	}
	return nil, fmt.Errorf("failed to locate a project using the given matcher")
}

// Me ... returns the current user
// https://circleci.com/docs/api/v1-reference/#user
func (c *Client) Me() (*User, error) {
	var me User
	err := c.requester(c, "GET", "me", nil, nil, &me)
	if err != nil {
		return nil, err
	}
	return &me, nil
}

/*
from the plurality of the variable name, you would think
that CircleCI would return this as an array of one
"workflows" : {
    "job_name" : "apply_terraform",
    "job_id" : "cebd33d5-e7d4-4375-8a57-7c484bda30cb",
    "workflow_id" : "25a9035c-1a5a-49e0-9dd6-ad45ac3501b4",
    "workspace_id" : "25a9035c-1a5a-49e0-9dd6-ad45ac3501b4",
    "upstream_job_ids" : [ "5e236d14-7275-423c-bc56-605a5c9a966e", "7c7ffd93-c988-4c82-a4f5-b6a39e3bfdb9" ],
    "upstream_concurrency_map" : { },
    "workflow_name" : "test_and_apply"
  }
*/

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

// GetBuild ... returns a *Build for the given buildNum, or an
// error if the request to CircleCI failed
func (c *Client) GetBuild(project *Project, buildNum int) (*Build, error) {
	var build Build
	err := c.requester(c, "GET", fmt.Sprintf("project/%s/%s/%s/%d", project.Vcs, project.Username, project.Reponame, buildNum), nil, nil, &build)
	if err != nil {
		return nil, err
	}
	return &build, nil
}