package tasks

import (
	"encoding/json"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// checksEnabled probes whether checks are enabled for an app by reading the
// `disabled-list` key from `checks:report --format json` (the global scope
// reads `global-disabled-list`). The dokku-checks plugin lists disabled
// process types there; an empty list or "none" means every process has checks
// enabled. The JSON report is used because dokku 0.38.21 renamed the
// flag-based `--checks-disabled` probe to `--checks-disabled-list`; the report
// key is stable across those versions.
func checksEnabled(ctx ToggleContext) (bool, error) {
	args := []string{"checks:report"}
	key := "disabled-list"
	if ctx.AllowGlobal && ctx.Global {
		args = append(args, "--global")
		key = "global-disabled-list"
	} else {
		args = append(args, ctx.App)
	}
	args = append(args, "--format", "json")

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return false, err
	}

	report := map[string]string{}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return false, err
	}
	disabled := strings.TrimSpace(report[key])
	return disabled == "" || disabled == "none", nil
}

// ExportApp emits a dokku_checks_toggle task only when checks are disabled
// (they are enabled by default).
func (t ChecksToggleTask) ExportApp(app string) ([]interface{}, error) {
	enabled, err := checksEnabled(ToggleContext{App: app})
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, nil
	}
	return []interface{}{ChecksToggleTask{App: app, State: StateAbsent}}, nil
}

// ChecksToggleTask enables or disables the checks plugin for a given dokku application
type ChecksToggleTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Global is a flag indicating if the checks plugin should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the checks plugin should be applied globally"`

	// State is the desired state of the checks plugin
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the checks plugin"`
}

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
func (t ChecksToggleTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the ChecksToggleTask's inputs without contacting the server.
func (t ChecksToggleTask) Validate() error {
	return validateToggleInput(t.App, t.Global, false)
}

// Plan reports the drift the ChecksToggleTask would produce.
func (t ChecksToggleTask) Plan() PlanResult {
	return planToggle(t.State, t.App, t.Global, false, "checks:enable", "checks:disable", checksEnabled)
}

// init registers the ChecksToggleTask with the task registry
func init() {
	RegisterTask(&ChecksToggleTask{})
}
