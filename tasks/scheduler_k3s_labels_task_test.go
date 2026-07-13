package tasks

import (
	"strings"
	"testing"
)

func TestSchedulerK3sLabelsTaskInvalidState(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": "edge"},
		State:        "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sLabelsTaskMissingApp(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": "edge"},
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "app is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sLabelsTaskGlobalWithAppSet(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		Global:       true,
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": "edge"},
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sLabelsTaskMissingResourceType(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:    "test-app",
		Labels: map[string]string{"tier": "edge"},
		State:  StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when resource_type is empty")
	}
	if !strings.Contains(result.Error.Error(), "resource_type is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sLabelsTaskPresentWithoutLabels(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		ResourceType: "deployment",
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no labels")
	}
	if !strings.Contains(result.Error.Error(), "'labels' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sLabelsTaskAbsentWithoutLabels(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		ResourceType: "deployment",
		State:        StateAbsent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has no labels")
	}
	if !strings.Contains(result.Error.Error(), "'labels' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sLabelsTaskEmptyLabelKey(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		ResourceType: "deployment",
		Labels:       map[string]string{"": "edge"},
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when a label key is empty")
	}
	if !strings.Contains(result.Error.Error(), "label keys must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sLabelsTaskPresentEmptyValueRejected(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": ""},
		State:        StatePresent,
	}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected error when a present-state label value is empty")
	}
	if !strings.Contains(err.Error(), "label values must not be empty for state 'present'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSchedulerK3sLabelsTaskAbsentEmptyValueAllowed(t *testing.T) {
	task := SchedulerK3sLabelsTask{
		App:          "test-app",
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": ""},
		State:        StateAbsent,
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("absent-state empty value should be allowed (clears the key), got %v", err)
	}
}
