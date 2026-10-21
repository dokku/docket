package tasks

import (
	"strings"
	"testing"
)

func TestSchedulerDockerLocalPropertyTaskInvalidState(t *testing.T) {
	task := SchedulerDockerLocalPropertyTask{App: "test-app", Property: "init-process", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerDockerLocalPropertyTaskMissingApp(t *testing.T) {
	task := SchedulerDockerLocalPropertyTask{Property: "init-process", Value: "true", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "exactly one of 'app' or 'global' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerDockerLocalPropertyTaskPresentWithoutValue(t *testing.T) {
	task := SchedulerDockerLocalPropertyTask{
		App:      "test-app",
		Property: "init-process",
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

func TestSchedulerDockerLocalPropertyTaskAbsentWithValue(t *testing.T) {
	task := SchedulerDockerLocalPropertyTask{
		App:      "test-app",
		Property: "init-process",
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

func TestSchedulerDockerLocalPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := SchedulerDockerLocalPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "parallel-schedule-count",
		Value:    "4",
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

func TestSchedulerDockerLocalPropertyTaskGlobalValidates(t *testing.T) {
	task := SchedulerDockerLocalPropertyTask{Global: true, Property: "init-process", Value: "true", State: StatePresent}
	if err := task.Validate(); err != nil {
		t.Errorf("global init-process should validate, got %v", err)
	}
}
