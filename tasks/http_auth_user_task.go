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

	// Users is the list of HTTP auth users to add or remove. The docs generator
	// lists an item's field names but cannot express that password and hash are
	// alternatives, nor that both are secret, so this description carries it.
	Users []HttpAuthUser `required:"false" yaml:"users" description:"List of HTTP auth users to add or remove. Each item gives the credential as exactly one of password (cleartext, applied with http-auth:add-user) or hash (an htpasswd entry, applied with http-auth:import-users); both are sensitive"`

	// UpdatePassword re-issues http-auth:add-user for users that already exist
	// so their password converges. Cleartext passwords are not exposed in the
	// report, so a rotation cannot be drift-detected; enable this to force
	// convergence. It governs the password path only - a hash user converges on
	// its own, because http-auth:export-users reads the stored hash back. It is
	// a *bool so the value survives decoding unchanged; nil defaults to false.
	UpdatePassword *bool `required:"false" yaml:"update_password,omitempty" default:"false" description:"Re-issue add-user for users that already exist so their cleartext password converges. Users given by hash converge on their own and ignore this"`

	// State is the desired state of the HTTP auth users. There is no `clear`
	// state, unlike the sibling dokku_http_auth_domain: `absent` with an empty
	// `users` already removes every user.
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set" description:"Desired state of the HTTP auth users"`
}

// HttpAuthUser represents a single HTTP auth user. Exactly one of Password and
// Hash carries the credential.
//
// The `sensitive:"true"` tags document intent, but because the fields live in a
// slice-of-structs the reflection walker in sensitive.go cannot reach them -
// the task's SensitiveValues method is what actually masks these values.
type HttpAuthUser struct {
	// Username is the HTTP auth username
	Username string `required:"true" yaml:"username" description:"HTTP auth username"`

	// Password is the cleartext HTTP auth password, applied with
	// http-auth:add-user. Mutually exclusive with Hash.
	Password string `required:"false" sensitive:"true" yaml:"password,omitempty" description:"Cleartext HTTP auth password. Mutually exclusive with hash"`

	// Hash is the user's htpasswd entry, as emitted by
	// http-auth:export-users. Applied with http-auth:import-users over stdin,
	// so the value never reaches argv. Mutually exclusive with Password; this
	// is the form `docket export` emits, since the plugin can read hashes back
	// but never the passwords behind them.
	Hash string `required:"false" sensitive:"true" yaml:"hash,omitempty" description:"htpasswd hash for the user, as emitted by http-auth:export-users. Mutually exclusive with password; applied over stdin with http-auth:import-users so it never reaches argv"`
}

// exampleHttpAuthHash is the htpasswd entry the hash-form examples use. It is
// a real salted SHA-512 crypt hash so the published snippets look like what
// http-auth:export-users actually emits, and so the example integration run
// applies something the plugin would accept.
const exampleHttpAuthHash = "$6$s0Vd6Ns8Wq2Kx1Lp$Zq8mQ0zH1pR3tY7uJ5bN2cV4dX6fG8hK0lM2nP4rS6tU8vW0xY2zA4bC6dE8fG0"

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
//
// Supported without a caveat because http-auth:export-users reads every user's
// htpasswd entry back, so an exported recipe reproduces the users with no
// operator-supplied credentials. That command is guaranteed by the >= 0.13.0
// floor in Requirements(); the whole http-auth family already assumes it (a
// credential-free http-auth:enable is a 0.13.0 behaviour too), so there is no
// fallback for older plugins here.
func (t HttpAuthUserTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t HttpAuthUserTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbePartial, Caveat: "an existing user's htpasswd hash is probed; a cleartext password is not readable, so a user that already exists converges only when update_password forces it"}
}

// Requirements lists the non-core dokku plugins this task depends on.
func (t HttpAuthUserTask) Requirements() []string {
	return []string{"dokku-http-auth plugin >= 0.13.0"}
}

// SensitiveValues returns the per-user credentials so they are masked in
// user-facing output. The tag-based walker in sensitive.go does not descend
// into slices of structs, so they are collected here. Hashes are credential
// material too - they are what an attacker needs to reproduce the htpasswd -
// so they are masked alongside the cleartext passwords.
func (t HttpAuthUserTask) SensitiveValues() []string {
	out := make([]string, 0, len(t.Users))
	for _, u := range t.Users {
		if u.Password != "" {
			out = append(out, u.Password)
		}
		if u.Hash != "" {
			out = append(out, u.Hash)
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
			Name: "Add a user from an htpasswd hash, as `docket export` emits",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App: "hello-world",
				Users: []HttpAuthUser{
					{Username: "deploy", Hash: exampleHttpAuthHash},
				},
			},
		},
		{
			Name: "Rotate an existing user's password",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App:            "hello-world",
				Users:          []HttpAuthUser{{Username: "admin", Password: "new-secret"}},
				UpdatePassword: boolPtr(true),
			},
		},
		{
			Name: "Replace the app's users with exactly this set",
			DokkuHttpAuthUser: HttpAuthUserTask{
				App: "hello-world",
				Users: []HttpAuthUser{
					{Username: "deploy", Hash: exampleHttpAuthHash},
				},
				State: StateSet,
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
	seen := map[string]bool{}
	for _, u := range t.Users {
		if u.Username == "" {
			return fmt.Errorf("'username' is required for each user")
		}
		// Two entries for one username used to be harmless (two add-user calls,
		// last wins). Now that the credential forms dispatch to different
		// commands, the winner would depend on nothing but list order, and two
		// lines for one user in a single import stream is outside the plugin's
		// contract.
		if seen[u.Username] {
			return fmt.Errorf("user %q is listed more than once", u.Username)
		}
		seen[u.Username] = true
		if u.Password != "" && u.Hash != "" {
			return fmt.Errorf("'password' and 'hash' are mutually exclusive for user %q", u.Username)
		}
		if u.Hash != "" {
			// docket frames the `username:hash` stream itself, so a colon in
			// the username or a newline in either half would write a corrupt
			// htpasswd. Password users go on argv and are left alone, so no
			// recipe that validates today starts failing.
			if strings.Contains(u.Username, ":") {
				return fmt.Errorf("'username' must not contain ':' for user %q given by hash", u.Username)
			}
			if strings.ContainsAny(u.Username, "\r\n") || strings.ContainsAny(u.Hash, "\r\n") {
				return fmt.Errorf("'username' and 'hash' must not contain newlines for user %q", u.Username)
			}
		}
	}
	if t.State == StatePresent || t.State == StateSet {
		if len(t.Users) == 0 {
			return fmt.Errorf("'users' must not be empty for state '%s'", t.State)
		}
		for _, u := range t.Users {
			if u.Password == "" && u.Hash == "" {
				return fmt.Errorf("one of 'password' or 'hash' is required for user %q when state is '%s'", u.Username, t.State)
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
		StatePresent: func() PlanResult { return planHttpAuthUsersPresent(t) },
		StateSet:     func() PlanResult { return planHttpAuthUsersSet(t) },
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

// planHttpAuthUsersPresent reports drift for the upsert state: every listed
// user ends up on the app, and users the task does not mention are untouched.
//
// A cleartext password is not readable, so a user that already exists converges
// only when update_password forces it. A hash is readable, so a hash user
// converges on its own - the stored-hash comparison is evaluated first, which
// also stops update_password from re-importing a hash that already matches.
func planHttpAuthUsersPresent(t HttpAuthUserTask) PlanResult {
	current, hashes, res := readHttpAuthUserState(t)
	if res != nil {
		return *res
	}

	toApply := []HttpAuthUser{}
	mutations := []string{}
	for _, u := range t.Users {
		switch {
		case !current[u.Username]:
			toApply = append(toApply, u)
			mutations = append(mutations, "add "+u.Username)
		case u.Hash != "":
			if hashes[u.Username] != u.Hash {
				toApply = append(toApply, u)
				mutations = append(mutations, "update "+u.Username)
			}
		case boolValue(t.UpdatePassword, false):
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
	inputs := httpAuthUserUpsertInputs(t.App, toApply, false)
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
}

// planHttpAuthUsersSet reports drift for the exact-set state: after applying,
// the app's users are exactly the listed ones.
//
// When every listed user carries a hash the whole set is expressible as one
// `http-auth:import-users --replace`, which is what that flag is for. A
// cleartext password cannot be written into the htpasswd stream, so a list with
// any password user falls back to removing the strays and upserting the rest.
func planHttpAuthUsersSet(t HttpAuthUserTask) PlanResult {
	current, hashes, res := readHttpAuthUserState(t)
	if res != nil {
		return *res
	}

	desired := map[string]bool{}
	for _, u := range t.Users {
		desired[u.Username] = true
	}

	toApply := []HttpAuthUser{}
	mutations := []string{}
	allHashed := true
	for _, u := range t.Users {
		if u.Hash == "" {
			allHashed = false
		}
		switch {
		case !current[u.Username]:
			toApply = append(toApply, u)
			mutations = append(mutations, "add "+u.Username)
		case u.Hash != "":
			if hashes[u.Username] != u.Hash {
				toApply = append(toApply, u)
				mutations = append(mutations, "update "+u.Username)
			}
		case boolValue(t.UpdatePassword, false):
			toApply = append(toApply, u)
			mutations = append(mutations, "update "+u.Username)
		}
	}

	toRemove := []string{}
	for u := range current {
		if !desired[u] {
			toRemove = append(toRemove, u)
		}
	}
	sort.Strings(toRemove)
	for _, u := range toRemove {
		mutations = append(mutations, "remove "+u)
	}

	if len(mutations) == 0 {
		return PlanResult{InSync: true, Status: PlanStatusOK}
	}

	var inputs []subprocess.ExecCommandInput
	if allHashed {
		// One atomic write of the whole htpasswd. Every listed user goes in,
		// not just the drifting ones, because --replace discards whatever it
		// does not receive.
		inputs = httpAuthUserUpsertInputs(t.App, t.Users, true)
	} else {
		for _, u := range toRemove {
			inputs = append(inputs, subprocess.ExecCommandInput{
				Command: "dokku",
				Args:    []string{"--quiet", "http-auth:remove-user", t.App, u},
			})
		}
		inputs = append(inputs, httpAuthUserUpsertInputs(t.App, toApply, false)...)
	}

	// Modify even when the only mutations are removals, matching
	// planHttpAuthDomainsSet and planDomainsSet: a whole-set replacement is one
	// operation, not a destroy.
	return PlanResult{
		InSync:    false,
		Status:    PlanStatusModify,
		Reason:    fmt.Sprintf("%d user change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(inputs),
		apply: func() TaskOutputState {
			return runExecInputs(TaskOutputState{State: StatePresent}, StateSet, inputs)
		},
	}
}

// readHttpAuthUserState probes the app's current users, and their stored hashes
// when the task expresses any user by hash. A non-nil PlanResult is the caller's
// early return.
//
// Membership keeps coming from `http-auth:report`, which every version of the
// plugin answers, so a password-only recipe costs exactly one round trip and is
// unaffected by the newer export-users command. The hash overlay is read only
// when it is needed.
func readHttpAuthUserState(t HttpAuthUserTask) (map[string]bool, map[string]string, *PlanResult) {
	current, err := getHttpAuthUsers(t.App)
	if err != nil {
		return nil, nil, &PlanResult{Status: PlanStatusError, Error: err}
	}

	wantsHashes := false
	for _, u := range t.Users {
		if u.Hash != "" {
			wantsHashes = true
			break
		}
	}
	if !wantsHashes {
		return current, map[string]string{}, nil
	}

	hashes, err := getHttpAuthUserHashes(t.App)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, nil, &PlanResult{Status: PlanStatusError, Error: err}
		}
		// A dokku-level failure means the hashes could not be read, not that
		// there are none: `plan` runs against servers where the app does not
		// exist yet (created by an earlier task in the same recipe), which is
		// why getHttpAuthUsers swallows too. Every hash user then reads as
		// drift, which is right for the fresh-app case and cannot spin
		// silently for the too-old-plugin case, since the import-users the
		// plan proposes is the very command that would be missing.
		return current, map[string]string{}, nil
	}
	return current, hashes, nil
}

// httpAuthUserUpsertInputs renders the commands that add or update users. Hash
// users are collapsed into a single `http-auth:import-users` fed over stdin;
// cleartext users each get an `http-auth:add-user`.
//
// The import comes first because it validates every line before writing
// anything, and runExecInputs stops at the first failure with earlier commands
// already applied - so the all-or-nothing command running first keeps the
// partial-apply window as small as it can be. Order carries no other meaning:
// both commands upsert by username.
func httpAuthUserUpsertInputs(app string, users []HttpAuthUser, replace bool) []subprocess.ExecCommandInput {
	var entries []string
	var passwordUsers []HttpAuthUser
	for _, u := range users {
		if u.Hash != "" {
			entries = append(entries, u.Username+":"+u.Hash)
			continue
		}
		passwordUsers = append(passwordUsers, u)
	}

	inputs := make([]subprocess.ExecCommandInput, 0, len(passwordUsers)+1)
	if len(entries) > 0 {
		args := []string{"--quiet", "http-auth:import-users", app}
		if replace {
			args = append(args, "--replace")
		}
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    args,
			// The payload never reaches argv, so it stays out of the plan
			// output, the trace log, and the remote process table.
			Stdin: strings.NewReader(strings.Join(entries, "\n") + "\n"),
		})
	}
	for _, u := range passwordUsers {
		inputs = append(inputs, subprocess.ExecCommandInput{
			Command: "dokku",
			Args:    []string{"--quiet", "http-auth:add-user", app, u.Username, u.Password},
		})
	}
	return inputs
}

// getHttpAuthUserHashes reads the app's stored htpasswd entries back via
// `dokku --quiet http-auth:export-users <app>`, which writes one `user:hash`
// line per user to stdout and nothing else. `--quiet` is a deliberate
// divergence from the `:report` readers in this package: the plugin logs a
// notice when the app has no users, and only the entries should be parsed.
//
// The command exits 0 with empty stdout when the app has no users, and works
// even while auth is disabled, so a missing entry means the user is absent
// rather than unreadable. Every error is returned raw, unlike getHttpAuthUsers,
// because Plan and ExportApp want different fallbacks for a dokku-level
// failure.
//
// Parsing is deliberately strict, since anything let through here ends up in an
// exported recipe: blank lines, lines with no separator, and lines with an
// empty hash are skipped rather than producing a credential-less user that the
// task's own Validate would reject. The split is on the first colon - a
// username cannot contain one, a crypt hash can.
//
// Comparison against a desired hash is byte-exact. A future plugin that
// normalized entries on import would surface as perpetual drift rather than
// silent divergence.
func getHttpAuthUserHashes(appName string) (map[string]string, error) {
	result, err := subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "dokku",
		Args: []string{
			"--quiet",
			"http-auth:export-users",
			appName,
		},
	})
	if err != nil {
		return nil, err
	}

	hashes := map[string]string{}
	for _, line := range strings.Split(result.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		username, hash, found := strings.Cut(line, ":")
		if !found || username == "" || hash == "" {
			continue
		}
		hashes[username] = hash
	}
	return hashes, nil
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

// ExportApp reconstructs the app's HTTP-auth users, each carrying the stored
// htpasswd hash. That is the whole point of exporting by hash: the recipe
// reproduces the users without anyone knowing the passwords behind them.
//
// http-auth:export-users reads the same htpasswd the report derives its `users`
// key from, so it is the only probe needed here. A transport-level failure
// (`*subprocess.SSHError`) is propagated; any other failure is treated as "no
// users", matching the other readers in this package - that is what keeps a
// server without the http-auth plugin from raising an export warning for every
// app it has.
//
// State is set explicitly so the emitted body is directly plannable, rather
// than depending on the decode-time default. It is `present`, not `set`, even
// though every listed user is exported and the sibling exact-set exporters
// (dokku_http_auth_domain, dokku_domains, dokku_ports) emit `set`: an exported
// recipe should not silently delete a user that exists only on the destination.
func (t HttpAuthUserTask) ExportApp(app string) ([]interface{}, error) {
	hashes, err := getHttpAuthUserHashes(app)
	if err != nil {
		var sshErr *subprocess.SSHError
		if errors.As(err, &sshErr) {
			return nil, err
		}
		return nil, nil
	}
	if len(hashes) == 0 {
		return nil, nil
	}
	names := make([]string, 0, len(hashes))
	for name := range hashes {
		names = append(names, name)
	}
	sort.Strings(names)
	list := make([]HttpAuthUser, 0, len(names))
	for _, name := range names {
		list = append(list, HttpAuthUser{Username: name, Hash: hashes[name]})
	}
	return []interface{}{HttpAuthUserTask{App: app, Users: list, State: StatePresent}}, nil
}

// init registers the HttpAuthUserTask with the task registry
func init() {
	RegisterTask(&HttpAuthUserTask{})
}
