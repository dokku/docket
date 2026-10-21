package tasks

import (
	"strings"
	"testing"
)

func TestSchedulerPropertyTaskInvalidState(t *testing.T) {
	task := SchedulerPropertyTask{App: "test-app", Property: "selected", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerPropertyTaskMissingApp(t *testing.T) {
	task := SchedulerPropertyTask{Property: "selected", Value: "docker-local", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestSchedulerPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := SchedulerPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "selected",
		Value:    "docker-local",
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

func TestSchedulerPropertyTaskPresentWithoutValue(t *testing.T) {
	task := SchedulerPropertyTask{
		App:      "test-app",
		Property: "selected",
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

func TestSchedulerPropertyTaskAbsentWithValue(t *testing.T) {
	task := SchedulerPropertyTask{
		App:      "test-app",
		Property: "selected",
		Value:    "docker-local",
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
