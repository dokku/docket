package tasks

import (
	"strings"
	"testing"
)

func TestBuildsPropertyTaskInvalidState(t *testing.T) {
	task := BuildsPropertyTask{App: "test-app", Property: "retention", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuildsPropertyTaskMissingApp(t *testing.T) {
	task := BuildsPropertyTask{Property: "retention", Value: "50", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuildsPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuildsPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "retention",
		Value:    "50",
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

func TestBuildsPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuildsPropertyTask{
		App:      "test-app",
		Property: "retention",
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

func TestBuildsPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuildsPropertyTask{
		App:      "test-app",
		Property: "retention",
		Value:    "50",
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
