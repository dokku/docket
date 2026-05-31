package tasks

import (
	"strings"
	"testing"
)

func TestSchedulerK3sAnnotationsTaskInvalidState(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		App:          "test-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"prometheus.io/scrape": "true"},
		State:        "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sAnnotationsTaskMissingApp(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		ResourceType: "deployment",
		Annotations:  map[string]string{"prometheus.io/scrape": "true"},
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

func TestSchedulerK3sAnnotationsTaskGlobalWithAppSet(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		App:          "test-app",
		Global:       true,
		ResourceType: "deployment",
		Annotations:  map[string]string{"prometheus.io/scrape": "true"},
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

func TestSchedulerK3sAnnotationsTaskMissingResourceType(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		App:         "test-app",
		Annotations: map[string]string{"prometheus.io/scrape": "true"},
		State:       StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when resource_type is empty")
	}
	if !strings.Contains(result.Error.Error(), "resource_type is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAnnotationsTaskPresentWithoutAnnotations(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		App:          "test-app",
		ResourceType: "deployment",
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no annotations")
	}
	if !strings.Contains(result.Error.Error(), "'annotations' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAnnotationsTaskAbsentWithoutAnnotations(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		App:          "test-app",
		ResourceType: "deployment",
		State:        StateAbsent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has no annotations")
	}
	if !strings.Contains(result.Error.Error(), "'annotations' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAnnotationsTaskEmptyAnnotationKey(t *testing.T) {
	task := SchedulerK3sAnnotationsTask{
		App:          "test-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"": "true"},
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when an annotation key is empty")
	}
	if !strings.Contains(result.Error.Error(), "annotation keys must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
