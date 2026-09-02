package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestHttpAuthTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := HttpAuthTask{App: "test-app", Username: "admin", Password: "secret", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthTaskPresentWithoutUsername(t *testing.T) {
	t.Parallel()
	task := HttpAuthTask{App: "test-app", Password: "secret", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when present state has no username")
	}
	if !strings.Contains(result.Error.Error(), "username is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthTaskPresentWithoutPassword(t *testing.T) {
	t.Parallel()
	task := HttpAuthTask{App: "test-app", Username: "admin", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when present state has no password")
	}
	if !strings.Contains(result.Error.Error(), "password is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// TestHttpAuthTaskPresentWithoutCredentialsIsValid pins the paired rule: the
// plugin takes the credentials as optional trailing arguments and skips user
// initialization without them, so a credential-free enable is valid. This is
// what lets the exporter emit an app's auth state without duplicating the
// password input dokku_http_auth_user already lifts (#428).
func TestHttpAuthTaskPresentWithoutCredentialsIsValid(t *testing.T) {
	t.Parallel()
	task := HttpAuthTask{App: "test-app", State: StatePresent}
	if err := task.Validate(); err != nil {
		t.Errorf("credential-free present state should validate, got: %v", err)
	}
}

func TestHttpAuthTaskEnableOmitsEmptyCredentials(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"http-auth:report test-app --format json": `{"enabled":"false"}`,
	}))

	plan := HttpAuthTask{App: "test-app", State: StatePresent}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan: %v", plan.Error)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected 1 planned command, got %v", plan.Commands)
	}
	// The command line ends at the app name: no empty positional arguments
	// standing in for the credentials that were never supplied.
	if !strings.HasSuffix(plan.Commands[0], "http-auth:enable test-app") {
		t.Errorf("planned command should end at the app name, got %q", plan.Commands[0])
	}
}

func TestGetTasksHttpAuthTaskParsedCorrectly(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: enable http auth
      dokku_http_auth:
        app: test-app
        username: admin
        password: secret
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("enable http auth")
	if task == nil {
		t.Fatal("task 'enable http auth' not found")
	}

	haTask, ok := task.(*HttpAuthTask)
	if !ok {
		ht, ok2 := task.(HttpAuthTask)
		if !ok2 {
			t.Fatalf("task is not an HttpAuthTask (type is %T)", task)
		}
		haTask = &ht
	}

	if haTask.App != "test-app" {
		t.Errorf("App = %q, want %q", haTask.App, "test-app")
	}
	if haTask.Username != "admin" {
		t.Errorf("Username = %q, want %q", haTask.Username, "admin")
	}
	if haTask.Password != "secret" {
		t.Errorf("Password = %q, want %q", haTask.Password, "secret")
	}
	if haTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", haTask.State)
	}
}
