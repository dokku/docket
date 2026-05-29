package tasks

import (
	"strings"

	"github.com/dokku/docket/subprocess"
)

// checksEnabled probes whether checks are enabled for an app via
// `dokku --quiet checks:report <app> --checks-disabled`. The dokku-checks
// plugin lists disabled process types here; an empty list or "none" means
// every process has checks enabled.
func checksEnabled(ctx ToggleContext) (bool, error) {
	args := []string{"--quiet", "checks:report"}
	if ctx.AllowGlobal && ctx.Global {
		args = append(args, "--global", "--checks-disabled")
	} else {
		args = append(args, ctx.App, "--checks-disabled")
	}
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return false, err
	}
	disabled := strings.TrimSpace(result.StdoutContents())
	return disabled == "" || disabled == "none", nil
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

// Plan reports the drift the ChecksToggleTask would produce.
func (t ChecksToggleTask) Plan() PlanResult {
	return planToggle(t.State, t.App, t.Global, false, "checks:enable", "checks:disable", checksEnabled)
}

// init registers the ChecksToggleTask with the task registry
func init() {
	RegisterTask(&ChecksToggleTask{})
}
