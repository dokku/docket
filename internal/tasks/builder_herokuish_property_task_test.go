package tasks

import (
	"strings"
	"testing"
)

func TestBuilderHerokuishPropertyTaskInvalidState(t *testing.T) {
	task := BuilderHerokuishPropertyTask{App: "test-app", Property: "allowed", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderHerokuishPropertyTaskMissingApp(t *testing.T) {
	task := BuilderHerokuishPropertyTask{Property: "allowed", Value: "true", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderHerokuishPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderHerokuishPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "allowed",
		Value:    "true",
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

func TestBuilderHerokuishPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderHerokuishPropertyTask{
		App:      "test-app",
		Property: "allowed",
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

func TestBuilderHerokuishPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderHerokuishPropertyTask{
		App:      "test-app",
		Property: "allowed",
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
