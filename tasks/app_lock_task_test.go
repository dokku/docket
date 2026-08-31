package tasks

import (
	"strings"
	"testing"
)

func TestAppLockTaskInvalidState(t *testing.T) {
	task := AppLockTask{App: "test-app", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestAppLockTaskPresentMissingApp(t *testing.T) {
	task := AppLockTask{State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestAppLockTaskAbsentMissingApp(t *testing.T) {
	task := AppLockTask{State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksAppLockTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: lock app
      dokku_app_lock:
        app: test-app
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("lock app")
	if task == nil {
		t.Fatal("task 'lock app' not found")
	}

	lockTask, ok := task.(*AppLockTask)
	if !ok {
		t.Fatalf("task is not an AppLockTask (type is %T)", task)
	}
	if lockTask.App != "test-app" {
		t.Errorf("App = %q, want %q", lockTask.App, "test-app")
	}
	if lockTask.State != StatePresent {
		t.Errorf("State = %q, want %q", lockTask.State, StatePresent)
	}
}
