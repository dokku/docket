package tasks

import (
	"strings"
	"testing"
)

func TestNginxPropertyTaskInvalidState(t *testing.T) {
	task := NginxPropertyTask{App: "test-app", Property: "proxy-read-timeout", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestNginxPropertyTaskMissingApp(t *testing.T) {
	task := NginxPropertyTask{Property: "proxy-read-timeout", Value: "120s", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestNginxPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := NginxPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "proxy-read-timeout",
		Value:    "120s",
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestNginxPropertyTaskPresentWithoutValue(t *testing.T) {
	task := NginxPropertyTask{
		App:      "test-app",
		Property: "proxy-read-timeout",
		Value:    "",
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when present state has no value")
	}
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestNginxPropertyTaskAbsentWithValue(t *testing.T) {
	task := NginxPropertyTask{
		App:      "test-app",
		Property: "proxy-read-timeout",
		Value:    "120s",
		State:    StateAbsent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when absent state has a value")
	}
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// TestGetTasksNginxPropertyTaskParsedCorrectly decodes a property task from a
// recipe rather than building it in Go, which is the one thing the sibling
// tests here do not do. NginxPropertyTask is declared as `type NginxPropertyTask
// PropertyFields` (#454), so this is what proves the shared field set still
// yields the flat `app`/`global`/`property`/`value`/`state` recipe keys and that
// SetDefaults still fills `state`.
func TestGetTasksNginxPropertyTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set the nginx read timeout
      dokku_nginx_property:
        app: node-js-app
        property: proxy-read-timeout
        value: 120s
`)

	tasks, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set the nginx read timeout")
	if task == nil {
		t.Fatal("task 'set the nginx read timeout' not found")
	}

	npTask, ok := task.(*NginxPropertyTask)
	if !ok {
		t.Fatalf("task is not a NginxPropertyTask (type is %T)", task)
	}

	if npTask.App != "node-js-app" {
		t.Errorf("App = %q, want %q", npTask.App, "node-js-app")
	}
	if npTask.Global {
		t.Errorf("Global = true, want false")
	}
	if npTask.Property != "proxy-read-timeout" {
		t.Errorf("Property = %q, want %q", npTask.Property, "proxy-read-timeout")
	}
	if npTask.Value != "120s" {
		t.Errorf("Value = %q, want %q", npTask.Value, "120s")
	}
	if npTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", npTask.State)
	}
}
