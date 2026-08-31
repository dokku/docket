package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// HttpAuthTask manages HTTP authentication for a dokku application
type HttpAuthTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Username is the HTTP auth username seeded when auth is enabled
	Username string `required:"false" yaml:"username,omitempty" description:"HTTP auth username to seed when enabling; supplied together with password"`

	// Password is the HTTP auth password seeded when auth is enabled
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"HTTP auth password for the seeded username; supplied together with username"`

	// State is the state of the HTTP auth
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"State of the HTTP auth"`
}

// HttpAuthTaskExample contains an example of an HttpAuthTask
type HttpAuthTaskExample struct {
	// Name is the task name holding the HttpAuthTask description
	Name string `yaml:"-"`

	// DokkuHttpAuth is the HttpAuthTask configuration
	DokkuHttpAuth HttpAuthTask `yaml:"dokku_http_auth"`
}

// GetName returns the name of the example
func (e HttpAuthTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the HTTP auth task
func (t HttpAuthTask) Doc() string {
	return "Manages HTTP authentication for a given dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t HttpAuthTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "the enabled state is exported; the seeded credentials are not carried on this task and come back as dokku_http_auth_user htpasswd hashes"}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t HttpAuthTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t HttpAuthTask) Requirements() []string {
	return []string{"dokku-http-auth plugin >= 0.13.0"}
}

// Examples returns a list of HttpAuthTaskExamples as yaml
func (t HttpAuthTask) Examples() ([]Doc, error) {
	return MarshalExamples([]HttpAuthTaskExample{
		{
			Name: "Enable HTTP authentication for an app",
			DokkuHttpAuth: HttpAuthTask{
				App:      "hello-world",
				Username: "admin",
				Password: "secret",
			},
		},
		{
			Name: "Enable HTTP authentication without seeding a user",
			DokkuHttpAuth: HttpAuthTask{
				App: "hello-world",
			},
		},
		{
			Name: "Disable HTTP authentication for an app",
			DokkuHttpAuth: HttpAuthTask{
				App:   "hello-world",
				State: "absent",
			},
		},
	})
}

// Execute enables or disables HTTP authentication for an app
func (t HttpAuthTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the HttpAuthTask's inputs without contacting the server.
//
// `http-auth:enable` takes the username and password as optional positional
// arguments (`<app> [<user>] [<password>]`) and skips user initialization when
// they are absent, so a credential-free `state: present` is valid and simply
// turns auth on. What is not valid is half a credential, so the pair is
// required together.
func (t HttpAuthTask) Validate() error {
	if t.State != StatePresent {
		return nil
	}
	if t.Username == "" && t.Password != "" {
		return fmt.Errorf("username is required when a password is set")
	}
	if t.Password == "" && t.Username != "" {
		return fmt.Errorf("password is required when a username is set")
	}
	return nil
}

// Plan reports the drift the HttpAuthTask would produce.
func (t HttpAuthTask) Plan(ctx context.Context) PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			enabled, err := httpAuthEnabled(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if enabled {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			// The credentials are optional trailing arguments; passing them as
			// empty strings would work but reads as a malformed command line in
			// plan output and the trace log, so they are omitted instead.
			args := []string{"--quiet", "http-auth:enable", t.App}
			if t.Username != "" && t.Password != "" {
				args = append(args, t.Username, t.Password)
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    args,
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusCreate,
				Reason:    "http-auth disabled",
				Mutations: []string{"http-auth:enable " + t.App},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			enabled, err := httpAuthEnabled(ctx, t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			if !enabled {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			inputs := []subprocess.ExecCommandInput{{
				Command: "dokku",
				Args:    []string{"--quiet", "http-auth:disable", t.App},
			}}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    "http-auth enabled",
				Mutations: []string{"http-auth:disable " + t.App},
				Commands:  resolveCommands(ctx, inputs),
				apply: func(ctx context.Context) TaskOutputState {
					return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// httpAuthEnabled checks if HTTP authentication is enabled for an app by
// reading the `enabled` key from `http-auth:report --format json` (the plugin
// strips the `http-auth-` prefix from JSON report keys, matching the nginx and
// proxy property plugins). A transport-level failure (`*subprocess.SSHError`)
// is propagated; a dokku-level non-zero exit (e.g. app does not exist) is
// treated as "disabled"; malformed JSON surfaces as an error.
func httpAuthEnabled(ctx context.Context, appName string) (bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"http-auth:report",
			appName,
			"--format",
			"json",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}

	var report struct {
		Enabled string `json:"enabled"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return false, err
	}

	return report.Enabled == "true", nil
}

// httpAuthState is the whole `http-auth:report --format json` payload the
// exporter needs: whether auth is serving, plus whether the app still carries
// any http-auth configuration. Disabling an app leaves its users, allowed IPs
// and auth domains in place, so those three are independent of Enabled.
type httpAuthState struct {
	Enabled    bool
	Configured bool
}

// getHttpAuthState reads the app's whole http-auth state in one report call.
// The four per-task readers each pull a single key; the exporter needs them
// together to decide whether a disabled app still needs a task emitted, so it
// reads them at once rather than making four round trips. Error handling
// matches those readers: a transport-level failure (`*subprocess.SSHError`) is
// propagated, a dokku-level non-zero exit (e.g. app does not exist) is treated
// as "nothing set", and malformed JSON surfaces as an error.
func getHttpAuthState(ctx context.Context, appName string) (httpAuthState, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"http-auth:report",
			appName,
			"--format",
			"json",
		},
	})
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return httpAuthState{}, err
		}
		return httpAuthState{}, nil
	}

	var report struct {
		Enabled    string `json:"enabled"`
		Users      string `json:"users"`
		AllowedIps string `json:"allowed-ips"`
		Domains    string `json:"domains"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return httpAuthState{}, err
	}

	return httpAuthState{
		Enabled: report.Enabled == "true",
		Configured: len(strings.Fields(report.Users)) > 0 ||
			len(strings.Fields(report.AllowedIps)) > 0 ||
			len(strings.Fields(report.Domains)) > 0,
	}, nil
}

// ExportApp reconstructs whether HTTP auth is serving for an app. It is emitted
// after the rest of the http-auth family (see appExportOrder), because
// http-auth:add-user, add-allowed-ip and add-domain all write enabled=true as a
// side effect: this task runs last so the recipe's stated auth state is the one
// that sticks.
//
// An enabled app emits `state: present`, with no credentials - the users come
// back as dokku_http_auth_user tasks, so seeding one here would only duplicate
// an input. A disabled app that still carries users, allowed IPs or auth
// domains emits `state: absent`, undoing the enable those tasks force; a
// disabled app with nothing configured emits no task at all.
func (t HttpAuthTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	state, err := getHttpAuthState(ctx, app)
	if err != nil {
		return nil, err
	}
	if state.Enabled {
		return []interface{}{HttpAuthTask{App: app, State: StatePresent}}, nil
	}
	if !state.Configured {
		return nil, nil
	}
	return []interface{}{HttpAuthTask{App: app, State: StateAbsent}}, nil
}

// init registers the HttpAuthTask with the task registry
func init() {
	RegisterTask(&HttpAuthTask{})
}
