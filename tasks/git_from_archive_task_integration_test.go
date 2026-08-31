package tasks

import (
	"testing"
)

func TestIntegrationGitFromArchive(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-git-from-archive"
	archiveURL := "https://github.com/dokku/smoke-test-app/archive/refs/heads/master.tar.gz"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// initial deploy
	task := GitFromArchiveTask{
		App:         appName,
		ArchiveURL:  archiveURL,
		ArchiveType: "tar.gz",
		State:       "deployed",
	}
	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to deploy archive: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first deploy")
	}
	if result.State != "deployed" {
		t.Errorf("expected state 'deployed', got '%s'", result.State)
	}

	// verify deploy-source metadata reflects the archive
	source, err := getAppDeploySource(testCtx(), appName)
	if err != nil {
		t.Fatalf("getAppDeploySource failed: %v", err)
	}
	if source.Source != "tar.gz" {
		t.Errorf("expected deploy source 'tar.gz', got %q", source.Source)
	}
	if source.SourceMetadata != archiveURL {
		t.Errorf("expected deploy source metadata %q, got %q", archiveURL, source.SourceMetadata)
	}

	// re-deploy with same archive - should be idempotent
	result = task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second deploy: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent deploy")
	}
	if result.State != "deployed" {
		t.Errorf("expected state 'deployed', got '%s'", result.State)
	}
}
