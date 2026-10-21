package tasks

import (
	"context"
	"fmt"

	"github.com/dokku/docket/internal/subprocess"
)

// AppCloneTask clones an existing dokku app to a new app
type AppCloneTask struct {
	// App is the name of the new (target) app
	App string `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the new (target) app"`

	// SourceApp is the name of the existing app to clone from
	SourceApp string `required:"true" apply_only:"true" yaml:"source_app" description:"Name of the existing app to clone from"`

	// SkipDeploy skips deployment of the cloned app
	SkipDeploy bool `required:"false" apply_only:"true" yaml:"skip_deploy,omitempty" description:"Skip deployment of the cloned app"`

	// State is the desired state of the cloned app
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present" description:"Desired state of the cloned app"`
}

// AppCloneTaskExample contains an example of an AppCloneTask
type AppCloneTaskExample struct {
	// Name is the task name holding the AppCloneTask description
	Name string `yaml:"-"`

	// AppCloneTask is the AppCloneTask configuration
	AppCloneTask AppCloneTask `yaml:"dokku_app_clone"`
}

// GetName returns the name of the example
func (e AppCloneTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the app clone task
func (t AppCloneTask) Doc() string {
	return "Clones an existing dokku app to a new app"
}

// Imperative reports that the task runs an operation rather than declaring
// a resource: cloning an app has nothing to undo.
func (t AppCloneTask) Imperative() bool {
	return true
}

// ExportSupport reports how docket export handles this task.
func (t AppCloneTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: "an imperative clone operation, not reconstructable state"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t AppCloneTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "only the target app's existence is probed; the clone source and the cloned contents are never read back"}
}

// MinimumVersions reports the oldest Dokku this task's probe parses.
// The floor is 0.38.31, the release the integration suite runs; the probe is `apps:clone`.
func (t AppCloneTask) MinimumVersions() VersionSupport {
	return VersionSupport{Dokku: "0.38.31"}
}

// examples returns the examples for the app clone task
func (t AppCloneTask) examples() ([]Doc, error) {
	return MarshalExamples([]AppCloneTaskExample{
		{
			Name: "Clone an app",
			AppCloneTask: AppCloneTask{
				App:       "node-js-app-staging",
				SourceApp: "node-js-app",
			},
		},
		{
			Name: "Clone an app without deploying",
			AppCloneTask: AppCloneTask{
				App:        "node-js-app-staging",
				SourceApp:  "node-js-app",
				SkipDeploy: true,
			},
		},
	})
}

// Execute clones an existing dokku app to a new app
func (t AppCloneTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// ObjectEffects reports that the task creates the target app and deploys it
// unless skip_deploy is set.
func (t AppCloneTask) ObjectEffects() ObjectEffects {
	return ObjectEffects{
		Creates:  []ObjectKind{ObjectApp},
		Restarts: []ObjectRestart{{Kind: ObjectApp, Restart: Restart{Unless: []string{"skip_deploy"}}}},
	}
}

// Validate checks the AppCloneTask's inputs without contacting the server.
func (t AppCloneTask) Validate() error {
	if t.State == StatePresent {
		if t.App == "" {
			return fmt.Errorf("'app' is required")
		}
		if t.SourceApp == "" {
			return fmt.Errorf("'source_app' is required")
		}
	}
	return nil
}

// observedAppClone attaches whether app exists to r. Only the target app's
// existence is read; the clone source is apply-only, so Observed carries the
// app alone.
func observedAppClone(r PlanResult, app string, exists bool) PlanResult {
	o := AppCloneTask{App: app, State: StateAbsent}
	if exists {
		o.State = StatePresent
	}
	return observed(r, o, exists)
}

// readAddress reads the app an address names without a clone source, which
// Validate demands in the task's only state.
func (t AppCloneTask) readAddress(ctx context.Context) (Task, bool, error) {
	exists, err := appExists(ctx, t.App)
	if err != nil {
		return nil, false, err
	}
	return observedResult(observedAppClone(PlanResult{Status: PlanStatusOK}, t.App, exists))
}

// Plan reports the drift the AppCloneTask would produce.
func (t AppCloneTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := appExists(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			seen := func(r PlanResult) PlanResult {
				return observedAppClone(r, t.App, exists)
			}
			if exists {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK})
			}
			args := []string{"--quiet", "apps:clone"}
			if t.SkipDeploy {
				args = append(args, "--skip-deploy")
			}
			args = append(args, t.SourceApp, t.App)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("target app %s missing", t.App),
				Mutations: []string{fmt.Sprintf("clone %s -> %s", t.SourceApp, t.App)},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			})
		},
	})
}

// init registers the AppCloneTask with the task registry
func init() {
	RegisterTask(&AppCloneTask{})
}
