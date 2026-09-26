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
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
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
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
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
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
