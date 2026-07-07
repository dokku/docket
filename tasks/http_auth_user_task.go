package tasks

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/dokku/docket/subprocess"
)

// HttpAuthUserTask manages the set of HTTP auth users for a dokku application
type HttpAuthUserTask struct {
	// App is the name of the app
	App string `required:"true" yaml:"app" description:"Name of the app"`

	// Users is the list of HTTP auth users to add or remove
	Users []HttpAuthUser `required:"false" yaml:"users" description:"List of HTTP auth users to add or remove"`

	// UpdatePassword re-issues http-auth:add-user for users that already exist
	// so their password converges. Passwords are not exposed in the report, so
	// a rotation cannot be drift-detected; enable this to force convergence.
	UpdatePassword bool `required:"false" yaml:"update_password" default:"false" description:"Re-issue add-user for users that already exist so their password converges"`

	// State is the desired state of the HTTP auth users
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent" description:"Desired state of the HTTP auth users"`
}

// HttpAuthUser represents a single HTTP auth user
type HttpAuthUser struct {
	// Username is the HTTP auth username
	Username string `required:"true" yaml:"username" description:"HTTP auth username"`

	// Password is the HTTP auth password. The `sensitive:"true"` tag documents
	// intent, but because the field lives in a slice-of-structs the reflection
	// walker in sensitive.go cannot reach it - the task's SensitiveValues method
	// is what actually masks these values.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"HTTP auth password"`
}

// HttpAuthUserTaskExample contains an example of an HttpAuthUserTask
type HttpAuthUserTaskExample struct {
	// Name is the task name holding the HttpAuthUserTask description
	Name string `yaml:"-"`

	// DokkuHttpAuthUser is the HttpAuthUserTask configuration
	DokkuHttpAuthUser HttpAuthUserTask `yaml:"dokku_http_auth_user"`
}

// GetName returns the name of the example
func (e HttpAuthUserTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the HTTP auth user task
func (t HttpAuthUserTask) Doc() string {
	return "Manages the set of HTTP auth users for a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t HttpAuthUserTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportPartial, Caveat: "usernames are exported; each password is not readable and becomes a required input"}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t HttpAuthUserTask) Requirements() []string {
	return []string{"dokku-http-auth plugin"}
}

// SensitiveValues returns the per-user passwords so they are masked in
// user-facing output. The tag-based walker in sensitive.go does not descend
// into slices of structs, so the sensitive passwords are collected here.
func (t HttpAuthUserTask) SensitiveValues() []string {
	out := make([]string, 0, len(t.Users))
	for _, u := range t.Users {
		if u.Password != "" {
			out = append(out, u.Password)
		}
	}
	return out
}

// Examples returns a list of HttpAuthUserTaskExamples as yaml
func (t HttpAuthUserTask) Examples() ([]Doc, error) {
	return MarshalExamples([]HttpAuthUserTaskExample{
		{
			Name: "Add HTTP auth users to an app",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App: "hello-world",
				Users: []HttpAuthUser{
					{Username: "admin", Password: "secret"},
					{Username: "ops", Password: "hunter2"},
				},
			},
		},
		{
			Name: "Rotate an existing user's password",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App:            "hello-world",
				Users:          []HttpAuthUser{{Username: "admin", Password: "new-secret"}},
				UpdatePassword: true,
			},
		},
		{
			Name: "Remove a user from an app",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App:   "hello-world",
				Users: []HttpAuthUser{{Username: "ops"}},
				State: StateAbsent,
			},
		},
		{
			Name: "Remove all HTTP auth users from an app",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App:   "hello-world",
				State: StateAbsent,
			},
		},
	})
}

// Execute manages the app's HTTP auth users
func (t HttpAuthUserTask) Execute() TaskOutputState {
	return ExecutePlan(t.Plan())
}

// Validate checks the HttpAuthUserTask's inputs without contacting the server.
func (t HttpAuthUserTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	for _, u := range t.Users {
		if u.Username == "" {
			return fmt.Errorf("'username' is required for each user")
		}
	}
	if t.State == StatePresent {
		if len(t.Users) == 0 {
			return fmt.Errorf("'users' must not be empty for state 'present'")
		}
		for _, u := range t.Users {
			if u.Password == "" {
				return fmt.Errorf("'password' is required for user %q when state is present", u.Username)
			}
		}
	}
	return nil
}

// Plan reports the drift the HttpAuthUserTask would produce.
func (t HttpAuthUserTask) Plan() PlanResult {
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			current, err := getHttpAuthUsers(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toApply := []HttpAuthUser{}
			mutations := []string{}
			for _, u := range t.Users {
				switch {
				case !current[u.Username]:
					toApply = append(toApply, u)
					mutations = append(mutations, "add "+u.Username)
				case t.UpdatePassword:
					toApply = append(toApply, u)
					mutations = append(mutations, "update "+u.Username)
				}
			}
			if len(toApply) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			status := PlanStatusModify
			if len(current) == 0 {
				status = PlanStatusCreate
			}
			inputs := make([]subprocess.ExecCommandInput, 0, len(toApply))
			for _, u := range toApply {
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", "http-auth:add-user", t.App, u.Username, u.Password},
				})
			}
			return PlanResult{
				InSync:    false,
				Status:    status,
				Reason:    fmt.Sprintf("%d user(s) to add or update", len(toApply)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, inputs)
				},
			}
		},
		StateAbsent: func() PlanResult {
			current, err := getHttpAuthUsers(t.App)
			if err != nil {
				return PlanResult{Status: PlanStatusError, Error: err}
			}
			toRemove := []string{}
			if len(t.Users) == 0 {
				for u := range current {
					toRemove = append(toRemove, u)
				}
				sort.Strings(toRemove)
			} else {
				for _, u := range t.Users {
					if current[u.Username] {
						toRemove = append(toRemove, u.Username)
					}
				}
			}
			if len(toRemove) == 0 {
				return PlanResult{InSync: true, Status: PlanStatusOK}
			}
			mutations := make([]string, 0, len(toRemove))
			inputs := make([]subprocess.ExecCommandInput, 0, len(toRemove))
			for _, u := range toRemove {
				mutations = append(mutations, "remove "+u)
				inputs = append(inputs, subprocess.ExecCommandInput{
					Command: "dokku",
					Args:    []string{"--quiet", "http-auth:remove-user", t.App, u},
				})
			}
			return PlanResult{
				InSync:    false,
				Status:    PlanStatusDestroy,
				Reason:    fmt.Sprintf("%d user(s) to remove", len(toRemove)),
				Mutations: mutations,
				Commands:  resolveCommands(inputs),
				apply: func() TaskOutputState {
					return runExecInputs(TaskOutputState{State: StatePresent}, StateAbsent, inputs)
				},
			}
		},
	})
}

// getHttpAuthUsers reads the current set of HTTP auth users for an app from the
// `users` key of `http-auth:report --format json`. The plugin strips the
// `http-auth-` prefix from JSON report keys (so the key is `users`, not
// `http-auth-users`) and emits the usernames as a single space-separated
// string. A transport-level failure (`*subprocess.SSHError`) is propagated; a
// dokku-level non-zero exit (e.g. app does not exist) is treated as "no users";
// malformed JSON surfaces as an error.
func getHttpAuthUsers(appName string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
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
			return nil, err
		}
		return map[string]bool{}, nil
	}

	var report struct {
		Users string `json:"users"`
	}
	if err := json.Unmarshal(result.StdoutBytes(), &report); err != nil {
		return nil, err
	}

	users := map[string]bool{}
	for _, u := range strings.Fields(report.Users) {
		users[u] = true
	}
	return users, nil
}

// ExportApp reconstructs the app's HTTP-auth users. Usernames come from the
// report; passwords are not exposed, so the engine lifts each into a required
// input the user fills in before applying (see processHttpAuthUser).
func (t HttpAuthUserTask) ExportApp(app string) ([]interface{}, error) {
	users, err := getHttpAuthUsers(app)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	list := make([]HttpAuthUser, 0, len(users))
	for _, name := range sortedSetKeys(users) {
		list = append(list, HttpAuthUser{Username: name})
	}
	return []interface{}{HttpAuthUserTask{App: app, Users: list}}, nil
}

// init registers the HttpAuthUserTask with the task registry
func init() {
	RegisterTask(&HttpAuthUserTask{})
}
