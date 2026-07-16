package tasks

import (
	"testing"
)

func TestIntegrationResourceReserve(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-resreserve"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set resource reservations
	setTask := ResourceReserveTask{
		App:       appName,
		Resources: map[string]string{"cpu": "100", "memory": "256"},
		State:     StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set resource reservations: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new resource reservations")
	}

	// setting same reservations again should be idempotent
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged resource reservations")
	}

	// clear resource reservations
	clearTask := ResourceReserveTask{
		App:   appName,
		State: StateAbsent,
	}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear resource reservations: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for clearing resource reservations")
	}

	// clear again should be idempotent
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent clear failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-cleared resource reservations")
	}
}

func TestIntegrationResourceReserveProcessType(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-resreserve-pt"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set resource reservations for a specific process type
	setTask := ResourceReserveTask{
		App:         appName,
		ProcessType: "web",
		Resources:   map[string]string{"memory": "512"},
		State:       StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set resource reservations: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new resource reservations")
	}

	// idempotent
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged resource reservations")
	}

	// clear before + set new values
	clearBeforeTask := ResourceReserveTask{
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

	// clear process-type reservations
	clearTask := ResourceReserveTask{
		App:         appName,
		ProcessType: "web",
		State:       StateAbsent,
	}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for clearing resource reservations")
	}
}
