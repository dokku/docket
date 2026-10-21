package tasks

import (
	"strings"
	"testing"
)

func TestTraefikPropertyTaskInvalidState(t *testing.T) {
	task := TraefikPropertyTask{Global: true, Property: "letsencrypt-email", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestTraefikPropertyTaskMissingApp(t *testing.T) {
	task := TraefikPropertyTask{Property: "letsencrypt-email", Value: "admin@example.com", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestTraefikPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := TraefikPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "letsencrypt-email",
		Value:    "admin@example.com",
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

func TestTraefikPropertyTaskPresentWithoutValue(t *testing.T) {
	task := TraefikPropertyTask{
		Global:   true,
		Property: "letsencrypt-email",
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

func TestTraefikPropertyTaskAbsentWithValue(t *testing.T) {
	task := TraefikPropertyTask{
		Global:   true,
		Property: "letsencrypt-email",
		Value:    "admin@example.com",
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
