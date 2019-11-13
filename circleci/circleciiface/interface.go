package circleciiface

import (
	"io"
	"time"

	"github.com/GSA/grace-circleci-builder/circleci"
)

// CIRCLECIAPI provides an interface to enable mocking the CircleCI REST client
type CIRCLECIAPI interface {
	BuildProject(*circleci.Project, io.Writer, *circleci.BuildProjectInput, time.Duration) (*circleci.BuildSummaryOutput, error)
	WaitForProjectBuild(*circleci.Project, io.Writer, *circleci.BuildProjectInput, *circleci.BuildSummaryOutput, time.Duration, time.Duration, bool) error
	BuildSummary(*circleci.Project, io.Writer, *circleci.BuildSummaryInput) ([]*circleci.BuildSummaryOutput, error)
	FindBuildSummaries(*circleci.Project, io.Writer, *circleci.BuildProjectInput) ([]*circleci.BuildSummaryOutput, error)
	Projects(io.Writer) ([]*circleci.Project, error)
	FollowProject(*circleci.Project, io.Writer) error
	UnfollowProject(*circleci.Project, io.Writer) error
	FindProject(io.Writer, func(*circleci.Project) bool) (*circleci.Project, error)
	Me(io.Writer) (*circleci.User, error)
	GetBuild(*circleci.Project, io.Writer, int) (*circleci.Build, error)
}

var _ CIRCLECIAPI = (*circleci.Client)(nil)
