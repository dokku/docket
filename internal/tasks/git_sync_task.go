package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// GitSyncTask syncs a git repository to a dokku application
type GitSyncTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Remote is the git remote url to sync
	Remote string `required:"true" yaml:"remote" description:"Git remote url to sync"`

	// GitRef is the git reference to sync
	GitRef string `required:"false" yaml:"git_ref" description:"Git reference to sync"`

	// Build triggers an application build after syncing
	Build bool `required:"false" apply_only:"true" yaml:"build" description:"Trigger an application build after syncing"`

	// BuildIfChanges triggers a build only if changes are detected
	BuildIfChanges bool `required:"false" apply_only:"true" yaml:"build_if_changes" description:"Trigger a build only if changes are detected"`

	// SkipDeployBranch skips automatically setting the deploy-branch property
	SkipDeployBranch bool `required:"false" apply_only:"true" yaml:"skip_deploy_branch" description:"Skip automatically setting the deploy-branch property"`

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t GitSyncTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "dokku records the resolved commit as `<remote>#<sha>`, which cannot be compared against a branch or tag name, so only the remote and the persisted deploy branch are probed"}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `git:report`.
func (t GitSyncTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the git sync task
func (t GitSyncTask) examples() ([]Doc, error) {
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
				GitRef: "master",
				Build:  true,
			},
		},
	})
}

// Execute syncs a git repository to a dokku application
func (t GitSyncTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Plan reports the drift the GitSyncTask would produce.
func (t GitSyncTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			sync, err := readAppSyncState(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			seen := func(r PlanResult) PlanResult {
				if !sync.read {
					return r
				}
				return observed(r, sync.observed, sync.observed.State == StatePresent)
			}
			if sync.matches(t.Remote, t.GitRef, t.SkipDeployBranch) {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK})
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
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "remote/ref drift",
				Mutations: []string{fmt.Sprintf("git:sync %s %s %s", t.App, t.Remote, ref)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			})
		},
	})
}

// appSyncState is what the git-sync probe read for an app: whether its last
// deploy came from git:sync, the remote it synced, and the deploy-branch
// git:sync persisted from the ref.
type appSyncState struct {
	// sourceRead is false when apps:report could not be read or parsed.
	sourceRead bool

	// gitSync is true when the app's deploy source is git-sync.
	gitSync bool

	// remote is the synced remote, the metadata before its last "#".
	remote string

	// branchRead is false when git:report could not be read or parsed.
	branchRead bool

	// deployBranch is the deploy-branch dokku has stored.
	deployBranch string

	// read is true when observed holds everything the exporter reads.
	read bool

	// observed is the app's git sync as the exporter would emit it, or the
	// app alone with StateAbsent when its deploy source is not git-sync.
	observed GitSyncTask
}

// matches reports whether the app is already synced from the expected remote
// and ref. dokku records the deploy source as "<remote>#<resolved SHA>" (and
// only after a build), so the stored SHA cannot be compared against a branch or
// tag name. Instead the remote portion of the metadata is compared, and the ref
// is compared against the deploy-branch git:sync persists from it (unless the
// caller opted out with skip_deploy_branch, or pinned no ref, in which case a
// matching remote is the most that can be verified offline). A read that
// failed is "no match", so the planner proposes a re-sync.
func (s appSyncState) matches(expectedRemote, expectedRef string, skipDeployBranch bool) bool {
	if !s.sourceRead || !s.gitSync || s.remote != expectedRemote {
		return false
	}
	if expectedRef == "" || skipDeployBranch {
		return true
	}
	return s.branchRead && s.deployBranch == expectedRef
}

// readAppSyncState reads an app's deploy source and, when it is git-sync, the
// deploy-branch. A transport-level failure (`*subprocess.SSHError`) is
// returned; any other read failure is recorded on the state, which then
// matches nothing and observes nothing.
func readAppSyncState(ctx context.Context, app string) (appSyncState, error) {
	var state appSyncState
	source, err := getAppDeploySource(ctx, app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return state, err
		}
		return state, nil
	}
	state.sourceRead = true
	if source.Source != "git-sync" {
		state.read = true
		state.observed = GitSyncTask{App: app, State: StateAbsent}
		return state, nil
	}
	state.gitSync = true
	state.remote = source.SourceMetadata
	if i := strings.LastIndex(source.SourceMetadata, "#"); i >= 0 {
		state.remote = source.SourceMetadata[:i]
	}

	deployBranch, err := getGitDeployBranch(ctx, app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return state, err
		}
		return state, nil
	}
	state.branchRead = true
	state.deployBranch = deployBranch
	state.read = true
	state.observed = GitSyncTask{App: app, Remote: state.remote, GitRef: deployBranch, State: StatePresent}
	return state, nil
}

// getGitDeployBranch returns the deploy-branch dokku has stored for the app,
// read from `git:report --format json` (JSON key `deploy-branch`). git:sync
// sets it from the synced ref by default, so it is the offline signal for which
// ref the app currently tracks.
func getGitDeployBranch(ctx context.Context, app string) (string, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"git:report", app, "--format", "json"},
	})
	if err != nil {
		return "", err
	}
	var report struct {
		DeployBranch string `json:"deploy-branch"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return "", err
	}
	return report.DeployBranch, nil
}

// ExportApp reconstructs a git-sync deploy source from apps:report. The metadata
// records "<remote>#<resolved SHA>", so the remote is taken from the portion
// before the last "#" and the ref from the deploy-branch git:sync persisted -
// not the SHA, which the probe cannot match against a branch or tag. It only
// emits when the app was last deployed via git:sync.
func (t GitSyncTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	source, err := getAppDeploySource(ctx, app)
	if err != nil {
		return nil, err
	}
	if source.Source != "git-sync" {
		return nil, nil
	}
	remote := source.SourceMetadata
	if i := strings.LastIndex(source.SourceMetadata, "#"); i >= 0 {
		remote = source.SourceMetadata[:i]
	}
	ref, err := getGitDeployBranch(ctx, app)
	if err != nil {
		return nil, err
	}
	return []interface{}{GitSyncTask{App: app, Remote: remote, GitRef: ref}}, nil
}

// init registers the GitSyncTask with the task registry
func init() {
	RegisterTask(&GitSyncTask{})
}
