package tasks

import (
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// AppLockTask locks or unlocks a dokku application from deployment
type AppLockTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// State is the desired lock state
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// AppLockTaskExample contains an example of an AppLockTask
type AppLockTaskExample struct {
	// Name is the task name holding the AppLockTask description
	Name string `yaml:"-"`

	// AppLockTask is the AppLockTask configuration
	AppLockTask AppLockTask `yaml:"dokku_app_lock"`
}

// GetName returns the name of the example
func (e AppLockTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the app lock task
func (t AppLockTask) Doc() string {
	return "Locks or unlocks a dokku application from deployment"
}

// Examples returns the examples for the app lock task
func (t AppLockTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AppLockTaskExample{
		{
			Name: "Lock an app",
			AppLockTask: AppLockTask{
				App: "node-js-app",
			},
		},
		{
			Name: "Unlock an app",
			AppLockTask: AppLockTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute locks or unlocks the app
func (t AppLockTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the AppLockTask would produce.
func (t AppLockTask) Plan() PlanResult {
	if t.App == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'app' is required")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			locked, err := appLocked(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if locked {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "apps:lock", t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "app unlocked",
				Mutations: []string{"lock " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			locked, err := appLocked(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !locked {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "apps:unlock", t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "app locked",
				Mutations: []string{"unlock " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// appLocked checks if a dokku app is locked. Returns (false,
// *subprocess.SSHError) on transport failure; (false, nil) when dokku
// reports unlocked; (true, nil) when locked.
func appLocked(app string) (bool, error) {
	return subprocess.Probe(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "apps:locked", app},
	})
}

// init registers the AppLockTask with the task registry
func init() {
	RegisterTask(&AppLockTask{})
}
