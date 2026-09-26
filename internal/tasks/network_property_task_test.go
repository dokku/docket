package tasks

import (
	"strings"
	"testing"
)

func TestNetworkPropertyTaskInvalidState(t *testing.T) {
	task := NetworkPropertyTask{App: "test-app", Property: "attach-post-create", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestNetworkPropertyTaskMissingApp(t *testing.T) {
	task := NetworkPropertyTask{Property: "attach-post-create", Value: "test-network", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestNetworkPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := NetworkPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "bind-all-interfaces",
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

func TestNetworkPropertyTaskPresentWithoutValue(t *testing.T) {
	task := NetworkPropertyTask{
		App:      "test-app",
		Property: "bind-all-interfaces",
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

func TestNetworkPropertyTaskAbsentWithValue(t *testing.T) {
	task := NetworkPropertyTask{
		App:      "test-app",
		Property: "bind-all-interfaces",
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
