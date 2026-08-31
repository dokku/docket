package tasks

import (
	"strings"
	"testing"
)

func TestGitAuthTaskInvalidState(t *testing.T) {
	task := GitAuthTask{Host: "github.com", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGitAuthTaskMissingHost(t *testing.T) {
	for _, st := range []State{StatePresent, StateAbsent} {
		task := GitAuthTask{Username: "u", Password: "p", State: st}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("Execute without host (state=%s) should return an error", st)
		}
		if !strings.Contains(result.Error.Error(), "'host' is required") {
			t.Errorf("state=%s: unexpected error: %v", st, result.Error)
		}
	}
}

func TestGitAuthTaskPresentMissingUsername(t *testing.T) {
	task := GitAuthTask{Host: "github.com", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without username should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGitAuthTaskPresentMissingPassword(t *testing.T) {
	task := GitAuthTask{Host: "github.com", Username: "u", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without password should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksGitAuthTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: configure git auth
      dokku_git_auth:
        host: github.com
        username: deploy-bot
        password: ghp_examplepat
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("configure git auth")
	if task == nil {
		t.Fatal("task 'configure git auth' not found")
	}

	authTask, ok := task.(*GitAuthTask)
	if !ok {
		t.Fatalf("task is not a GitAuthTask (type is %T)", task)
	}
	if authTask.Host != "github.com" {
		t.Errorf("Host = %q, want %q", authTask.Host, "github.com")
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
