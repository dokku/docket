package tasks

import (
	"testing"
)

func TestIntegrationGitSync(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-gitsync"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// Build so dokku records the git-sync deploy source (its metadata is only
	// written when a build is triggered), which is what the probe reads back.
	task := GitSyncTask{
		App:    appName,
		Remote: "https://github.com/dokku/smoke-test-app.git",
		Build:  true,
		State:  StatePresent,
	}
	result := task.Execute()
	if result.Error != nil {
		t.Fatalf("failed to sync git: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for git sync")
	}

	// A second apply with no pinned ref must converge on the matching remote
	// rather than re-syncing every time (issue #310).
	result = task.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent git sync failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false when the app is already synced from the same remote")
	}
}
