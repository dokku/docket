package tasks

import (
	"strings"
	"testing"
)

func TestBuilderPackPropertyTaskInvalidState(t *testing.T) {
	task := BuilderPackPropertyTask{App: "test-app", Property: "projecttoml-path", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderPackPropertyTaskMissingApp(t *testing.T) {
	task := BuilderPackPropertyTask{Property: "projecttoml-path", Value: "project.toml", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderPackPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderPackPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "projecttoml-path",
		Value:    "project.toml",
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

func TestBuilderPackPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderPackPropertyTask{
		App:      "test-app",
		Property: "projecttoml-path",
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

func TestBuilderPackPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderPackPropertyTask{
		App:      "test-app",
		Property: "projecttoml-path",
		Value:    "project.toml",
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
