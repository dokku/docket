package tasks

import (
	"strings"
	"testing"
)

func TestAppCloneTaskInvalidState(t *testing.T) {
	task := AppCloneTask{App: "new-app", SourceApp: "old-app", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestAppCloneTaskMissingApp(t *testing.T) {
	task := AppCloneTask{SourceApp: "old-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestAppCloneTaskMissingSourceApp(t *testing.T) {
	task := AppCloneTask{App: "new-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without source_app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'source_app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksAppCloneTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: clone app
      dokku_app_clone:
        app: new-app
        source_app: old-app
        skip_deploy: true
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("clone app")
	if task == nil {
		t.Fatal("task 'clone app' not found")
	}

	cloneTask, ok := task.(*AppCloneTask)
	if !ok {
		t.Fatalf("task is not an AppCloneTask (type is %T)", task)
	}
	if cloneTask.App != "new-app" {
		t.Errorf("App = %q, want %q", cloneTask.App, "new-app")
	}
	if cloneTask.SourceApp != "old-app" {
		t.Errorf("SourceApp = %q, want %q", cloneTask.SourceApp, "old-app")
	}
	if !cloneTask.SkipDeploy {
		t.Errorf("SkipDeploy = false, want true")
	}
}
