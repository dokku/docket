package tasks

import (
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// GitAuthTask manages netrc credentials for a git host via dokku git:auth.
//
// Idempotency is intentionally skipped here because dokku has no public way
// to query the current netrc state (the file lives at $DOKKU_ROOT/.netrc with
// mode 0600). Tracking upstream support in dokku/dokku#8504; once a
// no-change exit code is available, this task should switch to using it
// instead of always reporting Changed=true.
type GitAuthTask struct {
	// Host is the git server hostname (e.g. github.com)
	Host string `required:"true" yaml:"host"`

	// Username is the netrc username. Required when state is present.
	Username string `required:"false" yaml:"username,omitempty"`

	// Password is the netrc password. Required when state is present.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty"`

	// State is the desired state of the netrc entry
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent"`
}

// GitAuthTaskExample contains an example of a GitAuthTask
type GitAuthTaskExample struct {
	// Name is the task name holding the GitAuthTask description
	Name string `yaml:"-"`

	// GitAuthTask is the GitAuthTask configuration
	GitAuthTask GitAuthTask `yaml:"dokku_git_auth"`
}

// GetName returns the name of the example
func (e GitAuthTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the git auth task
func (t GitAuthTask) Doc() string {
	return "Manages netrc credentials for a git host"
}

// Examples returns the examples for the git auth task
func (t GitAuthTask) Examples() ([]Doc, error) {
	return MarshalExamples([]GitAuthTaskExample{
		{
			Name: "Configure netrc credentials for a git host",
			GitAuthTask: GitAuthTask{
				Host:     "github.com",
				Username: "deploy-bot",
				Password: "ghp_examplepat",
			},
		},
		{
			Name: "Remove netrc credentials for a git host",
			GitAuthTask: GitAuthTask{
				Host:  "github.com",
				State: StateAbsent,
			},
		},
	})
}

// Execute manages the netrc entry for a host
func (t GitAuthTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Plan reports the drift the GitAuthTask would produce. dokku has no public
// way to query netrc state, so the plan reports drift unconditionally.
func (t GitAuthTask) Plan() PlanResult {
	if t.Host == "" {
		return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'host' is required")}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			if t.Username == "" || t.Password == "" {
				return PlanResult{Status: PlanStatusError, Error: fmt.Errorf("'username' and 'password' are required when state is 'present'")}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "git:auth", t.Host, t.Username, t.Password},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "netrc state not probed",
				Mutations: []string{"git:auth " + t.Host + " " + t.Username + " " + t.Password},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "git:auth", t.Host},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "netrc state not probed",
				Mutations: []string{"git:auth " + t.Host + " (clear)"},
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// setGitAuth runs `dokku git:auth HOST USERNAME PASSWORD`.
func setGitAuth(t GitAuthTask) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   StateAbsent,
	}

	if t.Host == "" {
		state.Error = fmt.Errorf("'host' is required")
		return state
	}
	if t.Username == "" || t.Password == "" {
		state.Error = fmt.Errorf("'username' and 'password' are required when state is 'present'")
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "git:auth", t.Host, t.Username, t.Password},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = StatePresent
	return state
}

// unsetGitAuth runs `dokku git:auth HOST` (no username) which removes the entry.
func unsetGitAuth(t GitAuthTask) TaskOutputState {
	state := TaskOutputState{
		Changed: false,
		State:   StatePresent,
	}

	if t.Host == "" {
		state.Error = fmt.Errorf("'host' is required")
		return state
	}

	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "git:auth", t.Host},
	})
	state.Commands = append(state.Commands, result.Command)
	if err != nil {
		return TaskOutputErrorFromExec(state, err, result)
	}

	state.Changed = true
	state.State = StateAbsent
	return state
}

// init registers the GitAuthTask with the task registry
func init() {
	RegisterTask(&GitAuthTask{})
}
