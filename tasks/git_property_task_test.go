package tasks

import (
	"strings"
	"testing"
)

func TestGitPropertyTaskInvalidState(t *testing.T) {
	task := GitPropertyTask{App: "test-app", Property: "deploy-branch", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGitPropertyTaskMissingApp(t *testing.T) {
	task := GitPropertyTask{Property: "deploy-branch", Value: "main", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestGitPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := GitPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "deploy-branch",
		Value:    "main",
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

func TestGitPropertyTaskPresentWithoutValue(t *testing.T) {
	task := GitPropertyTask{
		App:      "test-app",
		Property: "deploy-branch",
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

func TestGitPropertyTaskAbsentWithValue(t *testing.T) {
	task := GitPropertyTask{
		App:      "test-app",
		Property: "deploy-branch",
		Value:    "main",
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
