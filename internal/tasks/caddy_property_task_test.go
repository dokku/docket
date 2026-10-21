package tasks

import (
	"strings"
	"testing"
)

func TestCaddyPropertyTaskInvalidState(t *testing.T) {
	task := CaddyPropertyTask{App: "test-app", Property: "tls-internal", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestCaddyPropertyTaskMissingApp(t *testing.T) {
	task := CaddyPropertyTask{Property: "tls-internal", Value: "true", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestCaddyPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := CaddyPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "tls-internal",
		Value:    "true",
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

func TestCaddyPropertyTaskPresentWithoutValue(t *testing.T) {
	task := CaddyPropertyTask{
		App:      "test-app",
		Property: "tls-internal",
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

func TestCaddyPropertyTaskAbsentWithValue(t *testing.T) {
	task := CaddyPropertyTask{
		App:      "test-app",
		Property: "tls-internal",
		Value:    "true",
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
