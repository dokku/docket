package tasks

import (
	"strings"

	"github.com/dokku/docket/subprocess"
)

// proxyEnabled probes whether the proxy is enabled for an app via
// `dokku --quiet proxy:report <app> --proxy-enabled`. Output is "true"/"false".
func proxyEnabled(ctx ToggleContext) (bool, error) {
	args := []string{"--quiet", "proxy:report"}
	if ctx.AllowGlobal && ctx.Global {
		args = append(args, "--global", "--proxy-enabled")
	} else {
		args = append(args, ctx.App, "--proxy-enabled")
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

// ProxyToggleTask manages the proxy for a given dokku application
type ProxyToggleTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Global is a flag indicating if the proxy should be applied globally
	Global bool `required:"false" yaml:"global,omitempty" description:"Flag indicating if the proxy should be applied globally"`

	// State is the desired state of the proxy
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the proxy"`
}

// ProxyToggleTaskExample contains an example of a ProxyToggleTask
type ProxyToggleTaskExample struct {
	// Name is the task name holding the ProxyToggleTask description
	Name string `yaml:"-"`

	// ProxyToggleTask is the ProxyToggleTask configuration
	ProxyToggleTask ProxyToggleTask `yaml:"dokku_proxy_toggle"`
}

// GetName returns the name of the example
func (e ProxyToggleTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the proxy toggle task
func (t ProxyToggleTask) Doc() string {
	return "Enables or disables the proxy plugin for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t ProxyToggleTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// Examples returns the examples for the proxy toggle task
func (t ProxyToggleTask) Examples() ([]Doc, error) {
	return MarshalExamples([]ProxyToggleTaskExample{
		{
			Name: "Enable the proxy for an app",
			ProxyToggleTask: ProxyToggleTask{
				App: "node-js-app",
			},
		},
		{
			Name: "Disable the proxy for an app",
			ProxyToggleTask: ProxyToggleTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
	})
}

// Execute enables or disables the proxy
func (t ProxyToggleTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the ProxyToggleTask would produce.
func (t ProxyToggleTask) Plan() PlanResult {
	return planToggle(t.State, t.App, t.Global, false, "proxy:enable", "proxy:disable", proxyEnabled)
}

// init registers the ProxyToggleTask with the task registry
func init() {
	RegisterTask(&ProxyToggleTask{})
}
