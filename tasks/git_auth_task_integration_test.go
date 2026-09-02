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

	// re-applying the same credentials is a no-op. This is the end-to-end
	// proof that git:auth-status compared what git:auth wrote, password
	// included - both of which travelled on stdin.
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to re-apply git auth: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false when the netrc entry already matches")
	}

	// a rotated password is drift even though the host and username are
	// unchanged, so the probe has to be comparing the secret and not just
	// the entry's existence.
	rotateTask := setTask
	rotateTask.Password = "rotated-token"
	result = rotateTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to rotate git auth: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when the password changed")
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

	// removing an entry that is already gone is a no-op
	result = unsetTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to re-unset git auth: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false when the host has no netrc entry")
	}
}
