package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestIntegrationStorageMount(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount"
	hostDir := "/var/lib/dokku/data/storage/docket-test-mount"
	containerDir := "/app/storage"
	entryName := "docket-test-mount-entry"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// ensure storage directory exists
	subprocess.CallExecCommand(subprocess.ExecCommandInput{
		Command: "mkdir",
		Args:    []string{"-p", hostDir},
	})

	// legacy form: mount storage
	mountTask := StorageMountTask{
		App:          appName,
		HostDir:      hostDir,
		ContainerDir: containerDir,
		State:        StatePresent,
	}
	result := mountTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to mount storage (legacy): %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new mount")
	}

	// mount again should be idempotent
	result = mountTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent mount failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing mount")
	}

	// unmount storage
	unmountTask := StorageMountTask{
		App:          appName,
		HostDir:      hostDir,
		ContainerDir: containerDir,
		State:        StateAbsent,
	}
	result = unmountTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to unmount storage (legacy): %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for unmount")
	}

	// unmount again should be idempotent
	result = unmountTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent unmount failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent mount")
	}

	// named-entry form: create an entry first, then mount it
	entry := StorageEntryTask{
		Name:  entryName,
		Chown: "herokuish",
		State: StatePresent,
	}
	if r := entry.Execute(); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute()
	}()

	namedMount := StorageMountTask{
		App:          appName,
		EntryName:    entryName,
		ContainerDir: "/app/named",
		Phases:       []string{"deploy", "run"},
		State:        StatePresent,
	}
	result = namedMount.Execute()
	if result.Error != nil {
		t.Fatalf("failed to mount named entry: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new named-entry mount")
	}

	// idempotent re-apply
	result = namedMount.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent named-entry mount failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing named-entry mount")
	}

	// unmount named entry
	namedUnmount := StorageMountTask{
		App:          appName,
		EntryName:    entryName,
		ContainerDir: "/app/named",
		State:        StateAbsent,
	}
	result = namedUnmount.Execute()
	if result.Error != nil {
		t.Fatalf("failed to unmount named entry: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for named-entry unmount")
	}
	result = namedUnmount.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent named-entry unmount failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent named-entry mount")
	}
}
