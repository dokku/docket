package tasks

import (
	"strings"
	"testing"
)

func TestLetsencryptPropertyTaskInvalidState(t *testing.T) {
	task := LetsencryptPropertyTask{App: "test-app", Property: "email", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestLetsencryptPropertyTaskMissingApp(t *testing.T) {
	task := LetsencryptPropertyTask{Property: "email", Value: "admin@example.com", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestLetsencryptPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := LetsencryptPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "email",
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

func TestLetsencryptPropertyTaskPresentWithoutValue(t *testing.T) {
	task := LetsencryptPropertyTask{
		App:      "test-app",
		Property: "email",
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

func TestLetsencryptPropertyTaskAbsentWithValue(t *testing.T) {
	task := LetsencryptPropertyTask{
		App:      "test-app",
		Property: "email",
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
