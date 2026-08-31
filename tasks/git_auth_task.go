package tasks

import (
	"context"
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
	Host string `required:"true" identity:"key" yaml:"host" description:"Git server hostname (e.g. github.com)"`

	// Username is the netrc username. Required when state is present.
	Username string `required:"false" yaml:"username,omitempty" description:"Netrc username. Required when state is present."`

	// Password is the netrc password. Required when state is present.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"Netrc password. Required when state is present."`

	// State is the desired state of the netrc entry
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the netrc entry"`
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

// ExportSupport reports how docket export handles this task.
func (t GitAuthTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: "netrc credentials are write-only and cannot be read back"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t GitAuthTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeUnsupported, Caveat: "netrc state has no read command, so the task plans as drift on every run"}
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
func (t GitAuthTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the GitAuthTask's inputs without contacting the server.
func (t GitAuthTask) Validate() error {
	if t.Host == "" {
		return fmt.Errorf("'host' is required")
	}
	if t.State == StatePresent && (t.Username == "" || t.Password == "") {
		return fmt.Errorf("'username' and 'password' are required when state is 'present'")
	}
	return nil
}

// Plan reports the drift the GitAuthTask would produce. dokku has no public
// way to query netrc state, so the plan reports drift unconditionally.
func (t GitAuthTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "git:auth", t.Host, t.Username, t.Password},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "netrc state not probed",
				Mutations: []string{"git:auth " + t.Host + " " + t.Username + " " + t.Password},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
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
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// init registers the GitAuthTask with the task registry
func init() {
	RegisterTask(&GitAuthTask{})
}
