package tasks

import (
	"testing"
)

func TestIntegrationConfigSetAndUnset(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-config"

	// ensure clean state
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set config
	setTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"TEST_KEY": "test_value"},
		State:   StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set config: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new config")
	}

	// setting same config again should be idempotent
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent set failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged config")
	}

	// unset config
	unsetTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"TEST_KEY": ""},
		State:   StateAbsent,
	}
	result = unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to unset config: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for config removal")
	}

	// unset again should be idempotent
	result = unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent unset failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-unset config")
	}
}

func TestIntegrationConfigMultipleKeys(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-multiconfig"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// set 3 keys
	setTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY_A": "val_a", "KEY_B": "val_b", "KEY_C": "val_c"},
		State:   StatePresent,
	}
	result := setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set config: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new config keys")
	}

	// update one key, keep others the same
	updateTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY_A": "val_a", "KEY_B": "val_b_updated", "KEY_C": "val_c"},
		State:   StatePresent,
	}
	result = updateTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to update config: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for partial config update")
	}

	// set same values again (idempotent)
	result = updateTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent update failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged config")
	}

	// unset all keys
	unsetTask := ConfigTask{
		App:     appName,
		Restart: boolPtr(false),
		Config:  map[string]string{"KEY_A": "", "KEY_B": "", "KEY_C": ""},
		State:   StateAbsent,
	}
	result = unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to unset config: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for config removal")
	}
}
