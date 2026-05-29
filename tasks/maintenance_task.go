package tasks

import (
	"strings"

	"github.com/dokku/docket/subprocess"
)

// maintenanceEnabled probes whether maintenance mode is enabled for an app via
// `dokku --quiet maintenance:report <app> --maintenance-enabled`. Output is
// "true"/"false".
func maintenanceEnabled(ctx ToggleContext) (bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "maintenance:report", ctx.App, "--maintenance-enabled"},
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// MaintenanceTask enables or disables maintenance mode for a given dokku application
type MaintenanceTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// State is the desired state of maintenance mode
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of maintenance mode"`
}

// MaintenanceTaskExample contains an example of a MaintenanceTask
type MaintenanceTaskExample struct {
	// Name is the task name holding the MaintenanceTask description
	Name string `yaml:"-"`

	// MaintenanceTask is the MaintenanceTask configuration
	MaintenanceTask MaintenanceTask `yaml:"dokku_maintenance"`
}

// GetName returns the name of the example
func (e MaintenanceTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the maintenance task
func (t MaintenanceTask) Doc() string {
	return "Enables or disables maintenance mode for a given dokku application"
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t MaintenanceTask) Requirements() []string {
	return []string{"dokku-maintenance plugin"}
}

// Examples returns the examples for the maintenance task
func (t MaintenanceTask) Examples() ([]Doc, error) {
	return MarshalExamples([]MaintenanceTaskExample{
		{
			Name: "Enable maintenance mode for an app",
			MaintenanceTask: MaintenanceTask{
				App: "node-js-app",
			},
		},
		{
			Name: "Disable maintenance mode for an app",
			MaintenanceTask: MaintenanceTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute enables or disables maintenance mode
func (t MaintenanceTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the MaintenanceTask would produce.
func (t MaintenanceTask) Plan() PlanResult {
	return planToggle(t.State, t.App, false, false, "maintenance:enable", "maintenance:disable", maintenanceEnabled)
}

// init registers the MaintenanceTask with the task registry
func init() {
	RegisterTask(&MaintenanceTask{})
}
