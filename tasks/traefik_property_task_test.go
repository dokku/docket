package tasks

import (
	"strings"
	"testing"
)

func TestTraefikPropertyTaskInvalidState(t *testing.T) {
	task := TraefikPropertyTask{Global: true, Property: "letsencrypt-email", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestTraefikPropertyTaskMissingApp(t *testing.T) {
	task := TraefikPropertyTask{Property: "letsencrypt-email", Value: "admin@example.com", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestTraefikPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := TraefikPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "letsencrypt-email",
		Value:    "admin@example.com",
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

func TestTraefikPropertyTaskPresentWithoutValue(t *testing.T) {
	task := TraefikPropertyTask{
		Global:   true,
		Property: "letsencrypt-email",
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

func TestTraefikPropertyTaskAbsentWithValue(t *testing.T) {
	task := TraefikPropertyTask{
		Global:   true,
		Property: "letsencrypt-email",
		Value:    "admin@example.com",
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
