package tasks

import (
	"testing"
)

func TestStorageEntryTaskInvalidState(t *testing.T) {
	task := StorageEntryTask{Name: "test-entry", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestStorageEntryAbsentStateAllowed(t *testing.T) {
	// Absent is a valid state, unlike storage_ensure. The task will fail
	// because dokku isn't reachable, but the failure must not be the
	// "absent state is not supported" sentinel.
	task := StorageEntryTask{Name: "test-entry", State: StateAbsent}
	result := task.Execute()
	if result.Error != nil && result.Error.Error() == "the absent state is not supported for storage:ensure" {
		t.Errorf("absent should be supported for storage_entry, got: %v", result.Error)
	}
}

func TestStorageEntryRegistered(t *testing.T) {
	if _, ok := RegisteredTasks["dokku_storage_entry"]; !ok {
		t.Fatal("expected dokku_storage_entry to be registered")
	}
}
