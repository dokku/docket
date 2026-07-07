package tasks

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// git:from-image [--build-dir DIRECTORY] <app> <docker-image> [<git-username> <git-email>]

// GitFromImageTask deploys a git repository from a docker image
type GitFromImageTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Image is the docker image to deploy. Tagged sensitive because image
	// references can embed registry credentials (e.g. user:token@host/repo).
	Image string `required:"true" sensitive:"true" yaml:"image" description:"Docker image to deploy"`

	// BuildDir is the directory to build the git repository
	BuildDir string `required:"false" yaml:"build_dir,omitempty" description:"Directory to build the git repository"`

	// GitUsername is the username to use for the git repository
	GitUsername string `required:"false" yaml:"git_username,omitempty" description:"Username to use for the git repository"`

	// GitEmail is the email to use for the git repository
	GitEmail string `required:"false" yaml:"git_email,omitempty" description:"Email to use for the git repository"`

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

// Examples returns the examples for the git from image task
func (t GitFromImageTask) Examples() ([]Doc, error) {
	return MarshalExamples([]GitFromImageTaskExample{
		{
			Name: "Deploy an app from a docker image",
			GitFromImageTask: GitFromImageTask{
				App:   "node-js-app",
				Image: "dokku/node-js-app:latest",
			},
		},
		{
			Name: "Deploy from an image with a build directory and git author",
			GitFromImageTask: GitFromImageTask{
				App:         "node-js-app",
				Image:       "dokku/node-js-app:latest",
				BuildDir:    "/app",
				GitUsername: "dokku",
				GitEmail:    "dokku@example.com",
			},
		},
	})
}

// Execute deploys a git repository from a docker image
func (t GitFromImageTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the GitFromImageTask would produce.
func (t GitFromImageTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StateDeployed: func() PlanResult {
			match, err := checkAppSourceImage(t.App, t.Image)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if match {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"git:from-image"}
			if t.BuildDir != "" {
				args = append(args, "--build-dir", t.BuildDir)
			}
			args = append(args, t.App, t.Image)
			if t.GitUsername != "" {
				args = append(args, t.GitUsername)
			}
			if t.GitEmail != "" {
				args = append(args, t.GitEmail)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "image source drift",
				Mutations: []string{fmt.Sprintf("git:from-image %s %s", t.App, t.Image)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: "undeployed"}, StateDeployed, inputs)
				},
			}
		},
	})
}

// checkAppSourceImage checks if the app is already deployed from a
// docker image. A transport-level failure (`*subprocess.SSHError`) is
// propagated; any other error is treated as "no match" so the planner
// proposes a re-deploy.
func checkAppSourceImage(app, expectedImage string) (bool, error) {
	source, err := getAppDeploySource(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}

	return source.Source == "docker-image" && source.SourceMetadata == expectedImage, nil
}

// ExportApp reconstructs a docker-image deploy source from apps:report. The
// image reference is sensitive, so the engine lifts it into the vars-file.
func (t GitFromImageTask) ExportApp(app string) ([]interface{}, error) {
	source, err := getAppDeploySource(app)
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
