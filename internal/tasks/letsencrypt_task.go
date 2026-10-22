package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// LetsencryptTask enables or disables the dokku-letsencrypt plugin for a dokku app
type LetsencryptTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" serialize:"app" yaml:"app" description:"Name of the app"`

	// State is the desired state of the letsencrypt integration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the letsencrypt integration"`
}

// LetsencryptTaskExample contains an example of a LetsencryptTask
type LetsencryptTaskExample struct {
	// Name is the task name holding the LetsencryptTask description
	Name string `yaml:"-"`

	// LetsencryptTask is the LetsencryptTask configuration
	LetsencryptTask LetsencryptTask `yaml:"dokku_letsencrypt"`

	// Requires are the tasks the example needs applied first, beyond the
	// ones TaskRequires derives.
	Requires []Task `yaml:"-"`
}

// GetName returns the name of the example
func (e LetsencryptTaskExample) GetName() string {
	return e.Name
}

// GetRequires returns the tasks the example declares it needs
func (e LetsencryptTaskExample) GetRequires() []Task {
	return e.Requires
}

// Doc returns the docblock for the letsencrypt task
func (t LetsencryptTask) Doc() string {
	return "Enables or disables letsencrypt SSL certificates for a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t LetsencryptTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t LetsencryptTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// The probe is `letsencrypt:active`, not the property report. The floor is
// 0.25.2, the plugin:list version of the release the integration suite
// installs. Tag 0.25.0's plugin.toml still said 0.20.4. 0.38.31 is the Dokku
// that suite runs.
func (t LetsencryptTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			Name:    "letsencrypt",
			Minimum: "0.25.2",
			Text:    "dokku-letsencrypt plugin >= 0.25.2",
		}},
	}
}

// examples returns the examples for the letsencrypt task
func (t LetsencryptTask) examples() ([]Doc, error) {
	return MarshalExamples([]LetsencryptTaskExample{
		{
			Name:     "Enable letsencrypt for an app",
			Requires: []Task{deployedExampleApp("node-js-app")},
			LetsencryptTask: LetsencryptTask{
				App: "node-js-app",
			},
		},
		{
			Name:     "Disable letsencrypt for an app",
			Requires: []Task{deployedExampleApp("node-js-app")},
			LetsencryptTask: LetsencryptTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute enables or disables letsencrypt
func (t LetsencryptTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// Validate checks the LetsencryptTask's inputs without contacting the server.
func (t LetsencryptTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	return nil
}

// Plan reports the drift the LetsencryptTask would produce.
func (t LetsencryptTask) Plan(ctx context.Context) (result PlanResult) {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	defer recordPlanned(&result, t)
	// seen attaches whether the resource exists; its identity is all there is
	// to read.
	seen := func(r PlanResult, exists bool) PlanResult {
		o := LetsencryptTask{App: t.App}
		o.State = StateAbsent
		if exists {
			o.State = StatePresent
		}
		return observed(r, t, o, exists)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			active, err := letsencryptActive(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if active {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK}, true)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "letsencrypt:enable", t.App},
			}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "letsencrypt not active",
				Mutations: []string{"letsencrypt:enable " + t.App},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}, false)
		},
		StateAbsent: func() PlanResult {
			active, err := letsencryptActive(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !active {
				return seen(PlanResult{InSync: true, Status: PlanStatusOK}, false)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "letsencrypt:disable", t.App},
			}}
			return seen(PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "letsencrypt active",
				Mutations: []string{"letsencrypt:disable " + t.App},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}, true)
		},
	})
}

// ExportApp emits a dokku_letsencrypt task when letsencrypt is active for the
// app (it is inactive by default, so a normal app needs no task).
func (t LetsencryptTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	active, err := letsencryptActive(ctx, app)
	if err != nil {
		return nil, err
	}
	if !active {
		return nil, nil
	}
	return []interface{}{LetsencryptTask{App: app, State: StatePresent}}, nil
}

// letsencryptActive reports whether letsencrypt is currently active for an app.
// Mirrors the upstream `dokku letsencrypt:active <app>` output ("true"/"false").
func letsencryptActive(ctx context.Context, app string) (bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "letsencrypt:active", app},
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// init registers the LetsencryptTask with the task registry
func init() {
	RegisterTask(&LetsencryptTask{})
}
