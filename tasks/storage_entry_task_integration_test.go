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

func TestIntegrationStorageEntryNumericChown(t *testing.T) {
	skipIfNoDokkuT(t)

	name := "docket-test-entry-numeric-chown"

	// Start clean, then create with a raw numeric uid. The sibling
	// dokku_storage_entry passes chown through unrestricted, so numeric uids
	// already work here (unlike the historically narrower dokku_storage_ensure).
	destroy := StorageEntryTask{Name: name, State: StateAbsent}
	destroy.Execute()
	defer destroy.Execute()

	create := StorageEntryTask{Name: name, Chown: "32767", State: StatePresent}
	result := create.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create entry with numeric chown: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new entry")
	}
}

func TestIntegrationStorageEntryAnnotationsAndLabels(t *testing.T) {
	skipIfNoDokkuT(t)

	name := "docket-test-entry-metadata"

	// The repeated --annotation / --label flags are the last of
	// storage:create's surface docket exposes. dokku records them on the
	// entry whatever the scheduler, so a docker-local entry is enough to
	// prove the pairs survive the flag round trip intact.
	destroy := StorageEntryTask{Name: name, State: StateAbsent}
	destroy.Execute()
	defer destroy.Execute()

	create := StorageEntryTask{
		Name:        name,
		Chown:       "herokuish",
		Annotations: map[string]string{"docket.io/team": "platform", "docket.io/tier": "data"},
		Labels:      map[string]string{"docket-managed": "true"},
		State:       StatePresent,
	}
	result := create.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create entry with annotations and labels: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new entry")
	}

	entries, err := storageEntries()
	if err != nil {
		t.Fatalf("failed to read storage entries: %v", err)
	}
	var found *storageEntry
	for i := range entries {
		if entries[i].Name == name {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatalf("expected %s in storage:list-entries, got %+v", name, entries)
	}
	if found.Annotations["docket.io/team"] != "platform" || found.Annotations["docket.io/tier"] != "data" {
		t.Errorf("annotations did not round-trip: %v", found.Annotations)
	}
	if found.Labels["docket-managed"] != "true" {
		t.Errorf("labels did not round-trip: %v", found.Labels)
	}
	if found.Chown != "herokuish" {
		t.Errorf("expected chown 'herokuish', got %q", found.Chown)
	}

	// ExportGlobal reconstructs the same values, so `docket export`
	// reproduces the entry rather than a lossy shell of it.
	exported, err := StorageEntryTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal returned an error: %v", err)
	}
	var task *StorageEntryTask
	for _, item := range exported {
		entry, ok := item.(StorageEntryTask)
		if ok && entry.Name == name {
			task = &entry
			break
		}
	}
	if task == nil {
		t.Fatalf("expected %s in the exported entries, got %+v", name, exported)
	}
	if task.Chown != "herokuish" {
		t.Errorf("expected exported chown 'herokuish', got %q", task.Chown)
	}
	if task.Annotations["docket.io/team"] != "platform" {
		t.Errorf("expected exported annotations to carry the team, got %v", task.Annotations)
	}
	if task.Labels["docket-managed"] != "true" {
		t.Errorf("expected exported labels to carry docket-managed, got %v", task.Labels)
	}
}
