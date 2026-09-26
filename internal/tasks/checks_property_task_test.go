package tasks

import (
	"strings"
	"testing"
)

func TestChecksPropertyTaskInvalidState(t *testing.T) {
	task := ChecksPropertyTask{App: "test-app", Property: "wait-to-retire", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestChecksPropertyTaskMissingApp(t *testing.T) {
	task := ChecksPropertyTask{Property: "wait-to-retire", Value: "60", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestChecksPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := ChecksPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "wait-to-retire",
		Value:    "60",
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

func TestChecksPropertyTaskPresentWithoutValue(t *testing.T) {
	task := ChecksPropertyTask{
		App:      "test-app",
		Property: "wait-to-retire",
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

func TestChecksPropertyTaskAbsentWithValue(t *testing.T) {
	task := ChecksPropertyTask{
		App:      "test-app",
		Property: "wait-to-retire",
		Value:    "60",
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
