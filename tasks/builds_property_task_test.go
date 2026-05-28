package tasks

import (
	"strings"
	"testing"
)

func TestBuildsPropertyTaskInvalidState(t *testing.T) {
	task := BuildsPropertyTask{App: "test-app", Property: "retention", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuildsPropertyTaskMissingApp(t *testing.T) {
	task := BuildsPropertyTask{Property: "retention", Value: "50", State: StatePresent}
	result := task.Execute()
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
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
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
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no value")
	}
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
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
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has a value")
	}
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
