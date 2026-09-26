package tasks

import (
	"context"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestAclAppTaskInvalidState(t *testing.T) {
	task := AclAppTask{App: "test-app", Users: []string{"alice"}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestAclAppTaskPresentMissingApp(t *testing.T) {
	task := AclAppTask{Users: []string{"alice"}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestAclAppTaskAbsentMissingApp(t *testing.T) {
	task := AclAppTask{State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestAclAppTaskPresentEmptyUsers(t *testing.T) {
	task := AclAppTask{App: "test-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty users and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'users' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksAclAppTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: grant access
      dokku_acl_app:
        app: test-app
        users:
          - alice
          - bob
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("grant access")
	if task == nil {
		t.Fatal("task 'grant access' not found")
	}

	aclTask, ok := task.(*AclAppTask)
	if !ok {
		t.Fatalf("task is not an AclAppTask (type is %T)", task)
	}
	if aclTask.App != "test-app" {
		t.Errorf("App = %q, want %q", aclTask.App, "test-app")
	}
	if len(aclTask.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(aclTask.Users))
	}
	if aclTask.Users[0] != "alice" || aclTask.Users[1] != "bob" {
		t.Errorf("Users = %v, want [alice, bob]", aclTask.Users)
	}
	if aclTask.State != StatePresent {
		t.Errorf("State = %q, want %q", aclTask.State, StatePresent)
	}
}

func TestAclAppTaskValidateUsersPerState(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		users   []string
		state   State
		wantErr string
	}{
		{name: "absent without users", state: StateAbsent, wantErr: "'users' must not be empty for state 'absent'"},
		{name: "set without users", state: StateSet, wantErr: "'users' must not be empty for state 'set'"},
		{name: "clear with users", users: []string{"alice"}, state: StateClear, wantErr: "'users' must not be set for state 'clear'"},
		{name: "clear without users", state: StateClear},
		{name: "set with users", users: []string{"alice"}, state: StateSet},
		{name: "present empty name", users: []string{"alice", ""}, state: StatePresent, wantErr: `for users[1], got ""`},
		{name: "absent slash", users: []string{"a/b"}, state: StateAbsent, wantErr: `for users[0], got "a/b"`},
		{name: "set dot", users: []string{"."}, state: StateSet, wantErr: `for users[0], got "."`},
		{name: "set dot dot", users: []string{".."}, state: StateSet, wantErr: `for users[0], got ".."`},
		{name: "dotted name is fine", users: []string{".alice", "a.b"}, state: StateSet},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := AclAppTask{App: "web", Users: tc.users, State: tc.state}.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("expected error containing %q, got %v", tc.wantErr, err)
			}
		})
	}
}

func TestAclAppTaskSetAndClearMissingApp(t *testing.T) {
	t.Parallel()
	for _, task := range []AclAppTask{
		{Users: []string{"alice"}, State: StateSet},
		{State: StateClear},
	} {
		if err := task.Validate(); err == nil || !strings.Contains(err.Error(), "'app' is required") {
			t.Errorf("state=%s: expected missing app error, got %v", task.State, err)
		}
	}
}

// aclAppListKey is the fakeDokku key for the probe getAclAppUsers runs.
const aclAppListKey = "--quiet acl:list web"

func TestAclAppSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{aclAppListKey: "bob\nalice\n"}))

	// The users are declared out of sorted order on purpose: the mutation lines
	// come out sorted, while the command keeps the declared order.
	plan := AclAppTask{App: "web", Users: []string{"dave", "alice", "carol"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the desired users differ from the ACL")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "acl:set-users web dave alice carol") {
		t.Errorf("expected acl:set-users with the full desired list, got %q", plan.Commands[0])
	}
	want := []string{"add carol", "add dave", "remove bob"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestAclAppSetOnEmptyAclIsACreate(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{}))

	plan := AclAppTask{App: "web", Users: []string{"alice"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
}

func TestAclAppSetConvergesWhenAclMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{aclAppListKey: "bob\nalice\n"}))

	// A repeated user collapses into the one entry the plugin stores.
	plan := AclAppTask{App: "web", Users: []string{"alice", "bob", "alice"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync once the ACL matches the desired set, got %#v", plan)
	}
}

func TestAclAppClearPlansEveryUser(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{aclAppListKey: "bob\nalice\n"}))

	plan := AclAppTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "acl:set-users web") {
		t.Errorf("expected a single acl:set-users with no users, got %v", plan.Commands)
	}
	if want := []string{"remove alice", "remove bob"}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestAclAppClearIsInSyncWithoutUsers(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{}))

	plan := AclAppTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync on an empty ACL, got %#v", plan)
	}
}

func TestAclAppAbsentPlansOnlyTheNamedUsers(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{aclAppListKey: "bob\nalice\n"}))

	plan := AclAppTask{App: "web", Users: []string{"bob", "carol"}, State: StateAbsent}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if want := []string{"remove bob"}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "acl:remove web bob") {
		t.Errorf("expected a single acl:remove for bob, got %v", plan.Commands)
	}
}

func TestAclAppPresentStatus(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		acl  string
		want PlanStatus
	}{
		{name: "empty acl", acl: "", want: PlanStatusCreate},
		{name: "non-empty acl", acl: "bob\n", want: PlanStatusModify},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{aclAppListKey: tc.acl}))
			plan := AclAppTask{App: "web", Users: []string{"alice"}, State: StatePresent}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.Status != tc.want {
				t.Errorf("Status = %q, want %q", plan.Status, tc.want)
			}
		})
	}
}

// recordingAclRunner answers the ACL probe with acl and records every other
// command it is asked to run.
func recordingAclRunner(probe, acl string) (func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error), func() []string) {
	var mu sync.Mutex
	var ran []string
	runner := func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if joined == probe {
			return subprocess.ExecCommandResponse{Stdout: acl}, nil
		}
		mu.Lock()
		defer mu.Unlock()
		ran = append(ran, joined)
		return subprocess.ExecCommandResponse{}, nil
	}
	return runner, func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), ran...)
	}
}

func TestAclAppSetAndClearRunOneCommand(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		task  AclAppTask
		want  []string
		state State
	}{
		{
			name:  "set",
			task:  AclAppTask{App: "web", Users: []string{"dave", "alice"}, State: StateSet},
			want:  []string{"--quiet acl:set-users web dave alice"},
			state: StateSet,
		},
		{
			name:  "clear",
			task:  AclAppTask{App: "web", State: StateClear},
			want:  []string{"--quiet acl:set-users web"},
			state: StateClear,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			runner, ran := recordingAclRunner(aclAppListKey, "bob\ncarol\n")
			result := tc.task.Execute(subprocess.ContextWithRunner(testCtx(), runner))
			if result.Error != nil {
				t.Fatalf("Execute: %v", result.Error)
			}
			if !result.Changed || result.State != tc.state {
				t.Errorf("Changed = %v, State = %q; want true, %q", result.Changed, result.State, tc.state)
			}
			if got := ran(); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ran %v, want %v", got, tc.want)
			}
		})
	}
}
