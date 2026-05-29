package tasks

import (
	"testing"
)

func TestIntegrationMaintenance(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "maintenance")

	appName := "docket-test-maintenance"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// enable maintenance mode
	enableTask := MaintenanceTask{App: appName, State: StatePresent}
	result := enableTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to enable maintenance: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// enable again - idempotent
	result = enableTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second enable: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent enable")
	}

	// disable maintenance mode
	disableTask := MaintenanceTask{App: appName, State: StateAbsent}
	result = disableTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to disable maintenance: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}

	// disable again - idempotent
	result = disableTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second disable: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent disable")
	}
}
