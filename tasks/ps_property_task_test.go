package tasks

import (
	"strings"
	"testing"
)

func TestPsPropertyTaskInvalidState(t *testing.T) {
	task := PsPropertyTask{App: "test-app", Property: "restart-policy", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestPsPropertyTaskMissingApp(t *testing.T) {
	task := PsPropertyTask{Property: "restart-policy", Value: "on-failure:5", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestPsPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := PsPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "restart-policy",
		Value:    "on-failure:5",
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

func TestPsPropertyTaskPresentWithoutValue(t *testing.T) {
	task := PsPropertyTask{
		App:      "test-app",
		Property: "restart-policy",
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

func TestPsPropertyTaskAbsentWithValue(t *testing.T) {
	task := PsPropertyTask{
		App:      "test-app",
		Property: "restart-policy",
		Value:    "on-failure:5",
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
