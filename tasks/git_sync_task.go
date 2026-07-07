package tasks

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// GitSyncTask syncs a git repository to a dokku application
type GitSyncTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Remote is the git remote url to sync
	Remote string `required:"true" yaml:"remote" description:"Git remote url to sync"`

	// GitRef is the git reference to sync
	GitRef string `required:"false" yaml:"git_ref" description:"Git reference to sync"`

	// Build triggers an application build after syncing
	Build bool `required:"false" yaml:"build" description:"Trigger an application build after syncing"`

	// BuildIfChanges triggers a build only if changes are detected
	BuildIfChanges bool `required:"false" yaml:"build_if_changes" description:"Trigger a build only if changes are detected"`

	// SkipDeployBranch skips automatically setting the deploy-branch property
	SkipDeployBranch bool `required:"false" yaml:"skip_deploy_branch" description:"Skip automatically setting the deploy-branch property"`

	// State is the desired state of the git sync
	State State `required:"false" yaml:"state" default:"present" options:"present" description:"Desired state of the git sync"`
}

// GitSyncTaskExample contains an example of a GitSyncTask
type GitSyncTaskExample struct {
	// Name is the task name holding the GitSyncTask description
	Name string `yaml:"-"`

	// GitSyncTask is the GitSyncTask configuration
	GitSyncTask GitSyncTask `yaml:"dokku_git_sync"`
}

// GetName returns the name of the example
func (e GitSyncTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the git sync task
func (t GitSyncTask) Doc() string {
	return "Syncs a git repository to a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t GitSyncTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the git sync task
func (t GitSyncTask) Examples() ([]Doc, error) {
	return MarshalExamples([]GitSyncTaskExample{
		{
			Name: "Sync a git repository to an app",
			GitSyncTask: GitSyncTask{
				App:    "hello-world",
				Remote: "https://github.com/dokku/smoke-test-app.git",
			},
		},
		{
			Name: "Sync a git repository with a specific ref and build",
			GitSyncTask: GitSyncTask{
				App:    "hello-world",
				Remote: "https://github.com/dokku/smoke-test-app.git",
				GitRef: "main",
				Build:  true,
			},
		},
	})
}

// Execute syncs a git repository to a dokku application
func (t GitSyncTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the GitSyncTask would produce.
func (t GitSyncTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			match, err := checkAppSyncState(t.App, t.Remote, t.GitRef)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if match {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			ref := t.GitRef
			if ref == "" {
				ref = "(default branch)"
			}
			args := []string{"git:sync"}
			if t.Build {
				args = append(args, "--build")
			}
			if t.BuildIfChanges {
				args = append(args, "--build-if-changes")
			}
			if t.SkipDeployBranch {
				args = append(args, "--skip-deploy-branch")
			}
			args = append(args, t.App, t.Remote)
			if t.GitRef != "" {
				args = append(args, t.GitRef)
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "remote/ref drift",
				Mutations: []string{fmt.Sprintf("git:sync %s %s %s", t.App, t.Remote, ref)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
	})
}

// checkAppSyncState checks if the app is already synced from the
// expected remote and ref. A transport-level failure
// (`*subprocess.SSHError`) is propagated; any other error is treated
// as "no match" so the planner proposes a re-sync.
func checkAppSyncState(app, expectedRemote, expectedRef string) (bool, error) {
	source, err := getAppDeploySource(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}

	expectedMetadata := fmt.Sprintf("%s#%s", expectedRemote, expectedRef)
	return source.Source == "git-sync" && source.SourceMetadata == expectedMetadata, nil
}

// init registers the GitSyncTask with the task registry
func init() {
	RegisterTask(&GitSyncTask{})
}
