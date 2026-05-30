package tasks

import (
	"testing"
)

func TestIntegrationStorageEntry(t *testing.T) {
	skipIfNoDokkuT(t)

	name := "docket-test-entry"

	// Start clean.
	destroy := StorageEntryTask{Name: name, State: StateAbsent}
	destroy.Execute()

	create := StorageEntryTask{Name: name, Chown: "herokuish", State: StatePresent}
	result := create.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create entry: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new entry")
	}

	// Re-apply: should be idempotent.
	result = create.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent create failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing entry")
	}

	// Destroy.
	result = destroy.Execute()
	if result.Error != nil {
		t.Fatalf("failed to destroy entry: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for destroy")
	}

	// Destroy again: idempotent.
	result = destroy.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent destroy failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for already-absent entry")
	}
}
