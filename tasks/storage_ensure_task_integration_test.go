package tasks

import (
	"testing"
)

func TestIntegrationStorageEnsure(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-storage"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	task := StorageEnsureTask{
		App:   appName,
		Chown: "herokuish",
		State: StatePresent,
	}
	result := task.Execute()
	if result.Error != nil {
		t.Fatalf("failed to ensure storage: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
}

func TestIntegrationStorageEnsureOmittedChown(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-storage-nochown"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// chown omitted: dokku applies its default (herokuish) ownership.
	task := StorageEnsureTask{
		App:   appName,
		State: StatePresent,
	}
	result := task.Execute()
	if result.Error != nil {
		t.Fatalf("failed to ensure storage without chown: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
}
