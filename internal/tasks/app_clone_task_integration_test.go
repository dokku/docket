package tasks

import (
	"testing"
)

func TestIntegrationAppClone(t *testing.T) {
	skipIfNoDokkuT(t)

	sourceApp := "docket-test-clone-source"
	targetApp := "docket-test-clone-target"

	destroyApp(testCtx(), sourceApp)
	destroyApp(testCtx(), targetApp)
	createApp(testCtx(), sourceApp)
	defer destroyApp(testCtx(), sourceApp)
	defer destroyApp(testCtx(), targetApp)

	// clone the source app to the target
	cloneTask := AppCloneTask{
		App:        targetApp,
		SourceApp:  sourceApp,
		SkipDeploy: true,
		State:      StatePresent,
	}
	result := cloneTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to clone app: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first clone")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	exists, _ := appExists(testCtx(), targetApp)
	if !exists {
		t.Errorf("expected target app %q to exist after clone", targetApp)
	}

	// cloning again should be idempotent (target already exists)
	result = cloneTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second clone: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent clone")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present' on idempotent clone, got '%s'", result.State)
	}
}
