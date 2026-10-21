package tasks

import (
	"context"
	"encoding/json"

	"github.com/dokku/docket/internal/subprocess"
)

// maintenanceEnabled probes whether maintenance mode is enabled for an app by
// reading the `enabled` key from `maintenance:report --format json` (the plugin
// strips the `maintenance-` prefix from JSON report keys). A probe failure
// returns an error, which planToggle treats as drift unless it is an SSH
// transport failure, which surfaces as a plan error.
func maintenanceEnabled(ctx context.Context, tc ToggleContext) (bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"maintenance:report", tc.App, "--format", "json"},
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

// ExportApp emits a dokku_maintenance task only when maintenance mode is on
// (it is off by default, so a normal app needs no task).
func (t MaintenanceTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	on, err := maintenanceEnabled(ctx, ToggleContext{App: app})
	if err != nil {
		return nil, err
	}
	if !on {
		return nil, nil
	}
	return []interface{}{MaintenanceTask{App: app, State: StatePresent}}, nil
}

// MaintenanceTask enables or disables maintenance mode for a given dokku application
type MaintenanceTask ToggleFields

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

// ProbeSupport reports whether Plan() can read this task's current state.
func (t MaintenanceTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// The probe is `maintenance:report`. 0.10.2 is the plugin.toml version of the
// maintenance release the integration suite installs; no older report shape
// is recorded here. 0.38.31 is the Dokku that suite runs.
func (t MaintenanceTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			Name:    "maintenance",
			Minimum: "0.10.2",
			Text:    "dokku-maintenance plugin >= 0.10.2",
		}},
	}
}

// examples returns the examples for the maintenance task
func (t MaintenanceTask) examples() ([]Doc, error) {
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
func (t MaintenanceTask) Execute(ctx context.Context) TaskOutputState {
	return executeTask(ctx, t)
}

// Plan reports the drift the MaintenanceTask would produce.
func (t MaintenanceTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	return planToggle(ctx, t, func(f ToggleFields) Task { return MaintenanceTask(f) }, t.State, t.App, "maintenance:enable", "maintenance:disable", maintenanceEnabled)
}

// init registers the MaintenanceTask with the task registry
func init() {
	RegisterTask(&MaintenanceTask{})
}
