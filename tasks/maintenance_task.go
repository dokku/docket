package tasks

import (
	"encoding/json"

	"github.com/dokku/docket/subprocess"
)

// maintenanceEnabled probes whether maintenance mode is enabled for an app by
// reading the `enabled` key from `maintenance:report --format json` (the plugin
// strips the `maintenance-` prefix from JSON report keys). A probe failure
// returns an error, which planToggle treats as drift.
func maintenanceEnabled(ctx ToggleContext) (bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"maintenance:report", ctx.App, "--format", "json"},
	})
	if err != nil {
		return false, err
	}
	var report struct {
		Enabled string `json:"enabled"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return false, err
	}
	return report.Enabled == "true", nil
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

// ExportSupport reports how docket export handles this task.
func (t MaintenanceTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
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
