package tasks

import (
	"context"
	"errors"
	"fmt"

	"github.com/dokku/docket/internal/subprocess"
)

// git:from-image [--build-dir DIRECTORY] <app> <docker-image> [<git-username> <git-email>]

// GitFromImageTask deploys a git repository from a docker image
type GitFromImageTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Image is the docker image to deploy. Tagged sensitive because image
	// references can embed registry credentials (e.g. user:token@host/repo).
	Image string `required:"true" sensitive:"true" yaml:"image" description:"Docker image to deploy"`

	// BuildDir is the directory to build the git repository
	BuildDir string `required:"false" apply_only:"true" yaml:"build_dir,omitempty" description:"Directory to build the git repository"`

	// GitUsername is the username to use for the git repository
	GitUsername string `required:"false" apply_only:"true" yaml:"git_username,omitempty" description:"Username to use for the git repository (requires git_email)"`

	// GitEmail is the email to use for the git repository
	GitEmail string `required:"false" apply_only:"true" yaml:"git_email,omitempty" description:"Email to use for the git repository (requires git_username)"`

	// State is the desired state of the git repository
	State State `required:"false" yaml:"state,omitempty" default:"deployed" options:"deployed" description:"Desired state of the git repository"`
}

// GitFromImageTaskExample contains an example of a GitFromImageTask
type GitFromImageTaskExample struct {
	// Name is the task name holding the GitFromImageTask description
	Name string `yaml:"-"`

	// GitFromImageTask is the GitFromImageTask configuration
	GitFromImageTask GitFromImageTask `yaml:"dokku_git_from_image"`
}

// GetName returns the name of the example
func (e GitFromImageTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the git from image task
func (t GitFromImageTask) Doc() string {
	return "Deploys a git repository from a docker image"
}

// ExportSupport reports how docket export handles this task.
func (t GitFromImageTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "the image reference is written to the companion vars-file"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t GitFromImageTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "the recorded image reference is probed; the image digest is not, so a mutable tag that moved plans as in sync"}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `git:report`.
func (t GitFromImageTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the git from image task
func (t GitFromImageTask) examples() ([]Doc, error) {
	return MarshalExamples([]GitFromImageTaskExample{
		{
			Name: "Deploy an app from a docker image",
			GitFromImageTask: GitFromImageTask{
				App:   "node-js-app",
				Image: "dokku/smoke-test-app:dockerfile",
			},
		},
		{
			Name: "Deploy from an image with git author metadata",
			GitFromImageTask: GitFromImageTask{
				App:         "node-js-app",
				Image:       "dokku/smoke-test-app:dockerfile",
				GitUsername: "dokku",
				GitEmail:    "dokku@example.com",
			},
		},
	})
}

// Execute deploys a git repository from a docker image
func (t GitFromImageTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the GitFromImageTask's inputs without contacting the server.
func (t GitFromImageTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	if t.Image == "" {
		return fmt.Errorf("'image' is required")
	}
	// git:from-image takes <git-username> and <git-email> as an adjacent
	// positional pair, so an email supplied without a username would be
	// passed in the username slot. Require them together, matching
	// GitFromArchiveTask.
	if (t.GitUsername == "") != (t.GitEmail == "") {
		return fmt.Errorf("'git_username' and 'git_email' must be set together")
	}
	return nil
}

// Plan reports the drift the GitFromImageTask would produce.
func (t GitFromImageTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StateDeployed: func() PlanResult {
			source, read, err := readAppDeploySource(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			seen := func(r PlanResult) PlanResult {
				if !read {
					return r
				}
				if source.Source != "docker-image" {
					return observed(r, GitFromImageTask{App: t.App, State: StateAbsent}, false)
				}
				// The image reference can carry registry credentials, and
				// the server's value is not the recipe's, so mask it too.
				subprocess.MaskerFromContext(ctx).Add(source.SourceMetadata)
				return observed(r, GitFromImageTask{App: t.App, Image: source.SourceMetadata, State: StateDeployed}, true)
			}
			if read && source.Source == "docker-image" && source.SourceMetadata == t.Image {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK})
			}
			args := []string{"git:from-image"}
			if t.BuildDir != "" {
				args = append(args, "--build-dir", t.BuildDir)
			}
			args = append(args, t.App, t.Image)
			// Validate guarantees username and email are set together, so
			// append them as a unit to keep them in their positional slots.
			if t.GitUsername != "" {
				args = append(args, t.GitUsername, t.GitEmail)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "image source drift",
				Mutations: []string{fmt.Sprintf("git:from-image %s %s", t.App, t.Image)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: "undeployed"}, StateDeployed, inputs)
				},
			})
		},
	})
}

// readAppDeploySource reads an app's deploy source for the git deploy tasks'
// probes. A transport-level failure (`*subprocess.SSHError`) is returned; any
// other failure reports read as false, which the planners treat as "no match"
// so they propose a re-deploy, and which leaves the plan with no observed
// state.
func readAppDeploySource(ctx context.Context, app string) (AppDeploySource, bool, error) {
	source, err := getAppDeploySource(ctx, app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return AppDeploySource{}, false, err
		}
		return AppDeploySource{}, false, nil
	}
	return source, true, nil
}

// ExportApp reconstructs a docker-image deploy source from apps:report. The
// image reference is sensitive, so the engine lifts it into the vars-file.
func (t GitFromImageTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	source, err := getAppDeploySource(ctx, app)
	if err != nil {
		return nil, err
	}
	if source.Source != "docker-image" {
		return nil, nil
	}
	return []interface{}{GitFromImageTask{App: app, Image: source.SourceMetadata}}, nil
}

// init registers the GitFromImageTask with the task registry
func init() {
	RegisterTask(&GitFromImageTask{})
}
