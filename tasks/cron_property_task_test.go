package tasks

import (
	"strings"
	"testing"
)

func TestCronPropertyTaskInvalidState(t *testing.T) {
	task := CronPropertyTask{App: "test-app", Property: "mailto", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestCronPropertyTaskMissingApp(t *testing.T) {
	task := CronPropertyTask{Property: "mailto", Value: "ops@example.com", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestCronPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := CronPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "mailto",
		Value:    "ops@example.com",
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCronPropertyTaskPresentWithoutValue(t *testing.T) {
	task := CronPropertyTask{
		App:      "test-app",
		Property: "maintenance",
		Value:    "",
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no value")
	}
	if !strings.Contains(result.Error.Error(), "invalid without a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCronPropertyTaskAbsentWithValue(t *testing.T) {
	task := CronPropertyTask{
		App:      "test-app",
		Property: "maintenance",
		Value:    "true",
		State:    StateAbsent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has a value")
	}
	if !strings.Contains(result.Error.Error(), "invalid with a value") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
