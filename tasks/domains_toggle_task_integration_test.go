package tasks

import (
	"testing"
)

func TestIntegrationDomainsToggle(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-domains"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// enable domains
	enableTask := DomainsToggleTask{App: appName, State: StatePresent}
	result := enableTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to enable domains: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// disable domains
	disableTask := DomainsToggleTask{App: appName, State: StateAbsent}
	result = disableTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to disable domains: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
}
