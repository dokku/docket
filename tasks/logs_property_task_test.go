package tasks

import (
	"strings"
	"testing"
)

func TestLogsPropertyTaskInvalidState(t *testing.T) {
	task := LogsPropertyTask{App: "test-app", Property: "max-size", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestLogsPropertyTaskMissingApp(t *testing.T) {
	task := LogsPropertyTask{Property: "max-size", Value: "100m", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestLogsPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := LogsPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "max-size",
		Value:    "100m",
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

func TestLogsPropertyTaskPresentWithoutValue(t *testing.T) {
	task := LogsPropertyTask{
		App:      "test-app",
		Property: "max-size",
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

func TestLogsPropertyTaskAbsentWithValue(t *testing.T) {
	task := LogsPropertyTask{
		App:      "test-app",
		Property: "max-size",
		Value:    "100m",
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
