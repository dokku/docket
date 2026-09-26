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
	if !strings.Contains(result.Error.Error(), "app is required") {
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
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
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
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
