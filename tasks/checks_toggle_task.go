package tasks

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// checksEnabled probes whether checks are enabled for an app by reading the
// `disabled-list` key from `checks:report <app> --format json`. The
// dokku-checks plugin lists disabled process types there; an empty list or
// "none" means every process has checks enabled. The JSON report is used
// because dokku 0.38.21 renamed the flag-based `--checks-disabled` probe to
// `--checks-disabled-list`; the report key is stable across those versions.
func checksEnabled(ctx context.Context, tc ToggleContext) (bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"checks:report", tc.App, "--format", "json"},
	})
	if err != nil {
		return false, err
	}

	report := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return false, err
	}
	disabled := strings.TrimSpace(report["disabled-list"])
	return disabled == "" || disabled == "none", nil
}

// ExportApp emits a dokku_checks_toggle task only when checks are disabled
// (they are enabled by default).
func (t ChecksToggleTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	enabled, err := checksEnabled(ctx, ToggleContext{App: app})
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, nil
	}
	return []interface{}{ChecksToggleTask{App: app, State: StateAbsent}}, nil
}

// ChecksToggleTask enables or disables the checks plugin for a given dokku application
type ChecksToggleTask ToggleFields

// ChecksToggleTaskExample contains an example of a ChecksToggleTask
type ChecksToggleTaskExample struct {
	// Name is the task name holding the ChecksToggleTask description
	Name string `yaml:"-"`

	// ChecksToggleTask is the ChecksToggleTask configuration
	ChecksToggleTask ChecksToggleTask `yaml:"dokku_checks_toggle"`
}

// GetName returns the name of the example
func (e ChecksToggleTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the checks toggle task
func (t ChecksToggleTask) Doc() string {
	return "Enables or disables the checks plugin for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t ChecksToggleTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t ChecksToggleTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the checks toggle task
func (t ChecksToggleTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ChecksToggleTaskExample{
		{
			Name: "Disable the zero downtime deployment",
			ChecksToggleTask: ChecksToggleTask{
				App:   "hello-world",
				State: "absent",
			},
		},
		{
			Name: "Re-enable the zero downtime deployment (enabled by default)",
			ChecksToggleTask: ChecksToggleTask{
				App:   "hello-world",
				State: "present",
			},
		},
	})
}

// Execute enables or disables the checks plugin
func (t ChecksToggleTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Plan reports the drift the ChecksToggleTask would produce.
func (t ChecksToggleTask) Plan(ctx context.Context) PlanResult {
	return planToggle(ctx, t.State, t.App, "checks:enable", "checks:disable", checksEnabled)
}

// init registers the ChecksToggleTask with the task registry
func init() {
	RegisterTask(&ChecksToggleTask{})
}
