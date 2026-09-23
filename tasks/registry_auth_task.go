package tasks

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// registry:auth-status exit codes. dokku reports the comparison through the
// exit code alone and prints nothing when the answer is definitive, so these
// are the whole protocol. 0 through 3 are the codes git:auth-status uses for
// the equivalent netrc states; 4 is registry-only, and 20 is dokku's usual
// "no such app".
const (
	// registryAuthMatch means the stored credential matches the requested state
	registryAuthMatch = 0

	// registryAuthMissing means no credential is stored for the server
	registryAuthMissing = 1

	// registryAuthDiffers means a credential is stored but does not match
	registryAuthDiffers = 2

	// registryAuthInvalidArguments means the state could not be checked
	registryAuthInvalidArguments = 3

	// registryAuthUnknown means a credential is stored but its secret is
	// unreadable, so the comparison cannot be made either way
	registryAuthUnknown = 4

	// registryAuthNoSuchApp is dokku's exit code for an app that does not exist
	registryAuthNoSuchApp = 20

	// registryAuthNoSuchCommand is not a dokku exit code. A dokku older than
	// 0.38.29 has no registry:auth-status and reports that by exiting 1, which
	// collides exactly with "no credential is stored" - so the probe reads the
	// reason off stderr and substitutes a code no answer uses, leaving the
	// collision out of the mapping.
	registryAuthNoSuchCommand = -1
)

// RegistryAuthTask manages docker registry authentication for a dokku
// application or globally.
//
// The stored credential is not readable - it lives in a docker config.json
// under $DOKKU_ROOT, and registry:report exposes which servers have one but
// never who it belongs to - yet idempotency does not need it to be. dokku's
// registry:auth-status compares a credential it is handed against the stored
// one and answers with its exit code, which is the only question Plan() asks.
// See registryAuthStatus for the shapes that answers. It needs dokku 0.38.29
// (dokku/dokku#8993); below that the subcommand does not exist and the task
// falls back to reporting drift on every run, as it did before.
//
// The password never reaches argv on either path. Both registry:login and
// registry:auth-status read it from stdin under --password-stdin, so it stays
// out of the plan output, the trace log, and the process table on the dokku
// host.
//
// Unlike dokku_git_auth this task needs no newline rule on the password.
// dokku reads stdin with io.ReadAll and trims it on both the login and the
// status path, so whatever survives that round trip compares equal to itself.
// What it does reject is a password that trims away to nothing, which dokku
// would answer with an invalid-arguments exit rather than a comparison.
type RegistryAuthTask struct {
	// App is the name of the app. Required if Global is false.
	App string `required:"false" identity:"key" yaml:"app" description:"Name of the app. Required if Global is false."`

	// Global is a flag indicating if the registry credential should be applied globally
	Global bool `required:"false" identity:"key" yaml:"global,omitempty" description:"Flag indicating if the registry credential should be applied globally"`

	// Server is the docker registry hostname (e.g. docker.io, ghcr.io).
	// dokku normalises hub.docker.com and docker.com to docker.io, and docker
	// stores Docker Hub under its index server address, on both the login and
	// the status path - so the value is passed through exactly as written.
	Server string `required:"true" identity:"key" yaml:"server" description:"Docker registry hostname (e.g. docker.io, ghcr.io)"`

	// Username is the registry username (required when state is present)
	Username string `required:"false" yaml:"username,omitempty" description:"Registry username (required when state is present)"`

	// Password is the registry password (required when state is present)
	// The value is fed to dokku via --password-stdin and never appears on argv
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"Registry password (required when state is present)"`

	// State is the desired state of the registry credential
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the registry credential"`
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

// ExportSupport reports how docket export handles this task.
//
// registry:report names the servers an app or the global config holds a
// credential for, which is enough to reconstruct which logins a server needs.
// The credential itself is not readable in either half: the password is
// write-only, and dokku exposes no username field either, so both become
// required inputs the operator supplies at apply time.
func (t RegistryAuthTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "registry:report names the servers a credential exists for, so the logins a server needs are reconstructed; the username and password behind each one are not readable and are lifted into required inputs the caller supplies before apply"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t RegistryAuthTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
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
func (t RegistryAuthTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the RegistryAuthTask's inputs without contacting the server.
func (t RegistryAuthTask) Validate() error {
	if err := validateRegistryAuthTask(t); err != nil {
		return err
	}
	if t.State == StatePresent && (t.Username == "" || t.Password == "") {
		return fmt.Errorf("'username' and 'password' are required when state is 'present'")
	}
	// dokku trims the password it reads from stdin, so one that is all
	// whitespace arrives empty and is answered with an invalid-arguments exit
	// rather than a comparison. Rejecting it here turns that into a clear
	// error before anything reaches the server.
	if t.State == StatePresent && strings.TrimSpace(t.Password) == "" {
		return fmt.Errorf("'password' must not be entirely whitespace")
	}
	return nil
}

// Plan reports the drift the RegistryAuthTask would produce.
func (t RegistryAuthTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	target := t.App
	if t.Global {
		target = "(global)"
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			code, message, err := registryAuthStatus(ctx, t, t.Username, t.Password)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if code == registryAuthMatch {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			if code == registryAuthInvalidArguments {
				return registryAuthArgumentError(t, message)
			}

			status, reason := PlanStatusModify, "registry credential does not match"
			var warnings []PlanWarning
			switch code {
			case registryAuthDiffers:
				// the default above is this case's own answer
			case registryAuthMissing, registryAuthNoSuchApp:
				// An app that does not exist yet holds no credential, and the
				// login below creates one exactly as it would for a server
				// that has none.
				status, reason = PlanStatusCreate, "no registry credential stored"
			case registryAuthUnknown:
				// Saying it does not match would overstate what dokku answered:
				// it found a credential and could not read it.
				reason = "registry credential could not be compared"
				warnings = append(warnings, registryAuthIndeterminate(t, message))
			default:
				// An exit code registry:auth-status does not define, which is
				// also what a dokku without the subcommand reports.
				reason = "registry login state not probed"
				warnings = append(warnings, registryAuthIndeterminate(t, message))
			}

			args := []string{"--quiet", "registry:login", "--password-stdin"}
			args = append(args, registryAuthScopeArgs(t)...)
			args = append(args, t.Server, t.Username)
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    args,
				Stdin:   strings.NewReader(t.Password),
			}}
			priorState := StatePresent
			if status == PlanStatusCreate {
				priorState = StateAbsent
			}
			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("registry:login %s %s as %s", target, t.Server, t.Username)},
				Commands:  resolveCommands(ctx, inputs),
				Warnings:  warnings,
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: priorState}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			code, message, err := registryAuthStatus(ctx, t, "", "")
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			// registry:logout verifies the app name, so planning a logout for
			// an app that does not exist guarantees a failure at apply. It can
			// hold no credential either way, which is the state asked for.
			if code == registryAuthMatch || code == registryAuthNoSuchApp {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			if code == registryAuthInvalidArguments {
				return registryAuthArgumentError(t, message)
			}

			reason := "registry credential present"
			var warnings []PlanWarning
			switch code {
			case registryAuthDiffers:
				// the only definitive answer this question has left
			case registryAuthUnknown:
				reason = "registry credential could not be compared"
				warnings = append(warnings, registryAuthIndeterminate(t, message))
			default:
				reason = "registry login state not probed"
				warnings = append(warnings, registryAuthIndeterminate(t, message))
			}

			args := []string{"--quiet", "registry:logout"}
			args = append(args, registryAuthScopeArgs(t)...)
			args = append(args, t.Server)
			inputs := []subprocess.ExecCommandInput{{Command: "dokku", Args: args}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    reason,
				Mutations: []string{fmt.Sprintf("registry:logout %s %s", target, t.Server)},
				Commands:  resolveCommands(ctx, inputs),
				Warnings:  warnings,
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// registryAuthScopeArgs returns the argument that selects the credential
// store: an app name, or --global. registry:auth-status requires the flag
// where registry:login would also accept the deprecated implicit form, so
// both commands are built from this one spelling.
func registryAuthScopeArgs(t RegistryAuthTask) []string {
	if t.Global {
		return []string{"--global"}
	}
	return []string{t.App}
}

// registryAuthStatus asks dokku how the stored credential compares to the
// requested state. registry:auth-status is a comparator rather than a dump: it
// prints nothing and exits 0 when the stored credential equals what it was
// handed. Handed no username it answers the absent-state question instead -
// exit 0 when the server has no credential at all.
//
// The password goes over stdin under --password-stdin, so it never reaches the
// argv of the dokku process on the server.
//
// Returns the exit code and, when dokku explained itself on stderr, that
// message. An error means the probe never produced an answer - a transport
// failure, a missing dokku binary, or a cancellation - and the caller should
// surface it rather than reading it as drift.
func registryAuthStatus(ctx context.Context, t RegistryAuthTask, username, password string) (int, string, error) {
	input := subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "registry:auth-status"},
	}
	if username != "" {
		input.Args = append(input.Args, "--password-stdin")
	}
	input.Args = append(input.Args, registryAuthScopeArgs(t)...)
	input.Args = append(input.Args, t.Server)
	if username != "" {
		input.Args = append(input.Args, username)
		input.Stdin = strings.NewReader(password)
	}

	result, err := subprocess.ProbeCode(ctx, input)
	if err != nil {
		return 0, "", err
	}
	message := registryAuthMessage(result.StderrContents())
	if strings.Contains(message, "is not a dokku command") {
		return registryAuthNoSuchCommand, message, nil
	}
	return result.ExitCode, message, nil
}

// registryAuthMessage reduces dokku's stderr to the one sentence a warning
// should carry: the first non-empty line, with the ` !     ` prefix dokku puts
// in front of a failure stripped off. An unknown command prints a second line
// pointing at `dokku help`, which is noise here.
func registryAuthMessage(stderr string) string {
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "!"))
		if line != "" {
			return line
		}
	}
	return ""
}

// registryAuthArgumentError turns an invalid-arguments exit into a plan
// error. Every argument is docket's own and the recipe-level ones are checked
// offline, so this means dokku could not answer the question that was asked -
// an app name it rejects, most likely. Reporting it is better than logging in
// on the strength of an answer that was never given.
func registryAuthArgumentError(t RegistryAuthTask, message string) PlanResult {
	if message == "" {
		message = "dokku could not check the registry credential"
	}
	return PlanResult{
		Status:   PlanStatusError,
		Error:    fmt.Errorf("dokku registry:auth-status rejected the probe for %s: %s", t.Server, message),
		Stderr:   message,
		ExitCode: registryAuthInvalidArguments,
	}
}

// registryAuthIndeterminate builds the warning for a probe that ran and could
// not answer. A docker credential helper or an identity token holding the
// secret is the documented case; a dokku older than 0.38.29, where the
// subcommand does not exist at all, lands here too. Either way the task applies
// rather than assuming a match, and never settles on that server - which is the
// part an operator watching a run report drift forever needs to know.
func registryAuthIndeterminate(t RegistryAuthTask, message string) PlanWarning {
	if message == "" {
		message = "dokku gave no reason"
	}
	scope := fmt.Sprintf("the global credential for %s", t.Server)
	if !t.Global {
		scope = fmt.Sprintf("%s's credential for %s", t.App, t.Server)
	}
	return PlanWarning{
		Reason:  WarnReasonProbeIndeterminate,
		Message: fmt.Sprintf("%s could not be compared: %s", scope, message),
	}
}

// ExportApp reconstructs the app's own registry logins. registry:report names
// the servers the app holds a credential for and nothing about the credential
// itself, so each emitted task carries the server and leaves the username and
// password for processRegistryAuth to lift into inputs.
func (t RegistryAuthTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	return exportRegistryAuth(ctx, app, false, func(server string) interface{} {
		return RegistryAuthTask{App: app, Server: server, State: StatePresent}
	})
}

// ExportGlobal reconstructs the global registry logins. An app with a
// credential of its own shadows the global config rather than merging with it,
// so the two scopes read different keys and never emit the same server twice.
func (t RegistryAuthTask) ExportGlobal(ctx context.Context) ([]interface{}, error) {
	return exportRegistryAuth(ctx, "", true, func(server string) interface{} {
		return RegistryAuthTask{Global: true, Server: server, State: StatePresent}
	})
}

// exportRegistryAuth reads the auth-servers key for one scope and builds one
// task per server. The key is a comma-separated list, already named the way
// registry:login takes it - dokku maps docker's index-server spelling back to
// docker.io before reporting it.
func exportRegistryAuth(ctx context.Context, app string, global bool, build func(server string) interface{}) ([]interface{}, error) {
	key := "auth-servers"
	if global {
		key = "global-auth-servers"
	}

	payload, err := readPropertyReport(ctx, "registry", app, global)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}

	servers := []string{}
	for _, server := range strings.Split(payload[key], ",") {
		if server = strings.TrimSpace(server); server != "" {
			servers = append(servers, server)
		}
	}
	sort.Strings(servers)

	var out []interface{}
	for _, server := range servers {
		out = append(out, build(server))
	}
	return out, nil
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

// init registers the RegistryAuthTask with the task registry
func init() {
	RegisterTask(&RegistryAuthTask{})
}
