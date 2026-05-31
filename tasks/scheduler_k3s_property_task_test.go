package tasks

import (
	"strings"
	"testing"
)

func TestSchedulerK3sPropertyTaskInvalidState(t *testing.T) {
	task := SchedulerK3sPropertyTask{App: "test-app", Property: "deploy-timeout", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sPropertyTaskMissingApp(t *testing.T) {
	task := SchedulerK3sPropertyTask{Property: "deploy-timeout", Value: "300s", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestSchedulerK3sPropertyTaskGlobalWithAppSet(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		App:      "test-app",
		Global:   true,
		Property: "letsencrypt-email-prod",
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

func TestSchedulerK3sPropertyTaskPresentWithoutValue(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		App:      "test-app",
		Property: "deploy-timeout",
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

func TestSchedulerK3sPropertyTaskAbsentWithValue(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		App:      "test-app",
		Property: "deploy-timeout",
		Value:    "300s",
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

func TestSchedulerK3sPropertyTaskRejectsChartProperty(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		Global:   true,
		Property: "chart.traefik.replicas",
		Value:    "3",
		State:    StatePresent,
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected chart.* property to be rejected")
	}
	msg := result.Error.Error()
	if !strings.Contains(msg, "dokku_scheduler_k3s_chart") {
		t.Errorf("error should point users at the replacement task, got: %v", result.Error)
	}
	if !strings.Contains(msg, "deprecated") {
		t.Errorf("error should mention the dokku deprecation, got: %v", result.Error)
	}
}
