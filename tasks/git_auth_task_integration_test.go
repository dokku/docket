package tasks

import (
	"testing"
)

func TestIntegrationGitAuth(t *testing.T) {
	skipIfNoDokkuT(t)

	host := "docket-test-git-auth.example.com"

	// best-effort cleanup before and after
	cleanup := func() {
		(&GitAuthTask{Host: host, State: StateAbsent}).Execute(testCtx())
	}
	cleanup()
	t.Cleanup(cleanup)

	// set credentials
	setTask := GitAuthTask{
		Host:     host,
		Username: "deploy-bot",
		Password: "secret-token",
		State:    StatePresent,
	}
	result := setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set git auth: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on set")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// remove credentials
	unsetTask := GitAuthTask{Host: host, State: StateAbsent}
	result = unsetTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to unset git auth: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on unset")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
}
