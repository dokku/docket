package tasks

import (
	"strings"
	"testing"
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
