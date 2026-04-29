package tasks

import (
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// RegistryAuthTask manages docker registry authentication for a dokku application or globally
type RegistryAuthTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" yaml:"app"`

	// Global is a flag indicating if the registry credential should be applied globally
	Global bool `required:"false" yaml:"global,omitempty"`

	// Server is the docker registry hostname (e.g. docker.io, ghcr.io)
	Server string `required:"true" yaml:"server"`

	// Username is the registry username (required when state is present)
	Username string `required:"false" yaml:"username,omitempty"`

	// Password is the registry password (required when state is present)
	// The value is fed to dokku via --password-stdin and never appears on argv
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty"`

	// State is the desired state of the registry credential
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// RegistryAuthTaskExample contains an example of a RegistryAuthTask
type RegistryAuthTaskExample struct {
	// Name is the task name holding the RegistryAuthTask description
	Name string `yaml:"-"`

	// RegistryAuthTask is the RegistryAuthTask configuration
	RegistryAuthTask RegistryAuthTask `yaml:"dokku_registry_auth"`
}

// GetName returns the name of the example
func (e RegistryAuthTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the registry auth task
func (t RegistryAuthTask) Doc() string {
	return "Manages docker registry authentication for a dokku application or globally"
}

// Examples returns the examples for the registry auth task
func (t RegistryAuthTask) Examples() ([]Doc, error) {
	return MarshalExamples([]RegistryAuthTaskExample{
		{
			Name: "Log in to a registry for an app",
			RegistryAuthTask: RegistryAuthTask{
				App:      "node-js-app",
				Server:   "ghcr.io",
				Username: "deploy-bot",
				Password: "ghp_examplepat",
			},
		},
		{
			Name: "Log in to a registry globally",
			RegistryAuthTask: RegistryAuthTask{
				Global:   true,
				Server:   "docker.io",
				Username: "deploy-bot",
				Password: "examplepassword",
			},
		},
		{
			Name: "Log out from a registry for an app",
			RegistryAuthTask: RegistryAuthTask{
				App:    "node-js-app",
				Server: "ghcr.io",
				State:  StateAbsent,
			},
		},
		{
			Name: "Log out from a registry globally",
			RegistryAuthTask: RegistryAuthTask{
				Global: true,
				Server: "docker.io",
				State:  StateAbsent,
			},
		},
	})
}

// Execute manages the registry authentication
func (t RegistryAuthTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the RegistryAuthTask would produce. dokku registry
// plugins do not expose a probe for current login state, so the plan
// reports drift unconditionally.
func (t RegistryAuthTask) Plan() PlanResult {
	if err := validateRegistryAuthTask(t); err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	target := t.App
	if t.Global {
		target = "(global)"
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if t.Username == "" || t.Password == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'username' and 'password' are required when state is 'present'")}
			}
			args := []string{"--quiet", "registry:login", "--password-stdin"}
			if t.Global {
				args = append(args, "--global")
			} else {
				args = append(args, t.App)
			}
			args = append(args, t.Server, t.Username)
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    args,
				Stdin:   strings.NewReader(t.Password),
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "registry login state not probed",
				Mutations: []string{fmt.Sprintf("registry:login %s %s as %s", target, t.Server, t.Username)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			args := []string{"--quiet", "registry:logout"}
			if t.Global {
				args = append(args, "--global")
			} else {
				args = append(args, t.App)
			}
			args = append(args, t.Server)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "registry login state not probed",
				Mutations: []string{fmt.Sprintf("registry:logout %s %s", target, t.Server)},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// validateRegistryAuthTask validates the registry auth task parameters
func validateRegistryAuthTask(t RegistryAuthTask) error {
	if t.Global && t.App != "" {
		return fmt.Errorf("'app' must not be set when 'global' is set to true")
	}
	if !t.Global && t.App == "" {
		return fmt.Errorf("'app' is required when 'global' is not set to true")
	}
	if t.Server == "" {
		return fmt.Errorf("'server' is required")
	}
	return nil
}

// registryLogin runs `dokku registry:login [--global|<app>] <server> <username> --password-stdin`
// piping the password via stdin so it does not appear on the process argument list
func registryLogin(t RegistryAuthTask) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   StateAbsent,
	}

	if err := validateRegistryAuthTask(t); err != nil {
		state.Error = err
		return state
	}
	if t.Username == "" || t.Password == "" {
		state.Error = fmt.Errorf("'username' and 'password' are required when state is 'present'")
		return state
	}

	args := []string{"--quiet", "registry:login", "--password-stdin"}
	if t.Global {
		args = append(args, "--global")
	} else {
		args = append(args, t.App)
	}
	args = append(args, t.Server, t.Username)

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
		Stdin:   strings.NewReader(t.Password),
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = StatePresent
	return state
}

// registryLogout runs `dokku registry:logout [--global|<app>] <server>`
func registryLogout(t RegistryAuthTask) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   StatePresent,
	}

	if err := validateRegistryAuthTask(t); err != nil {
		state.Error = err
		return state
	}

	args := []string{"--quiet", "registry:logout"}
	if t.Global {
		args = append(args, "--global")
	} else {
		args = append(args, t.App)
	}
	args = append(args, t.Server)

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    args,
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = StateAbsent
	return state
}

// init registers the RegistryAuthTask with the task registry
func init() {
	RegisterTask(&RegistryAuthTask{})
}
