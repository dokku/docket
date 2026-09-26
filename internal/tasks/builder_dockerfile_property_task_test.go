package tasks

import (
	"strings"
	"testing"
)

func TestBuilderDockerfilePropertyTaskInvalidState(t *testing.T) {
	task := BuilderDockerfilePropertyTask{App: "test-app", Property: "dockerfile-path", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderDockerfilePropertyTaskMissingApp(t *testing.T) {
	task := BuilderDockerfilePropertyTask{Property: "dockerfile-path", Value: "Dockerfile", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderDockerfilePropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderDockerfilePropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "dockerfile-path",
		Value:    "Dockerfile",
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

func TestBuilderDockerfilePropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderDockerfilePropertyTask{
		App:      "test-app",
		Property: "dockerfile-path",
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

func TestBuilderDockerfilePropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderDockerfilePropertyTask{
		App:      "test-app",
		Property: "dockerfile-path",
		Value:    "Dockerfile",
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
