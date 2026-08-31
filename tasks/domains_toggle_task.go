package tasks

import (
	"context"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// domainsEnabled probes whether the domains plugin is enabled for an app
// via `dokku --quiet domains:report <app> --domains-app-enabled`.
func domainsEnabled(ctx context.Context, tc ToggleContext) (bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "domains:report", tc.App, "--domains-app-enabled"},
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// ExportApp emits a dokku_domains_toggle task only when the domains plugin is
// disabled for the app (it is enabled by default).
func (t DomainsToggleTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	enabled, err := domainsEnabled(ctx, ToggleContext{App: app})
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, nil
	}
	return []interface{}{DomainsToggleTask{App: app, State: StateAbsent}}, nil
}

// DomainsToggleTask enables or disables the domains plugin for a given dokku application
type DomainsToggleTask ToggleFields

// DomainsToggleTaskExample contains an example of a DomainsToggleTask
type DomainsToggleTaskExample struct {
	// Name is the task name holding the DomainsToggleTask description
	Name string `yaml:"-"`

	// DomainsToggleTask is the DomainsToggleTask configuration
	DomainsToggleTask DomainsToggleTask `yaml:"dokku_domains_toggle"`
}

// GetName returns the name of the example
func (e DomainsToggleTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the domains toggle task
func (t DomainsToggleTask) Doc() string {
	return "Enables or disables the domains plugin for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t DomainsToggleTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t DomainsToggleTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Examples returns the examples for the domains toggle task
func (t DomainsToggleTask) Examples() ([]Doc, error) {
	return MarshalExamples([]DomainsToggleTaskExample{
		{
			Name: "Enable the domains plugin for an app",
			DomainsToggleTask: DomainsToggleTask{
				App: "node-js-app",
			},
		},
		{
			Name: "Disable the domains plugin for an app",
			DomainsToggleTask: DomainsToggleTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute enables or disables the domains plugin
func (t DomainsToggleTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Plan reports the drift the DomainsToggleTask would produce.
func (t DomainsToggleTask) Plan(ctx context.Context) PlanResult {
	return planToggle(ctx, t.State, t.App, "domains:enable", "domains:disable", domainsEnabled)
}

// init registers the DomainsToggleTask with the task registry
func init() {
	RegisterTask(&DomainsToggleTask{})
}
