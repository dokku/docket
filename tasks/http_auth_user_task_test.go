package tasks

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestHttpAuthUserTaskInvalidState(t *testing.T) {
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Username: "admin", Password: "secret"}}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthUserTaskPresentMissingApp(t *testing.T) {
	task := HttpAuthUserTask{Users: []HttpAuthUser{{Username: "admin", Password: "secret"}}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskAbsentMissingApp(t *testing.T) {
	task := HttpAuthUserTask{State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskPresentEmptyUsers(t *testing.T) {
	task := HttpAuthUserTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty users and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'users' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskPresentWithoutPassword(t *testing.T) {
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Username: "admin"}}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when a present user has no password")
	}
	if !strings.Contains(result.Error.Error(), "'password' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskMissingUsername(t *testing.T) {
	task := HttpAuthUserTask{App: "test-app", Users: []HttpAuthUser{{Password: "secret"}}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when a user has no username")
	}
	if !strings.Contains(result.Error.Error(), "'username' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthUserTaskSensitiveValues(t *testing.T) {
	task := HttpAuthUserTask{
		App: "test-app",
		Users: []HttpAuthUser{
			{Username: "admin", Password: "secret"},
			{Username: "ops", Password: "hunter2"},
			{Username: "guest"},
		},
	}
	got := task.SensitiveValues()
	sort.Strings(got)
	want := []string{"hunter2", "secret"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("SensitiveValues() = %v, want %v", got, want)
	}
}

func TestGetTasksHttpAuthUserTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: add http auth users
      dokku_http_auth_user:
        app: test-app
        update_password: true
        users:
          - username: admin
            password: secret
          - username: ops
            password: hunter2
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("add http auth users")
	if task == nil {
		t.Fatal("task 'add http auth users' not found")
	}

	hauTask, ok := task.(*HttpAuthUserTask)
	if !ok {
		ht, ok2 := task.(HttpAuthUserTask)
		if !ok2 {
			t.Fatalf("task is not an HttpAuthUserTask (type is %T)", task)
		}
		hauTask = &ht
	}

	if hauTask.App != "test-app" {
		t.Errorf("App = %q, want %q", hauTask.App, "test-app")
	}
	// UpdatePassword is a *bool, so an explicit update_password: true survives decoding.
	if hauTask.UpdatePassword == nil || !*hauTask.UpdatePassword {
		t.Error("UpdatePassword = false, want true")
	}
	if len(hauTask.Users) != 2 {
		t.Fatalf("expected 2 users, got %d", len(hauTask.Users))
	}
	if hauTask.Users[0].Username != "admin" || hauTask.Users[0].Password != "secret" {
		t.Errorf("Users[0] = %+v, want {admin secret}", hauTask.Users[0])
	}
	if hauTask.Users[1].Username != "ops" || hauTask.Users[1].Password != "hunter2" {
		t.Errorf("Users[1] = %+v, want {ops hunter2}", hauTask.Users[1])
	}
	if hauTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", hauTask.State)
	}
}
