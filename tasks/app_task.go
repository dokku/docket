package tasks

import (
	"github.com/dokku/docket/subprocess"
)

// AppTask creates or destroys an app
type AppTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app"`

	// State is the state of the app
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// AppTaskExample contains an example of an AppTask
type AppTaskExample struct {
	// Name is the task name holding the AppTask description
	Name string `yaml:"-"`

	// DokkuApp is the AppTask configuration
	DokkuApp AppTask `yaml:"dokku_app"`
}

// GetName returns the name of the example
func (e AppTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the app task
func (t AppTask) Doc() string {
	return "Creates or destroys an app"
}

// Examples returns a list of AppTaskExamples as yaml
func (t AppTask) Examples() ([]Doc, error) {
	return MarshalExamples([]AppTaskExample{
		{
			Name: "Create an app named hello-world",
			DokkuApp: AppTask{
				App: "hello-world",
			},
		},
		{
			Name: "Destroy the app named hello-world",
			DokkuApp: AppTask{
				App:   "hello-world",
				State: "absent",
			},
		},
	})
}

// Execute creates or destroys an app
func (t AppTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the AppTask would produce.
func (t AppTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			exists, err := appExists(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := createAppInputs(t.App)
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "app missing",
				Mutations: []string{"create app " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			exists, err := appExists(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := destroyAppInputs(t.App)
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "app present",
				Mutations: []string{"destroy app " + t.App},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// appExists checks if an app exists. Returns (true, nil) when the app
// is present, (false, nil) when dokku reports it absent, and
// (false, *subprocess.SSHError) when the probe could not reach the
// server. Plan() callers must short-circuit on the error.
func appExists(appName string) (bool, error) {
	return subprocess.Probe(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "apps:exists", appName},
	})
}

// createAppInputs returns the subprocess inputs that create app.
func createAppInputs(app string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", "apps:create", app}},
	}
}

// destroyAppInputs returns the subprocess inputs that destroy app.
func destroyAppInputs(app string) []subprocess.ExecCommandInput {
	return []subprocess.ExecCommandInput{
		{Command: "dokku", Args: []string{"--quiet", "--force", "apps:destroy", app}},
	}
}

// destroyApp is retained as an integration-test helper. It runs the
// destroy-app apply path synchronously.
func destroyApp(app string) TaskOutputState {
	exists, _ := appExists(app)
	if !exists {
		return TaskOutputState{Changed: false, State: StateAbsent}
	}
	return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, destroyAppInputs(app))
}

// createApp is retained as an integration-test helper. It runs the
// create-app apply path synchronously.
func createApp(app string) TaskOutputState {
	exists, _ := appExists(app)
	if exists {
		return TaskOutputState{Changed: false, State: StatePresent}
	}
	return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, createAppInputs(app))
}

// init registers the AppTask with the task registry
func init() {
	RegisterTask(&AppTask{})
}
