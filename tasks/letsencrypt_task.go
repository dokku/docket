package tasks

import (
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// LetsencryptTask enables or disables the dokku-letsencrypt plugin for a dokku app
type LetsencryptTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// State is the desired state of the letsencrypt integration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// LetsencryptTaskExample contains an example of a LetsencryptTask
type LetsencryptTaskExample struct {
	// Name is the task name holding the LetsencryptTask description
	Name string `yaml:"-"`

	// LetsencryptTask is the LetsencryptTask configuration
	LetsencryptTask LetsencryptTask `yaml:"dokku_letsencrypt"`
}

// GetName returns the name of the example
func (e LetsencryptTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the letsencrypt task
func (t LetsencryptTask) Doc() string {
	return "Enables or disables letsencrypt SSL certificates for a dokku application"
}

// Examples returns the examples for the letsencrypt task
func (t LetsencryptTask) Examples() ([]Doc, error) {
	return MarshalExamples([]LetsencryptTaskExample{
		{
			Name: "Enable letsencrypt for an app",
			LetsencryptTask: LetsencryptTask{
				App: "node-js-app",
			},
		},
		{
			Name: "Disable letsencrypt for an app",
			LetsencryptTask: LetsencryptTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute enables or disables letsencrypt
func (t LetsencryptTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the LetsencryptTask would produce.
func (t LetsencryptTask) Plan() PlanResult {
	if t.App == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'app' is required")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			active, err := letsencryptActive(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if active {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "letsencrypt:enable", t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "letsencrypt not active",
				Mutations: []string{"letsencrypt:enable " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			active, err := letsencryptActive(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !active {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "letsencrypt:disable", t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "letsencrypt active",
				Mutations: []string{"letsencrypt:disable " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// letsencryptActive reports whether letsencrypt is currently active for an app.
// Mirrors the upstream `dokku letsencrypt:active <app>` output ("true"/"false").
func letsencryptActive(app string) (bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "letsencrypt:active", app},
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// enableLetsencrypt runs `dokku letsencrypt:enable APP` if not already active
func enableLetsencrypt(app string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   StateAbsent,
	}

	if app == "" {
		state.Error = fmt.Errorf("'app' is required")
		return state
	}

	active, err := letsencryptActive(app)
	if err != nil {
		state.Error = err
		state.Message = err.Error()
		return state
	}
	if active {
		state.State = StatePresent
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "letsencrypt:enable", app},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = StatePresent
	return state
}

// disableLetsencrypt runs `dokku letsencrypt:disable APP` if currently active
func disableLetsencrypt(app string) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   StatePresent,
	}

	if app == "" {
		state.Error = fmt.Errorf("'app' is required")
		return state
	}

	active, err := letsencryptActive(app)
	if err != nil {
		state.Error = err
		state.Message = err.Error()
		return state
	}
	if !active {
		state.State = StateAbsent
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "letsencrypt:disable", app},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = StateAbsent
	return state
}

// init registers the LetsencryptTask with the task registry
func init() {
	RegisterTask(&LetsencryptTask{})
}
