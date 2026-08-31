package tasks

import (
	"testing"
)

func TestIntegrationAppCreateAndDestroy(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-app"

	// ensure clean state
	destroyApp(testCtx(), appName)

	// create the app
	task := AppTask{App: appName, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to create app: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new app creation")
	}

	// creating again should be idempotent
	result = task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent create failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing app")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// destroy the app
	destroyTask := AppTask{App: appName, State: StateAbsent}
	result = destroyTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to destroy app: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for app destruction")
	}

	// destroying again should be idempotent
	result = destroyTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent destroy failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent app")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
}
