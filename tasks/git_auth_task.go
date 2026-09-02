package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// GitAuthTask manages netrc credentials for a git host via dokku git:auth.
//
// The netrc file itself is unreadable (mode 0600 under $DOKKU_ROOT) and there
// is no report that dumps it, but idempotency does not need one: dokku's
// git:auth-status compares the stored entry against credentials it is handed
// and answers with its exit code, which is the only question Plan() asks. See
// gitAuthMatches for the two shapes that answers.
//
// The password never reaches argv on either path. Both git:auth and
// git:auth-status read it from stdin when the username is supplied and the
// password argument is omitted, so it stays out of the plan output, the trace
// log, and the process table on the dokku host.
type GitAuthTask struct {
	// Host is the git server hostname (e.g. github.com)
	Host string `required:"true" identity:"key" yaml:"host" description:"Git server hostname (e.g. github.com)"`

	// Username is the netrc username. Required when state is present.
	Username string `required:"false" yaml:"username,omitempty" description:"Netrc username. Required when state is present."`

	// Password is the netrc password. Required when state is present.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"Netrc password. Required when state is present. Must not contain a newline."`

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
//
// Probing and exporting ask different questions of the same command:
// git:auth-status confirms credentials the recipe already holds, but it cannot
// enumerate the hosts with an entry and never reveals a stored username, so
// there is still nothing for an exporter to reconstruct.
func (t GitAuthTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportUnsupported, Caveat: "netrc credentials are write-only and cannot be read back"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t GitAuthTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
	// dokku reads the password with `read -r`, which stops at the first
	// newline, and a .netrc entry is a single line either way. Rejecting it
	// here is the difference between a clear error and a task that silently
	// writes a truncated password and then never converges.
	if strings.ContainsAny(t.Password, "\r\n") {
		return fmt.Errorf("'password' must not contain a newline")
	}
	return nil
}

// Plan reports the drift the GitAuthTask would produce.
func (t GitAuthTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			matches, err := gitAuthMatches(ctx, t.Host, t.Username, t.Password)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if matches {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "git:auth", t.Host, t.Username},
				Stdin:   strings.NewReader(t.Password),
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusModify,
				Reason:    "netrc entry does not match",
				Mutations: []string{"git:auth " + t.Host + " as " + t.Username},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			cleared, err := gitAuthMatches(ctx, t.Host, "", "")
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if cleared {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "git:auth", t.Host},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "netrc entry present",
				Mutations: []string{"git:auth " + t.Host + " (clear)"},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// gitAuthMatches reports whether the netrc entry for host already matches the
// requested state. git:auth-status is a comparator rather than a dump: it
// prints nothing and exits 0 when the stored entry equals what it was handed.
// Handed no username it answers the absent-state question instead - exit 0
// when the host has no entry at all.
//
// The password goes over stdin, where dokku's fn-git-auth-read-password picks
// it up, so it never reaches the argv of the dokku process on the server.
//
// Returns (false, err) when the probe could not run - a transport failure, a
// missing dokku binary, or a cancellation; (true, nil) when the server is
// already in the requested state; (false, nil) otherwise. A "no" is one answer
// and not two: git:auth-status exits non-zero both for a host with no entry
// and for a host whose entry differs, so the present-state plan reports a
// modify rather than telling a create apart from a replacement. Tracking
// distinct exit codes upstream in dokku/dokku#8995.
func gitAuthMatches(ctx context.Context, host, username, password string) (bool, error) {
	input := subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "git:auth-status", host},
	}
	if username != "" {
		input.Args = append(input.Args, username)
		input.Stdin = strings.NewReader(password)
	}
	return subprocess.Probe(ctx, input)
}

// init registers the GitAuthTask with the task registry
func init() {
	RegisterTask(&GitAuthTask{})
}
