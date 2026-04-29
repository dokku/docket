package tasks

import (
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// AppCloneTask clones an existing dokku app to a new app
type AppCloneTask struct {
	// App is the name of the new (target) app
	App string `required:"true" yaml:"app"`

	// SourceApp is the name of the existing app to clone from
	SourceApp string `required:"true" yaml:"source_app"`

	// SkipDeploy skips deployment of the cloned app
	SkipDeploy bool `required:"false" yaml:"skip_deploy,omitempty"`

	// State is the desired state of the cloned app
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present"`
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

// Examples returns the examples for the app clone task
func (t AppCloneTask) Examples() ([]Doc, error) {
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
func (t AppCloneTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the AppCloneTask would produce.
func (t AppCloneTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if t.App == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'app' is required")}
			}
			if t.SourceApp == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'source_app' is required")}
			}
			exists, err := appExists(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if exists {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			args := []string{"--quiet", "apps:clone"}
			if t.SkipDeploy {
				args = append(args, "--skip-deploy")
			}
			args = append(args, t.SourceApp, t.App)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    fmt.Sprintf("target app %s missing", t.App),
				Mutations: []string{fmt.Sprintf("clone %s -> %s", t.SourceApp, t.App)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
	})
}

// init registers the AppCloneTask with the task registry
func init() {
	RegisterTask(&AppCloneTask{})
}
