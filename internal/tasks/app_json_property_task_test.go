package tasks

import (
	"strings"
	"testing"
)

func TestAppJsonPropertyTaskInvalidState(t *testing.T) {
	task := AppJsonPropertyTask{App: "test-app", Property: "appjson-path", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestAppJsonPropertyTaskMissingApp(t *testing.T) {
	task := AppJsonPropertyTask{Property: "appjson-path", Value: "app.json", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestAppJsonPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := AppJsonPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "appjson-path",
		Value:    "app.json",
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "'app' and 'global' are mutually exclusive") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestAppJsonPropertyTaskPresentWithoutValue(t *testing.T) {
	task := AppJsonPropertyTask{
		App:      "test-app",
		Property: "appjson-path",
		Value:    "",
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when present state has no value")
	}
	if !strings.Contains(result.Error.Error(), "'value' is required for state 'present'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestAppJsonPropertyTaskAbsentWithValue(t *testing.T) {
	task := AppJsonPropertyTask{
		App:      "test-app",
		Property: "appjson-path",
		Value:    "app.json",
		State:    StateAbsent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when absent state has a value")
	}
	if !strings.Contains(result.Error.Error(), "'value' must not be set for state 'absent'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
