package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestSchedulerK3sAutoscalingAuthTaskSensitiveValues(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:     "test-app",
		Trigger: "aws-secret-manager",
		Metadata: map[string]string{
			"awsRegion":          "us-east-1",
			"awsSecretAccessKey": "REALSECRET",
		},
		State: StatePresent,
	}
	got := task.SensitiveValues()
	// Every non-empty metadata value is masked, regardless of key.
	if !sortedEqual(got, []string{"us-east-1", "REALSECRET"}) {
		t.Errorf("SensitiveValues() = %v, want both metadata values", got)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskSensitiveValuesAbsentEmpty(t *testing.T) {
	// On absent the values are empty (only keys matter), so nothing is
	// contributed from the struct; probed values are registered at plan time.
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StateAbsent,
	}
	if got := task.SensitiveValues(); len(got) != 0 {
		t.Errorf("SensitiveValues() = %v, want empty for absent", got)
	}
}

func TestSchedulerK3sAutoscalingAuthUnsetMasksProbedSecrets(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	// The server holds two metadata keys; the task clears one, so the other
	// survives and is read back (with its secret value) for the restore call.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet scheduler-k3s:autoscaling-auth:report test-app --format json": `{"aws-secret-manager.secretName":"my-secret","aws-secret-manager.awsSecretAccessKey":"REALSECRET"}`,
	}))()

	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StateAbsent,
	}
	result := task.Plan()
	if result.Error != nil {
		t.Fatalf("Plan returned error: %v", result.Error)
	}

	// Both the cleared key's old value and the surviving key's value are
	// server-probed secrets; they must be registered so every sink masks them.
	registered := subprocess.GlobalSensitive()
	for _, secret := range []string{"my-secret", "REALSECRET"} {
		found := false
		for _, v := range registered {
			if v == secret {
				found = true
			}
		}
		if !found {
			t.Errorf("probed secret %q not registered with masker: %v", secret, registered)
		}
	}

	// The rendered mutations (unset ... (was "my-secret"), restore ...=REALSECRET)
	// must mask to *** once the probed values are registered.
	for _, m := range result.Mutations {
		if masked := subprocess.MaskString(m); strings.Contains(masked, "REALSECRET") || strings.Contains(masked, "my-secret") {
			t.Errorf("mutation leaked a probed secret after masking: %q -> %q", m, masked)
		}
	}
}

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

func TestSchedulerK3sAutoscalingAuthTaskPresentEmptyValueRejected(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StatePresent,
	}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected error when a present-state metadata value is empty")
	}
	if !strings.Contains(err.Error(), "metadata values must not be empty for state 'present'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskAbsentEmptyValueAllowed(t *testing.T) {
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StateAbsent,
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("absent-state empty value should be allowed (clears the key), got %v", err)
	}
}
