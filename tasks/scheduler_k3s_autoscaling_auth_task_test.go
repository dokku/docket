package tasks

import (
	"strings"
	"testing"
)

func TestSchedulerK3sAutoscalingAuthTaskInvalidState(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sAutoscalingAuthTaskMissingApp(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "app is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskGlobalWithAppSet(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Global:   true,
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
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

func TestSchedulerK3sAutoscalingAuthTaskMissingTrigger(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when trigger is empty")
	}
	if !strings.Contains(result.Error.Error(), "trigger is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskPresentWithoutMetadata(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:     "test-app",
		Trigger: "aws-secret-manager",
		State:   StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when present state has no metadata")
	}
	if !strings.Contains(result.Error.Error(), "'metadata' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskAbsentWithoutMetadata(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:     "test-app",
		Trigger: "aws-secret-manager",
		State:   StateAbsent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when absent state has no metadata")
	}
	if !strings.Contains(result.Error.Error(), "'metadata' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskEmptyMetadataKey(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"": "value"},
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when a metadata key is empty")
	}
	if !strings.Contains(result.Error.Error(), "metadata keys must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}
