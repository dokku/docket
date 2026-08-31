package tasks

import (
	"strings"
	"testing"
)

func TestLetsencryptTaskInvalidState(t *testing.T) {
	task := LetsencryptTask{App: "test-app", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestLetsencryptTaskPresentMissingApp(t *testing.T) {
	task := LetsencryptTask{State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestLetsencryptTaskAbsentMissingApp(t *testing.T) {
	task := LetsencryptTask{State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksLetsencryptTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: enable letsencrypt
      dokku_letsencrypt:
        app: test-app
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("enable letsencrypt")
	if task == nil {
		t.Fatal("task 'enable letsencrypt' not found")
	}

	leTask, ok := task.(*LetsencryptTask)
	if !ok {
		t.Fatalf("task is not a LetsencryptTask (type is %T)", task)
	}
	if leTask.App != "test-app" {
		t.Errorf("App = %q, want %q", leTask.App, "test-app")
	}
	if leTask.State != StatePresent {
		t.Errorf("State = %q, want %q", leTask.State, StatePresent)
	}
}
