package tasks

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestAclServiceTaskInvalidState(t *testing.T) {
	task := AclServiceTask{Service: "my-redis", Type: "redis", Users: []string{"alice"}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestAclServiceTaskMissingService(t *testing.T) {
	for _, st := range []State{StatePresent, StateAbsent} {
		task := AclServiceTask{Type: "redis", Users: []string{"alice"}, State: st}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("Execute without service (state=%s) should return an error", st)
		}
		if !strings.Contains(result.Error.Error(), "'service' is required") {
			t.Errorf("state=%s: unexpected error: %v", st, result.Error)
		}
	}
}

func TestAclServiceTaskMissingType(t *testing.T) {
	for _, st := range []State{StatePresent, StateAbsent} {
		task := AclServiceTask{Service: "my-redis", Users: []string{"alice"}, State: st}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("Execute without type (state=%s) should return an error", st)
		}
		if !strings.Contains(result.Error.Error(), "'type' is required") {
			t.Errorf("state=%s: unexpected error: %v", st, result.Error)
		}
	}
}

func TestAclServiceTaskPresentEmptyUsers(t *testing.T) {
	task := AclServiceTask{Service: "my-redis", Type: "redis", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty users and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'users' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksAclServiceTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: grant access
      dokku_acl_service:
        service: my-redis
        type: redis
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

	aclTask, ok := task.(*AclServiceTask)
	if !ok {
		t.Fatalf("task is not an AclServiceTask (type is %T)", task)
	}
	if aclTask.Service != "my-redis" {
		t.Errorf("Service = %q, want %q", aclTask.Service, "my-redis")
	}
	if aclTask.Type != "redis" {
		t.Errorf("Type = %q, want %q", aclTask.Type, "redis")
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

// aclServiceRunner answers `acl:list-service redis my-redis` with the given
// streams, the way dokku-acl 2.0.0+ (stdout) or 1.5.1 and earlier (stderr)
// would.
func aclServiceRunner(stdout, stderr string, err error) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if strings.Join(in.Args, " ") != "--quiet acl:list-service redis my-redis" {
			return subprocess.ExecCommandResponse{}, fmt.Errorf("unexpected command: %v", in.Args)
		}
		return subprocess.ExecCommandResponse{Stdout: stdout, Stderr: stderr}, err
	}
}

func TestGetAclServiceUsersReadsBothStreams(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name   string
		stdout string
		stderr string
		want   []string
	}{
		{name: "stdout (dokku-acl 2.0.0+)", stdout: "bob\nalice\n", want: []string{"alice", "bob"}},
		{name: "stderr (dokku-acl 1.5.1 and earlier)", stderr: "bob\nalice\n", want: []string{"alice", "bob"}},
		{name: "empty acl", want: []string{}},
		{name: "stdout wins over stderr", stdout: "alice\n", stderr: "mallory\n", want: []string{"alice"}},
		{name: "blank lines ignored", stdout: "\n  alice  \n\n bob\n", want: []string{"alice", "bob"}},
		{name: "whitespace-only stdout falls back", stdout: " \n", stderr: "alice\n", want: []string{"alice"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), aclServiceRunner(tc.stdout, tc.stderr, nil))
			got, err := getAclServiceUsers(ctx, "redis", "my-redis")
			if err != nil {
				t.Fatalf("getAclServiceUsers: %v", err)
			}
			if users := sortedSetKeys(got); !reflect.DeepEqual(users, tc.want) {
				t.Errorf("users = %v, want %v", users, tc.want)
			}
		})
	}
}

func TestGetAclServiceUsersPropagatesError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), aclServiceRunner("", "", errors.New("boom")))
	if _, err := getAclServiceUsers(ctx, "redis", "my-redis"); err == nil {
		t.Fatal("expected the runner error to be returned")
	}
}

func TestAclServiceTaskPlanInSyncFromStdout(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), aclServiceRunner("alice\nbob\n", "", nil))
	task := AclServiceTask{Service: "my-redis", Type: "redis", Users: []string{"alice"}, State: StatePresent}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("expected no drift when the ACL is read from stdout, got status %v reason %q", plan.Status, plan.Reason)
	}
}
