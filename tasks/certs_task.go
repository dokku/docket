package tasks

import (
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// CertsTask manages SSL certificates for a dokku app or globally
type CertsTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the certificate should be applied globally
	// via the dokku-global-cert plugin
	Global bool `required:"false" yaml:"global,omitempty"`

	// Cert is the path on the dokku server to the SSL certificate file
	Cert string `required:"false" sensitive:"true" yaml:"cert,omitempty"`

	// Key is the path on the dokku server to the SSL certificate key file
	Key string `required:"false" sensitive:"true" yaml:"key,omitempty"`

	// State is the desired state of the SSL configuration
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// CertsTaskExample contains an example of a CertsTask
type CertsTaskExample struct {
	// Name is the task name holding the CertsTask description
	Name string `yaml:"-"`

	// CertsTask is the CertsTask configuration
	CertsTask CertsTask `yaml:"dokku_certs"`
}

// GetName returns the name of the example
func (e CertsTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the certs task
func (t CertsTask) Doc() string {
	return "Manages SSL certificates for a dokku app or globally. The `cert` " +
		"and `key` fields are paths on the dokku server, so when running with " +
		"`DOKKU_HOST` set the referenced files must already exist on the " +
		"remote host - docket does not upload them."
}

// Examples returns the examples for the certs task
func (t CertsTask) Examples() ([]Doc, error) {
	return MarshalExamples([]CertsTaskExample{
		{
			Name: "Add an SSL certificate to an app",
			CertsTask: CertsTask{
				App:  "node-js-app",
				Cert: "/etc/nginx/ssl/node-js-app.crt",
				Key:  "/etc/nginx/ssl/node-js-app.key",
			},
		},
		{
			Name: "Remove an SSL certificate from an app",
			CertsTask: CertsTask{
				App:   "node-js-app",
				State: StateAbsent,
			},
		},
		{
			Name: "Add a global SSL certificate (requires the dokku-global-cert plugin)",
			CertsTask: CertsTask{
				Global: true,
				Cert:   "/etc/nginx/ssl/global.crt",
				Key:    "/etc/nginx/ssl/global.key",
			},
		},
		{
			Name: "Remove the global SSL certificate",
			CertsTask: CertsTask{
				Global: true,
				State:  StateAbsent,
			},
		},
	})
}

// Execute manages the SSL certificate
func (t CertsTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the CertsTask would produce.
func (t CertsTask) Plan() PlanResult {
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if err := validateCertsTask(t); err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if t.Cert == "" || t.Key == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'cert' and 'key' are required when state is 'present'")}
			}
			enabled, err := certsEnabled(t)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if enabled {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			target := t.App
			if t.Global {
				target = "(global)"
			}
			args := []string{"--quiet", "certs:add", t.App, t.Cert, t.Key}
			if t.Global {
				args = []string{"--quiet", "global-cert:set", t.Cert, t.Key}
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "certificate not installed",
				Mutations: []string{fmt.Sprintf("install certificate for %s", target)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			if err := validateCertsTask(t); err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			enabled, err := certsEnabled(t)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !enabled {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			target := t.App
			if t.Global {
				target = "(global)"
			}
			args := []string{"--quiet", "certs:remove", t.App}
			if t.Global {
				args = []string{"--quiet", "global-cert:remove"}
			}
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "certificate present",
				Mutations: []string{fmt.Sprintf("remove certificate for %s", target)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// validateCertsTask validates the certs task parameters
func validateCertsTask(t CertsTask) error {
	if t.Global && t.App != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if !t.Global && t.App == "" {
		return fmt.Errorf("'app' is required when 'global' is not set to true")
	}
	return nil
}

// certsEnabled checks if a certificate is currently configured for an app or globally
func certsEnabled(t CertsTask) (bool, error) {
	args := []string{"--quiet", "certs:report", t.App, "--ssl-enabled"}
	if t.Global {
		args = []string{"--quiet", "global-cert:report", "--global-cert-enabled"}
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

// init registers the CertsTask with the task registry
func init() {
	RegisterTask(&CertsTask{})
}
