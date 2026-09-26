package tasks

import (
	"strings"
	"testing"
)

func TestBuildpacksPropertyTaskInvalidState(t *testing.T) {
	task := BuildpacksPropertyTask{App: "test-app", Property: "stack", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuildpacksPropertyTaskMissingApp(t *testing.T) {
	task := BuildpacksPropertyTask{Property: "stack", Value: "gliderlabs/herokuish:latest", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuildpacksPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuildpacksPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "stack",
		Value:    "gliderlabs/herokuish:latest",
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

func TestBuildpacksPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuildpacksPropertyTask{
		App:      "test-app",
		Property: "stack",
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

func TestBuildpacksPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuildpacksPropertyTask{
		App:      "test-app",
		Property: "stack",
		Value:    "gliderlabs/herokuish:latest",
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
