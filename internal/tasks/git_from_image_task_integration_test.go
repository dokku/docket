package tasks

import (
	"testing"
)

func TestIntegrationGitFromImage(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-fromimage"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	task := GitFromImageTask{
		App:   appName,
		Image: "dokku/smoke-test-app:dockerfile",
		State: StateDeployed,
	}
	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to deploy from image: %v", result.Error)
	}
	if result.State != StateDeployed {
		t.Errorf("expected state 'deployed', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for initial deploy")
	}

	// deploy same image again (idempotent)
	result = task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent deploy failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for same image")
	}
	if result.State != StateDeployed {
		t.Errorf("expected state 'deployed', got '%s'", result.State)
	}
}
