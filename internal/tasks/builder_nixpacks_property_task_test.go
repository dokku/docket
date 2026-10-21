package tasks

import (
	"strings"
	"testing"
)

func TestBuilderNixpacksPropertyTaskInvalidState(t *testing.T) {
	task := BuilderNixpacksPropertyTask{App: "test-app", Property: "nixpackstoml-path", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderNixpacksPropertyTaskMissingApp(t *testing.T) {
	task := BuilderNixpacksPropertyTask{Property: "nixpackstoml-path", Value: "nixpacks.toml", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderNixpacksPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderNixpacksPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "nixpackstoml-path",
		Value:    "nixpacks.toml",
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

func TestBuilderNixpacksPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderNixpacksPropertyTask{
		App:      "test-app",
		Property: "nixpackstoml-path",
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

func TestBuilderNixpacksPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderNixpacksPropertyTask{
		App:      "test-app",
		Property: "nixpackstoml-path",
		Value:    "nixpacks.toml",
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
