package tasks

import (
	"testing"
)

func TestTaskDeprecationEmptyForUndeprecatedTask(t *testing.T) {
	if got := TaskDeprecation(&AppTask{}); got != "" {
		t.Errorf("expected empty deprecation for AppTask, got %q", got)
	}
}

func TestTaskDeprecationForStorageEnsure(t *testing.T) {
	got := TaskDeprecation(&StorageEnsureTask{})
	if got == "" {
		t.Fatal("expected non-empty deprecation for StorageEnsureTask")
	}
	if !contains(got, "dokku_storage_entry") {
		t.Errorf("expected deprecation message to point to dokku_storage_entry, got %q", got)
	}
}

func contains(haystack, needle string) bool {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
