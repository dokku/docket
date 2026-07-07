package tasks

import (
	"strings"

	"github.com/dokku/docket/subprocess"
)

// domainsEnabled probes whether the domains plugin is enabled for an app
// via `dokku --quiet domains:report <app> --domains-app-enabled`, or for
// the global scope via `--domains-global-enabled`.
func domainsEnabled(ctx ToggleContext) (bool, error) {
	args := []string{"--quiet", "domains:report"}
	if ctx.AllowGlobal && ctx.Global {
		args = append(args, "--global", "--domains-global-enabled")
	} else {
		args = append(args, ctx.App, "--domains-app-enabled")
	}
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(result.StdoutContents()) == "true", nil
}

// ExportApp emits a dokku_domains_toggle task only when the domains plugin is
// disabled for the app (it is enabled by default).
func (t DomainsToggleTask) ExportApp(app string) ([]interface{}, error) {
	enabled, err := domainsEnabled(ToggleContext{App: app})
	if err != nil {
		return nil, err
	}
	if enabled {
		return nil, nil
	}
	return []interface{}{DomainsToggleTask{App: app, State: StateAbsent}}, nil
}

// DomainsToggleTask enables or disables the domains plugin for a given dokku application
type DomainsToggleTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Global is a flag indicating if the domains plugin should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the domains plugin should be applied globally"`

	// State is the desired state of the domains plugin
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the domains plugin"`
}

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
func (t DomainsToggleTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the DomainsToggleTask's inputs without contacting the server.
func (t DomainsToggleTask) Validate() error {
	return validateToggleInput(t.App, t.Global, false)
}

// Plan reports the drift the DomainsToggleTask would produce.
func (t DomainsToggleTask) Plan() PlanResult {
	return planToggle(t.State, t.App, t.Global, false, "domains:enable", "domains:disable", domainsEnabled)
}

// init registers the DomainsToggleTask with the task registry
func init() {
	RegisterTask(&DomainsToggleTask{})
}
