package tasks

import (
	"testing"
)

func TestIntegrationAppLock(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-app-lock"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// initial state - not locked
	if locked, _ := appLocked(testCtx(), appName); locked {
		t.Fatalf("expected newly-created app %q to be unlocked", appName)
	}

	// lock the app
	lockTask := AppLockTask{App: appName, State: StatePresent}
	result := lockTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to lock app: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first lock")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if locked, _ := appLocked(testCtx(), appName); !locked {
		t.Errorf("expected app %q to be locked after lock task", appName)
	}

	// lock again - should be idempotent
	result = lockTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second lock: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent lock")
	}
	if locked, _ := appLocked(testCtx(), appName); !locked {
		t.Errorf("expected app %q to remain locked", appName)
	}

	// unlock the app
	unlockTask := AppLockTask{App: appName, State: StateAbsent}
	result = unlockTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to unlock app: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first unlock")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if locked, _ := appLocked(testCtx(), appName); locked {
		t.Errorf("expected app %q to be unlocked after unlock task", appName)
	}

	// unlock again - should be idempotent
	result = unlockTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second unlock: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent unlock")
	}
	if locked, _ := appLocked(testCtx(), appName); locked {
		t.Errorf("expected app %q to remain unlocked", appName)
	}
}
