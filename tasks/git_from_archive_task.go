package tasks

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// GitFromArchiveTask deploys a git repository from an archive URL
type GitFromArchiveTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// ArchiveURL is the URL of the archive to deploy. Tagged sensitive
	// because URLs can embed credentials (e.g. https://user:token@host/path).
	ArchiveURL string `required:"true" sensitive:"true" yaml:"archive_url"`

	// ArchiveType is the format of the archive
	ArchiveType string `required:"false" yaml:"archive_type,omitempty" default:"tar" options:"tar,tar.gz,zip"`

	// GitUsername is the git author username for the synthetic commit
	GitUsername string `required:"false" yaml:"git_username,omitempty"`

	// GitEmail is the git author email for the synthetic commit
	GitEmail string `required:"false" yaml:"git_email,omitempty"`

	// State is the desired state of the deployment
	State State `required:"false" yaml:"state,omitempty" default:"deployed" options:"deployed"`
}

// GitFromArchiveTaskExample contains an example of a GitFromArchiveTask
type GitFromArchiveTaskExample struct {
	// Name is the task name holding the GitFromArchiveTask description
	Name string `yaml:"-"`

	// GitFromArchiveTask is the GitFromArchiveTask configuration
	GitFromArchiveTask GitFromArchiveTask `yaml:"dokku_git_from_archive"`
}

// GetName returns the name of the example
func (e GitFromArchiveTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the git from archive task
func (t GitFromArchiveTask) Doc() string {
	return "Deploys a git repository from an archive URL"
}

// Examples returns the examples for the git from archive task
func (t GitFromArchiveTask) Examples() ([]Doc, error) {
	return MarshalExamples([]GitFromArchiveTaskExample{
		{
			Name: "Deploy a tar archive",
			GitFromArchiveTask: GitFromArchiveTask{
				App:        "node-js-app",
				ArchiveURL: "https://example.com/release-1.0.0.tar",
			},
		},
		{
			Name: "Deploy a zip archive with author metadata",
			GitFromArchiveTask: GitFromArchiveTask{
				App:         "node-js-app",
				ArchiveURL:  "https://example.com/release-1.0.0.zip",
				ArchiveType: "zip",
				GitUsername: "deploy-bot",
				GitEmail:    "deploy@example.com",
			},
		},
	})
}

var validGitFromArchiveTypes = map[string]bool{"tar": true, "tar.gz": true, "zip": true}

// Execute deploys a git repository from an archive URL
func (t GitFromArchiveTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the GitFromArchiveTask would produce.
func (t GitFromArchiveTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StateDeployed: func() PlanResult {
			if t.App == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'app' is required")}
			}
			if t.ArchiveURL == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'archive_url' is required")}
			}
			archiveType := t.ArchiveType
			if archiveType == "" {
				archiveType = "tar"
			}
			if !validGitFromArchiveTypes[archiveType] {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'archive_type' must be one of tar, tar.gz, zip")}
			}
			if (t.GitUsername == "") != (t.GitEmail == "") {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'git_username' and 'git_email' must be set together")}
			}
			match, err := checkAppSourceArchive(t.App, archiveType, t.ArchiveURL)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if match {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"git:from-archive", "--archive-type", archiveType, t.App, t.ArchiveURL}
			if t.GitUsername != "" {
				args = append(args, t.GitUsername, t.GitEmail)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "archive source drift",
				Mutations: []string{fmt.Sprintf("git:from-archive %s %s (%s)", t.App, t.ArchiveURL, archiveType)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: "undeployed"}, StateDeployed, inputs)
				},
			}
		},
	})
}

// checkAppSourceArchive returns true if the app is already deployed
// from the expected archive URL with the expected archive type. The
// archive type is stored as the deploy source value, so a tar.gz
// deploy reports source "tar.gz". A transport-level failure
// (`*subprocess.SSHError`) is propagated; any other error is treated
// as "no match" so the planner proposes a re-deploy.
func checkAppSourceArchive(app, expectedType, expectedURL string) (bool, error) {
	source, err := getAppDeploySource(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}
	return source.Source == expectedType && source.SourceMetadata == expectedURL, nil
}

// init registers the GitFromArchiveTask with the task registry
func init() {
	RegisterTask(&GitFromArchiveTask{})
}
