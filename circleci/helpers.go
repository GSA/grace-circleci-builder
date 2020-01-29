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
	"strings"
	"time"
)

const (
	lifecycleFinished = "finished"
)

// nolint: gochecknoglobals
var retrierIntervalSecs, retrierAttempts = 30, 3

//nolint:unparam
func retrier(intervalSecs int, attempts int, fn func() error) (err error) {
	for attempt := 0; attempt < attempts; attempt++ {
		err = fn()
		if err == nil {
			return
		}
		time.Sleep(time.Duration(intervalSecs) * time.Second)
	}
	return
}

func logf(logger io.Writer, format string, args ...interface{}) {
	_, err := fmt.Fprintf(logger, format, args...)
	if err != nil {
		log.Printf(format, args...)
	}
}

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

// request ... used internally to process requests to CircleCI
// nolint: gocyclo
func request(c *Client, method string, path string, params url.Values, input interface{}, output interface{}) error {
	if params == nil {
		params = url.Values{}
	}
	params.Set("circle-token", c.Token)

	u := c.baseURL.ResolveReference(&url.URL{Path: path, RawQuery: params.Encode()})

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

	if resp.StatusCode >= http.StatusMultipleChoices || resp.StatusCode < http.StatusOK {
		return fmt.Errorf("non-success status code returned %s", resp.Status)
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

// timeoutExceededError ... used internally to signify
// a timeout occurred while calling waiter
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

// finalWorkflowStatus checks all build summaries related to the provided workflowID
// if any build has a status not equal to success will return an error
func finalWorkflowStatus(c API, project *Project, logger io.Writer, input *BuildProjectInput, workflowID string) error {
	var (
		summaries []*BuildSummaryOutput
		err       error
	)
	// retry up to 3 times, once every five seconds
	// this should allow us to be resilient to intermittent webservice availability issues
	err = retrier(5, 3, func() error {
		summaries, err = c.BuildSummary(project, logger, nil)
		if err != nil {
			return fmt.Errorf("failed to enumerate build summaries: %v", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("attempted to get build summaries but failed: %v", err)
	}

	for _, s := range summaries {
		if input.matchSummary(s) &&
			s.Workflow != nil &&
			s.Workflow.WorkflowID == workflowID &&
			s.Status != "success" {
			return fmt.Errorf("workflow %s [%s->%s] failed with status: %s", s.Reponame, s.Workflow.WorkflowName, s.Workflow.JobName, s.Status)
		}
	}
	return nil
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
		// skip this summary if the workflow object has not been populated
		if b.Workflow == nil {
			continue
		}
		// if this workflowID has not been seen before, store the status and continue
		if currentStatus, ok = workflows[b.Workflow.WorkflowID]; !ok {
			workflows[b.Workflow.WorkflowID] = b.Status
			continue
		}
		// only update the status for WorkflowID, if it is not equal to 'status'
		if b.Status != currentStatus && b.Status != status {
			workflows[b.Workflow.WorkflowID] = b.Status
		}
	}
	for i, s := range workflows {
		// skip workflows that do not match the provided status
		if s != status {
			continue
		}
		// push all summaries matching this workflowId
		// onto the output slice
		for _, b := range input {
			if b.Workflow == nil {
				continue
			}
			if b.Workflow.WorkflowID == i {
				output = append(output, b)
			}
		}
	}
	return output
}

// ProjectFromURL ... takes a code repository path and converts it
// to a Project object - only tested on github paths
func ProjectFromURL(rawurl string) (*Project, error) {
	const minParts = 2
	u, err := url.Parse(rawurl)
	if err != nil {
		return nil, fmt.Errorf("failed to parse URL: %s -> %v", rawurl, err)
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) < minParts {
		return nil, fmt.Errorf("path not properly formatted: %s", u)
	}
	vcs := strings.Split(u.Host, ".")
	if len(vcs) < minParts {
		return nil, fmt.Errorf("host not properly formatted: %s", u)
	}
	return &Project{
		Username: parts[0],
		Reponame: parts[1],
		Vcs:      vcs[0],
		VcsURL:   u.String(),
	}, nil
}
