package tasks

import (
	"strings"
	"testing"
)

func TestRegistryAuthTaskInvalidState(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestRegistryAuthTaskMissingApp(t *testing.T) {
	task := RegistryAuthTask{Server: "docker.io", Username: "u", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestRegistryAuthTaskGlobalWithApp(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Global: true, Server: "docker.io", Username: "u", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestRegistryAuthTaskMissingServer(t *testing.T) {
	for _, st := range []State{StatePresent, StateAbsent} {
		task := RegistryAuthTask{App: "test-app", Username: "u", Password: "p", State: st}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("expected error with empty server (state=%s)", st)
		}
		if !strings.Contains(result.Error.Error(), "'server' is required") {
			t.Errorf("state=%s: unexpected error: %v", st, result.Error)
		}
	}
}

func TestRegistryAuthTaskPresentMissingUsername(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without username should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestRegistryAuthTaskPresentMissingPassword(t *testing.T) {
	task := RegistryAuthTask{App: "test-app", Server: "docker.io", Username: "u", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without password should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksRegistryAuthTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: log in to ghcr
      dokku_registry_auth:
        app: test-app
        server: ghcr.io
        username: deploy-bot
        password: ghp_examplepat
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("log in to ghcr")
	if task == nil {
		t.Fatal("task 'log in to ghcr' not found")
	}

	authTask, ok := task.(*RegistryAuthTask)
	if !ok {
		t.Fatalf("task is not a RegistryAuthTask (type is %T)", task)
	}
	if authTask.App != "test-app" {
		t.Errorf("App = %q, want %q", authTask.App, "test-app")
	}
	if authTask.Server != "ghcr.io" {
		t.Errorf("Server = %q, want %q", authTask.Server, "ghcr.io")
	}
	if authTask.Username != "deploy-bot" {
		t.Errorf("Username = %q, want %q", authTask.Username, "deploy-bot")
	}
	if authTask.Password != "ghp_examplepat" {
		t.Errorf("Password = %q, want %q", authTask.Password, "ghp_examplepat")
	}
	if authTask.State != StatePresent {
		t.Errorf("State = %q, want %q", authTask.State, StatePresent)
	}
}
