package tasks

import (
	"strings"
	"testing"
)

func TestBuilderPropertyTaskInvalidState(t *testing.T) {
	task := BuilderPropertyTask{App: "test-app", Property: "selected", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderPropertyTaskMissingApp(t *testing.T) {
	task := BuilderPropertyTask{Property: "selected", Value: "dockerfile", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "selected",
		Value:    "dockerfile",
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

func TestBuilderPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderPropertyTask{
		App:      "test-app",
		Property: "selected",
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

func TestBuilderPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderPropertyTask{
		App:      "test-app",
		Property: "selected",
		Value:    "dockerfile",
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
