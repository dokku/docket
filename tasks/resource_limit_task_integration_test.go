package tasks

import (
	"testing"
)

func TestIntegrationResourceLimit(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-reslimit"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set resource limits
	setTask := ResourceLimitTask{
		App:       appName,
		Resources: map[string]string{"cpu": "100", "memory": "256"},
		State:     StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set resource limits: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new resource limits")
	}

	// setting same limits again should be idempotent
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged resource limits")
	}

	// clear resource limits
	clearTask := ResourceLimitTask{
		App:   appName,
		State: StateAbsent,
	}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear resource limits: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for clearing resource limits")
	}

	// clear again should be idempotent
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent clear failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-cleared resource limits")
	}
}

func TestIntegrationResourceLimitProcessType(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-reslimit-pt"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set resource limits for a specific process type
	setTask := ResourceLimitTask{
		App:         appName,
		ProcessType: "web",
		Resources:   map[string]string{"memory": "512"},
		State:       StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set resource limits: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new resource limits")
	}

	// idempotent
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged resource limits")
	}

	// clear before + set new values
	clearBeforeTask := ResourceLimitTask{
		App:         appName,
		ProcessType: "web",
		Resources:   map[string]string{"cpu": "200"},
		ClearBefore: boolPtr(true),
		State:       StatePresent,
	}
	result = clearBeforeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear_before and set: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for clear_before operation")
	}

	// clear process-type limits
	clearTask := ResourceLimitTask{
		App:         appName,
		ProcessType: "web",
		State:       StateAbsent,
	}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for clearing resource limits")
	}
}
