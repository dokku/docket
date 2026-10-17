package tasks

import (
	"context"
	"fmt"
	"strings"

	"github.com/dokku/docket/internal/subprocess"
)

// AclAppTask manages the dokku-acl access list for a dokku application
type AclAppTask struct {
	// App is the name of the app
	App string `required:"true" identity:"key" yaml:"app" description:"Name of the app"`

	// Users is the list of users to add, remove, or set on the ACL
	Users []string `required:"false" identity:"collection" ordered:"false" yaml:"users,omitempty" description:"List of users to add, remove, or set on the ACL; omit for state 'clear'"`

	// State is the desired state of the ACL entries
	State State `required:"false" yaml:"state,omitempty" default:"present" options:"present,absent,set,clear" description:"Desired state of the ACL entries"`
}

// AclAppTaskExample contains an example of an AclAppTask
type AclAppTaskExample struct {
	// Name is the task name holding the AclAppTask description
	Name string `yaml:"-"`

	// AclAppTask is the AclAppTask configuration
	AclAppTask AclAppTask `yaml:"dokku_acl_app"`
}

// GetName returns the name of the example
func (e AclAppTaskExample) GetName() string {
	return e.Name
}

// Doc returns the docblock for the acl app task
func (t AclAppTask) Doc() string {
	return "Manages the dokku-acl access list for a dokku application"
}

// ExportSupport reports how docket export handles this task.
func (t AclAppTask) ExportSupport() ExportSupport {
	return ExportSupport{Status: ExportSupported}
}

// ProbeSupport reports whether Plan() can read this task's current state.
func (t AclAppTask) ProbeSupport() ProbeSupport {
	return ProbeSupport{Status: ProbeSupported}
}

// MinimumVersions reports the oldest Dokku, and any plugin, this task's probe parses.
// The probe is `acl:list`. 0.38.31 is the Dokku the integration suite runs.
func (t AclAppTask) MinimumVersions() VersionSupport {
	return VersionSupport{
		Dokku: "0.38.31",
		Plugins: []PluginRequirement{{
			Name:    "acl",
			Minimum: "2.0.1",
			Text:    "dokku-acl plugin >= 2.0.1",
		}},
	}
}

// examples returns the examples for the acl app task
func (t AclAppTask) examples() ([]Doc, error) {
	return MarshalExamples([]AclAppTaskExample{
		{
			Name: "Grant users access to an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				Users: []string{"alice", "bob"},
			},
		},
		{
			Name: "Revoke a user's access to an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				Users: []string{"bob"},
				State: StateAbsent,
			},
		},
		{
			Name: "Replace the users with access to an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				Users: []string{"alice"},
				State: StateSet,
			},
		},
		{
			Name: "Clear the entire ACL for an app",
			AclAppTask: AclAppTask{
				App:   "node-js-app",
				State: StateClear,
			},
		},
	})
}

// Execute manages the app ACL
func (t AclAppTask) Execute(ctx context.Context) TaskOutputState {
	return ExecutePlan(ctx, t.Plan(ctx))
}

// Validate checks the AclAppTask's inputs without contacting the server, so
// `docket validate` and Plan() surface the same errors.
func (t AclAppTask) Validate() error {
	if t.App == "" {
		return fmt.Errorf("'app' is required")
	}
	return validateAclUsers(t.State, t.Users)
}

// Plan reports the drift the AclAppTask would produce.
func (t AclAppTask) Plan(ctx context.Context) PlanResult {
	if r, ok := gatePlan(ctx, t); !ok {
		return r
	}
	if err := t.Validate(); err != nil {
		return planErr(err)
	}
	probe := func() (map[string]bool, error) { return getAclAppUsers(ctx, t.App) }
	observe := func(users []string, state State) Task {
		return AclAppTask{App: t.App, Users: users, State: state}
	}
	return DispatchPlan(t.State, map[State]func() PlanResult{
		StatePresent: func() PlanResult {
			return planAclPresent(ctx, observe, probe, t.Users, func(u string) []string {
				return []string{"--quiet", "acl:add", t.App, u}
			})
		},
		StateAbsent: func() PlanResult {
			return planAclAbsent(ctx, observe, probe, t.Users, func(u string) []string {
				return []string{"--quiet", "acl:remove", t.App, u}
			})
		},
		StateSet: func() PlanResult {
			if len(t.Users) == 0 {
				return planSetAsClear(planAclClear(ctx, observe, probe, dokkuArgsInputs("acl:set-users", t.App, nil)))
			}
			return planAclSet(ctx, observe, probe, t.Users, dokkuArgsInputs("acl:set-users", t.App, t.Users))
		},
		StateClear: func() PlanResult {
			return planAclClear(ctx, observe, probe, dokkuArgsInputs("acl:set-users", t.App, nil))
		},
	})
}

// validateAclUsers checks the users list against the state it is used with.
// present and absent each need at least one user, a set with none plans as
// clear, and clear takes none: acl:set-users is called with no users to clear,
// so a list supplied alongside it would be silently discarded rather than
// removed. Every user is held to
// fn-acl-validate-user in dokku-acl, which acl:add, acl:remove and
// acl:set-users (and their service variants) all apply, since the name is used
// as a file name inside the acl directory.
func validateAclUsers(state State, users []string) error {
	switch state {
	case StatePresent, StateAbsent, StateSet:
		if len(users) == 0 && state != StateSet {
			return fmt.Errorf("'users' must not be empty for state '%s'", state)
		}
		for i, u := range users {
			if !validAclUser(u) {
				return fmt.Errorf("'users' must be a valid user name for users[%d], got %q", i, u)
			}
		}
	case StateClear:
		if len(users) > 0 {
			return fmt.Errorf("'users' must not be set for state 'clear'")
		}
	}
	return nil
}

// validAclUser reports whether a user name is one dokku-acl can store: not
// empty, free of `/`, and neither `.` nor `..`.
func validAclUser(user string) bool {
	return user != "" && !strings.Contains(user, "/") && user != "." && user != ".."
}

// aclObserveFunc builds the calling ACL task's own type from the users the
// probe read and the state they amount to, for PlanResult.Observed.
type aclObserveFunc func(users []string, state State) Task

// observedAcl attaches the users the probe read to r. An additive plan
// (present, absent) reads only the users it names, reported as present or
// absent; a plan that owns the list (set, clear) reads every user, reported
// as set, or as clear when there are none.
func observedAcl(r PlanResult, observe aclObserveFunc, current map[string]bool, declared []string, whole bool) PlanResult {
	held := map[string]bool{}
	if whole {
		for u := range current {
			held[u] = true
		}
	} else {
		for _, u := range declared {
			if current[u] {
				held[u] = true
			}
		}
	}
	var users []string
	if len(held) > 0 {
		users = sortedSetKeys(held)
	}
	state := StateAbsent
	switch {
	case whole && len(users) > 0:
		state = StateSet
	case whole:
		state = StateClear
	case len(users) > 0:
		state = StatePresent
	}
	return observed(r, observe(users, state), len(users) > 0)
}

// planAclPresent reports drift for the present-state user add, running one
// command (built by addArgs) per user missing from the ACL.
func planAclPresent(ctx context.Context, observe aclObserveFunc, probe func() (map[string]bool, error), users []string, addArgs func(string) []string) PlanResult {
	current, err := probe()
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	inputs := []subprocess.ExecCommandInput{}
	mutations := []string{}
	for _, u := range users {
		if !current[u] {
			inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: addArgs(u)})
			mutations = append(mutations, "add "+u)
		}
	}
	if len(inputs) == 0 {
		return observedAcl(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, users, false)
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	return observedAcl(PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d user(s) to add", len(inputs)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StatePresent, inputs)
		},
	}, observe, current, users, false)
}

// planAclAbsent reports drift for the absent-state user remove, running one
// command (built by removeArgs) per named user still on the ACL.
func planAclAbsent(ctx context.Context, observe aclObserveFunc, probe func() (map[string]bool, error), users []string, removeArgs func(string) []string) PlanResult {
	current, err := probe()
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	inputs := []subprocess.ExecCommandInput{}
	mutations := []string{}
	for _, u := range users {
		if current[u] {
			inputs = append(inputs, subprocess.ExecCommandInput{Command: "dokku", Args: removeArgs(u)})
			mutations = append(mutations, "remove "+u)
		}
	}
	if len(inputs) == 0 {
		return observedAcl(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, users, false)
	}
	return observedAcl(PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("%d user(s) to remove", len(inputs)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateAbsent, inputs)
		},
	}, observe, current, users, false)
}

// planAclSet reports drift for the set-state full replacement, which runs the
// single whole-list command in inputs rather than one add or remove per user,
// so a failure partway through can never leave a user on the ACL that the
// recipe dropped.
func planAclSet(ctx context.Context, observe aclObserveFunc, probe func() (map[string]bool, error), users []string, inputs []subprocess.ExecCommandInput) PlanResult {
	current, err := probe()
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	desired := map[string]bool{}
	for _, u := range users {
		desired[u] = true
	}
	mutations := []string{}
	for _, u := range sortedSetKeys(desired) {
		if !current[u] {
			mutations = append(mutations, "add "+u)
		}
	}
	for _, u := range sortedSetKeys(current) {
		if !desired[u] {
			mutations = append(mutations, "remove "+u)
		}
	}
	if len(mutations) == 0 {
		return observedAcl(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, nil, true)
	}
	status := PlanStatusModify
	if len(current) == 0 {
		status = PlanStatusCreate
	}
	return observedAcl(PlanResult{
		InSync:    false,
		Status:    status,
		Reason:    fmt.Sprintf("%d user change(s)", len(mutations)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StateAbsent}, StateSet, inputs)
		},
	}, observe, current, nil, true)
}

// planAclClear reports drift for the clear-state operation, which runs the
// whole-list command in inputs with no users.
func planAclClear(ctx context.Context, observe aclObserveFunc, probe func() (map[string]bool, error), inputs []subprocess.ExecCommandInput) PlanResult {
	current, err := probe()
	if err != nil {
		return PlanResult{Status: PlanStatusError, Error: err}
	}
	if len(current) == 0 {
		return observedAcl(PlanResult{InSync: true, Status: PlanStatusOK}, observe, current, nil, true)
	}
	users := sortedSetKeys(current)
	mutations := make([]string, 0, len(users))
	for _, u := range users {
		mutations = append(mutations, "remove "+u)
	}
	return observedAcl(PlanResult{
		InSync:    false,
		Status:    PlanStatusDestroy,
		Reason:    fmt.Sprintf("clear %d user(s)", len(users)),
		Mutations: mutations,
		Commands:  resolveCommands(ctx, inputs),
		apply: func(ctx context.Context) TaskOutputState {
			return runExecInputs(ctx, TaskOutputState{State: StatePresent}, StateClear, inputs)
		},
	}, observe, current, nil, true)
}

// getAclAppUsers reads the current ACL for an app via `acl:list APP`. The
// plugin emits one username per line; an empty ACL produces no output.
func getAclAppUsers(ctx context.Context, app string) (map[string]bool, error) {
	result, err := subprocess.CallExecCommand(ctx, subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "acl:list", app},
	})
	if err != nil {
		return nil, err
	}
	return parseAclUsers(result.StdoutContents()), nil
}

// parseAclUsers turns dokku-acl listing output, one username per line, into
// a set. Blank lines are ignored.
func parseAclUsers(output string) map[string]bool {
	users := map[string]bool{}
	for _, line := range strings.Split(output, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		users[trimmed] = true
	}
	return users
}

// ExportApp reconstructs the app's ACL user list, or nil when it is empty.
// state:set replaces the whole list for an exact match.
func (t AclAppTask) ExportApp(ctx context.Context, app string) ([]interface{}, error) {
	users, err := getAclAppUsers(ctx, app)
	if err != nil {
		return nil, err
	}
	if len(users) == 0 {
		return nil, nil
	}
	return []interface{}{AclAppTask{App: app, Users: sortedSetKeys(users), State: StateSet}}, nil
}

// init registers the AclAppTask with the task registry
func init() {
	RegisterTask(&AclAppTask{})
}
