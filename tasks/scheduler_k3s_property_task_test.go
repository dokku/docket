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

// TestSchedulerK3sPropertyTaskChartRejectionIsIdenticalOffline pins the fix for
// #458: the chart.* refusal used to be a guard inside Plan(), so `docket
// validate` fell through to the generic "unsupported property" list - a list
// that can never contain what the user wrote. It now lives on the property
// table both paths read, so the two report the same sentence.
func TestSchedulerK3sPropertyTaskChartRejectionIsIdenticalOffline(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		Global:   true,
		Property: "chart.traefik.replicas",
		Value:    "3",
		State:    StatePresent,
	}

	validateErr := task.Validate()
	if validateErr == nil {
		t.Fatal("Validate should reject a chart.* property")
	}

	plan := task.Plan()
	if plan.Status != PlanStatusError || plan.Error == nil {
		t.Fatalf("Plan should reject a chart.* property, got status %q error %v", plan.Status, plan.Error)
	}

	if validateErr.Error() != plan.Error.Error() {
		t.Errorf("validate and plan disagree:\n  validate: %v\n  plan:     %v", validateErr, plan.Error)
	}
	if !strings.Contains(validateErr.Error(), "dokku_scheduler_k3s_chart") {
		t.Errorf("Validate should name the replacement task, got: %v", validateErr)
	}
	if strings.Contains(validateErr.Error(), "unsupported property") {
		t.Errorf("Validate should not fall through to the supported-name list, got: %v", validateErr)
	}
}

// TestSchedulerK3sPropertyTaskChartRejectionOutranksScoping documents the
// ordering validatePropertyInput uses: a chart.* property with no app is
// answered with the task that owns it, not with the missing app, because the
// scoping error would send the user off to fix a recipe this task will never
// accept.
func TestSchedulerK3sPropertyTaskChartRejectionOutranksScoping(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		Property: "chart.traefik.replicas",
		Value:    "3",
		State:    StatePresent,
	}
	err := task.Validate()
	if err == nil {
		t.Fatal("Validate should reject a chart.* property with no app")
	}
	if !strings.Contains(err.Error(), "dokku_scheduler_k3s_chart") {
		t.Errorf("chart.* rejection should win over the missing-app error, got: %v", err)
	}
}

// TestSchedulerK3sPropertyTaskAcceptsChartPrefixedName guards the prefix
// boundary: the family is "chart." with the dot, so a supported-looking name
// that merely starts with the same letters is not swallowed by it.
func TestSchedulerK3sPropertyTaskAcceptsChartPrefixedName(t *testing.T) {
	task := SchedulerK3sPropertyTask{
		App:      "test-app",
		Property: "chartreuse",
		Value:    "3",
		State:    StatePresent,
	}
	err := task.Validate()
	if err == nil {
		t.Fatal("Validate should reject an unknown property")
	}
	if !strings.Contains(err.Error(), "unsupported property") {
		t.Errorf("a near-miss name should get the generic error, got: %v", err)
	}
}
