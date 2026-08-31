package tasks

import (
	"strings"
	"testing"
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
