package tasks

import (
	"context"
	"fmt"

	"github.com/dokku/docket/internal/subprocess"
)

// GitFromArchiveTask deploys a git repository from an archive URL
type GitFromArchiveTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// ArchiveURL is the URL of the archive to deploy. Tagged sensitive
	// because URLs can embed credentials (e.g. https://user:token@host/path).
	ArchiveURL string `required:"true" sensitive:"true" yaml:"archive_url" description:"URL of the archive to deploy"`

	// ArchiveType is the format of the archive
	ArchiveType string `required:"false" yaml:"archive_type,omitempty" default:"tar" options:"tar,tar.gz,zip" description:"Format of the archive"`

	// GitUsername is the git author username for the synthetic commit
	GitUsername string `required:"false" apply_only:"true" yaml:"git_username,omitempty" description:"Git author username for the synthetic commit"`

	// GitEmail is the git author email for the synthetic commit
	GitEmail string `required:"false" apply_only:"true" yaml:"git_email,omitempty" description:"Git author email for the synthetic commit"`

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t GitFromArchiveTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "the recorded archive source is probed; the archive contents are not hashed, so a changed archive at the same url plans as in sync"}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `git:report`.
func (t GitFromArchiveTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the git from archive task
func (t GitFromArchiveTask) examples() ([]Doc, error) {
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
func (t GitFromArchiveTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
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
func (t GitFromArchiveTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StateDeployed: func() PlanResult {
			archiveType := t.ArchiveType
			if archiveType == "" {
				archiveType = "tar"
			}
			// The archive type is stored as the deploy source value, so a
			// tar.gz deploy reports source "tar.gz".
			source, read, err := readAppDeploySource(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			seen := func(r PlanResult) PlanResult {
				if !read {
					return r
				}
				if !isArchiveDeploySource(source.Source) {
					return observed(r, GitFromArchiveTask{App: t.App, State: StateAbsent}, false)
				}
				// The URL can carry credentials, and the server's value is
				// not the recipe's, so mask it too.
				subprocess.MaskerFromContext(ctx).Add(source.SourceMetadata)
				return observed(r, GitFromArchiveTask{App: t.App, ArchiveURL: source.SourceMetadata, ArchiveType: source.Source, State: StateDeployed}, true)
			}
			if read && source.Source == archiveType && source.SourceMetadata == t.ArchiveURL {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK})
			}
			args := []string{"git:from-archive", "--archive-type", archiveType, t.App, t.ArchiveURL}
			if t.GitUsername != "" {
				args = append(args, t.GitUsername, t.GitEmail)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "archive source drift",
				Mutations: []string{fmt.Sprintf("git:from-archive %s %s (%s)", t.App, t.ArchiveURL, archiveType)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: "undeployed"}, StateDeployed, inputs)
				},
			})
		},
	})
}

// isArchiveDeploySource reports whether a deploy source value is one
// git:from-archive records: the archive type it was deployed from.
func isArchiveDeploySource(source string) bool {
	switch source {
	case "tar", "tar.gz", "zip":
		return true
	}
	return false
}

// ExportApp reconstructs a git-from-archive deploy source from apps:report.
// dokku records the archive type (tar/tar.gz/zip) as the source and the URL as
// the metadata; the URL is sensitive, so the engine lifts it into the vars-file.
func (t GitFromArchiveTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	source, err := getAppDeploySource(ctx, app)
	if err != nil {
		return nil, err
	}
	if !isArchiveDeploySource(source.Source) {
		return nil, nil
	}
	return []interface{}{GitFromArchiveTask{App: app, ArchiveURL: source.SourceMetadata, ArchiveType: source.Source}}, nil
}

// init registers the GitFromArchiveTask with the task registry
func init() {
	RegisterTask(&GitFromArchiveTask{})
}
