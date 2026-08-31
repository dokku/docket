package tasks

import (
	"strings"
	"testing"
)

func TestHaproxyPropertyTaskInvalidState(t *testing.T) {
	task := HaproxyPropertyTask{Global: true, Property: "letsencrypt-email", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHaproxyPropertyTaskMissingApp(t *testing.T) {
	task := HaproxyPropertyTask{Property: "letsencrypt-email", Value: "admin@example.com", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestHaproxyPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := HaproxyPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "letsencrypt-email",
		Value:    "admin@example.com",
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

func TestHaproxyPropertyTaskPresentWithoutValue(t *testing.T) {
	task := HaproxyPropertyTask{
		Global:   true,
		Property: "letsencrypt-email",
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

func TestHaproxyPropertyTaskAbsentWithValue(t *testing.T) {
	task := HaproxyPropertyTask{
		Global:   true,
		Property: "letsencrypt-email",
		Value:    "admin@example.com",
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
