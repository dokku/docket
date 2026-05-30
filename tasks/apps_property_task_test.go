package tasks

import (
	"strings"
	"testing"
)

func TestAppsPropertyTaskInvalidState(t *testing.T) {
	task := AppsPropertyTask{App: "test-app", Property: "deploy-source", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestAppsPropertyTaskMissingApp(t *testing.T) {
	task := AppsPropertyTask{Property: "deploy-source", Value: "git", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestAppsPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := AppsPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "disable-autocreation",
		Value:    "true",
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

func TestAppsPropertyTaskPresentWithoutValue(t *testing.T) {
	task := AppsPropertyTask{
		App:      "test-app",
		Property: "deploy-source",
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

func TestAppsPropertyTaskAbsentWithValue(t *testing.T) {
	task := AppsPropertyTask{
		App:      "test-app",
		Property: "deploy-source",
		Value:    "git",
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
