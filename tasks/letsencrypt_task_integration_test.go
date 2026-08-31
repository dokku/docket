package tasks

import (
	"testing"
)

// TestIntegrationLetsencrypt exercises the idempotency paths that do not
// require an ACME server: the `letsencrypt:active` check on a fresh app and
// the `disable` no-op when nothing is configured. The full `enable` flow
// requires a mock ACME server (e.g. pebble) plus lego cert-trust plumbing
// inside the dokku-letsencrypt plugin's container, which is out of scope
// for this CI configuration; tracked for follow-up.
func TestIntegrationLetsencrypt(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "letsencrypt")

	appName := "docket-test-letsencrypt"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// fresh app should not be active
	active, err := letsencryptActive(testCtx(), appName)
	if err != nil {
		t.Fatalf("letsencryptActive failed: %v", err)
	}
	if active {
		t.Errorf("expected newly-created app to have letsencrypt inactive")
	}

	// disable on a non-active app is a no-op
	disableTask := LetsencryptTask{App: appName, State: StateAbsent}
	result := disableTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("disable on inactive app returned error: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on disable of inactive app")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}

	// disable again - still idempotent
	result = disableTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("second disable returned error: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent disable")
	}
}
