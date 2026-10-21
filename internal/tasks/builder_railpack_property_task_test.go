package tasks

import (
	"strings"
	"testing"
)

func TestBuilderRailpackPropertyTaskInvalidState(t *testing.T) {
	task := BuilderRailpackPropertyTask{App: "test-app", Property: "railpackjson-path", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuilderRailpackPropertyTaskMissingApp(t *testing.T) {
	task := BuilderRailpackPropertyTask{Property: "railpackjson-path", Value: "railpack.json", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestBuilderRailpackPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := BuilderRailpackPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "railpackjson-path",
		Value:    "railpack.json",
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

func TestBuilderRailpackPropertyTaskPresentWithoutValue(t *testing.T) {
	task := BuilderRailpackPropertyTask{
		App:      "test-app",
		Property: "railpackjson-path",
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

func TestBuilderRailpackPropertyTaskAbsentWithValue(t *testing.T) {
	task := BuilderRailpackPropertyTask{
		App:      "test-app",
		Property: "railpackjson-path",
		Value:    "railpack.json",
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
