package tasks

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// GitFromArchiveTask deploys a git repository from an archive URL
type GitFromArchiveTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// ArchiveURL is the URL of the archive to deploy. Tagged sensitive
	// because URLs can embed credentials (e.g. https://user:token@host/path).
	ArchiveURL string `required:"true" sensitive:"true" yaml:"archive_url" description:"URL of the archive to deploy"`

	// ArchiveType is the format of the archive
	ArchiveType string `required:"false" yaml:"archive_type,omitempty" default:"tar" options:"tar,tar.gz,zip" description:"Format of the archive"`

	// GitUsername is the git author username for the synthetic commit
	GitUsername string `required:"false" yaml:"git_username,omitempty" description:"Git author username for the synthetic commit"`

	// GitEmail is the git author email for the synthetic commit
	GitEmail string `required:"false" yaml:"git_email,omitempty" description:"Git author email for the synthetic commit"`

	// State is the desired state of the deployment
	State State `required:"false" yaml:"state,omitempty" default:"deployed" options:"deployed" description:"Desired state of the deployment"`
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

// ExportSupport reports how docket export handles this task.
func (t GitFromArchiveTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the git from archive task
func (t GitFromArchiveTask) Examples() ([]Doc, error) {
	return MarshalExamples([]GitFromArchiveTaskExample{
		{
			Name: "Deploy a tar.gz archive",
			GitFromArchiveTask: GitFromArchiveTask{
				App:         "node-js-app",
				ArchiveURL:  "https://github.com/dokku/smoke-test-app/archive/refs/heads/master.tar.gz",
				ArchiveType: "tar.gz",
			},
		},
		{
			Name: "Deploy a zip archive with author metadata",
			GitFromArchiveTask: GitFromArchiveTask{
				App:         "node-js-app",
				ArchiveURL:  "https://github.com/dokku/smoke-test-app/archive/refs/heads/master.zip",
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

// Validate checks the GitFromArchiveTask's inputs without contacting the server.
func (t GitFromArchiveTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.ArchiveURL == "" {
		return fmt.Errorf("'archive_url' is required")
	}
	archiveType := t.ArchiveType
	if archiveType == "" {
		archiveType = "tar"
	}
	if !validGitFromArchiveTypes[archiveType] {
		return fmt.Errorf("'archive_type' must be one of tar, tar.gz, zip")
	}
	if (t.GitUsername == "") != (t.GitEmail == "") {
		return fmt.Errorf("'git_username' and 'git_email' must be set together")
	}
	return nil
}

// Plan reports the drift the GitFromArchiveTask would produce.
func (t GitFromArchiveTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StateDeployed: func() PlanResult {
			archiveType := t.ArchiveType
			if archiveType == "" {
				archiveType = "tar"
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

// ExportApp reconstructs a git-from-archive deploy source from apps:report.
// dokku records the archive type (tar/tar.gz/zip) as the source and the URL as
// the metadata; the URL is sensitive, so the engine lifts it into the vars-file.
func (t GitFromArchiveTask) ExportApp(app string) ([]interface{}, error) {
	source, err := getAppDeploySource(app)
	if err != nil {
		return nil, err
	}
	switch source.Source {
	case "tar", "tar.gz", "zip":
		return []interface{}{GitFromArchiveTask{App: app, ArchiveURL: source.SourceMetadata, ArchiveType: source.Source}}, nil
	}
	return nil, nil
}

// init registers the GitFromArchiveTask with the task registry
func init() {
	RegisterTask(&GitFromArchiveTask{})
}
