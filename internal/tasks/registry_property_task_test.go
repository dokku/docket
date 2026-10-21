package tasks

import (
	"strings"
	"testing"
)

func TestRegistryPropertyTaskInvalidState(t *testing.T) {
	task := RegistryPropertyTask{App: "test-app", Property: "image-repo", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestRegistryPropertyTaskMissingApp(t *testing.T) {
	task := RegistryPropertyTask{Property: "image-repo", Value: "registry.example.com/app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestRegistryPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := RegistryPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "server",
		Value:    "registry.example.com",
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

func TestRegistryPropertyTaskPresentWithoutValue(t *testing.T) {
	task := RegistryPropertyTask{
		App:      "test-app",
		Property: "image-repo",
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

func TestRegistryPropertyTaskAbsentWithValue(t *testing.T) {
	task := RegistryPropertyTask{
		App:      "test-app",
		Property: "image-repo",
		Value:    "registry.example.com/app",
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
