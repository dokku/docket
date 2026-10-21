package tasks

import (
	"strings"
	"testing"
)

func TestOpenrestyPropertyTaskInvalidState(t *testing.T) {
	task := OpenrestyPropertyTask{App: "test-app", Property: "proxy-read-timeout", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestOpenrestyPropertyTaskMissingApp(t *testing.T) {
	task := OpenrestyPropertyTask{Property: "proxy-read-timeout", Value: "120s", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestOpenrestyPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := OpenrestyPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "bind-address-ipv4",
		Value:    "0.0.0.0",
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

func TestOpenrestyPropertyTaskPresentWithoutValue(t *testing.T) {
	task := OpenrestyPropertyTask{
		App:      "test-app",
		Property: "proxy-read-timeout",
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

func TestOpenrestyPropertyTaskAbsentWithValue(t *testing.T) {
	task := OpenrestyPropertyTask{
		App:      "test-app",
		Property: "proxy-read-timeout",
		Value:    "120s",
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
