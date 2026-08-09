package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
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

func TestStorageEntryCreateArgsMinimal(t *testing.T) {
	// An omitted scheduler leaves the flag off entirely so dokku applies
	// its own default rather than docket restating it.
	task := StorageEntryTask{Name: "app-data", State: StatePresent}
	want := []string{"--quiet", "storage:create", "app-data"}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryCreateArgsPathIsTrailingPositional(t *testing.T) {
	task := StorageEntryTask{
		Name:      "app-data",
		Path:      "/mnt/app-data",
		Scheduler: "docker-local",
		Chown:     "herokuish",
		State:     StatePresent,
	}
	want := []string{
		"--quiet", "storage:create",
		"--scheduler", "docker-local",
		"--chown", "herokuish",
		"app-data", "/mnt/app-data",
	}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryCreateArgsEveryFlag(t *testing.T) {
	// The full k3s surface, asserting the fixed flag order plan and apply
	// both build from.
	task := StorageEntryTask{
		Name:          "app-data",
		Scheduler:     "k3s",
		Size:          "2Gi",
		AccessMode:    "ReadWriteOnce",
		StorageClass:  "longhorn",
		Namespace:     "apps",
		Chown:         "32767",
		ReclaimPolicy: "Retain",
		Annotations:   map[string]string{"team": "platform"},
		Labels:        map[string]string{"tier": "data"},
		State:         StatePresent,
	}
	want := []string{
		"--quiet", "storage:create",
		"--scheduler", "k3s",
		"--size", "2Gi",
		"--access-mode", "ReadWriteOnce",
		"--storage-class-name", "longhorn",
		"--namespace", "apps",
		"--chown", "32767",
		"--reclaim-policy", "Retain",
		"--annotation", "team=platform",
		"--label", "tier=data",
		"app-data",
	}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryCreateArgsRepeatsMapFlagsInSortedOrder(t *testing.T) {
	// dokku collects the flag into a map, so docket sorts the keys to keep
	// plan and apply byte-identical across runs.
	task := StorageEntryTask{
		Name:        "app-data",
		Scheduler:   "k3s",
		Size:        "1Gi",
		Annotations: map[string]string{"zeta": "1", "alpha": "2", "mid": "3"},
		Labels:      map[string]string{"zulu": "a", "bravo": "b"},
		State:       StatePresent,
	}
	want := []string{
		"--quiet", "storage:create",
		"--scheduler", "k3s",
		"--size", "1Gi",
		"--annotation", "alpha=2",
		"--annotation", "mid=3",
		"--annotation", "zeta=1",
		"--label", "bravo=b",
		"--label", "zulu=a",
		"app-data",
	}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryValidChownValues(t *testing.T) {
	// The same value set dokku_storage_ensure accepts: the entry task now
	// shares validChown rather than forwarding anything at all.
	for _, chown := range []string{"heroku", "herokuish", "paketo", "root", "false", "0", "1000", "32767", "65535"} {
		task := StorageEntryTask{Name: "app-data", Chown: chown, State: StatePresent}
		if err := task.Validate(); err != nil {
			t.Errorf("chown %q should be valid, got: %v", chown, err)
		}
	}
}

func TestStorageEntryInvalidChownValue(t *testing.T) {
	for _, chown := range []string{"packeto", "65536", "70000", "-1", "+5", "1000:1000", "root:root", "0x10", "1_000", "abc"} {
		task := StorageEntryTask{Name: "app-data", Chown: chown, State: StatePresent}
		err := task.Validate()
		if err == nil {
			t.Errorf("chown %q should be rejected", chown)
			continue
		}
		if !strings.Contains(err.Error(), "'chown' must be one of") {
			t.Errorf("chown %q: unexpected error %v", chown, err)
		}
	}
}

func TestStorageEntryOmittedChownAllowed(t *testing.T) {
	task := StorageEntryTask{Name: "app-data", State: StatePresent}
	if err := task.Validate(); err != nil {
		t.Errorf("an omitted chown should be valid, got: %v", err)
	}
}

func TestStorageEntryValidateSchedulerRules(t *testing.T) {
	tests := []struct {
		name    string
		task    StorageEntryTask
		wantErr string
	}{
		{
			name:    "unknown scheduler",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "nomad"},
			wantErr: "'scheduler' must be one of docker-local, k3s",
		},
		{
			name:    "docker-local rejects size",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "docker-local", Size: "2Gi"},
			wantErr: "'size' must not be set for scheduler 'docker-local'",
		},
		{
			name:    "docker-local rejects access_mode",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "docker-local", AccessMode: "ReadWriteOnce"},
			wantErr: "'access_mode' must not be set for scheduler 'docker-local'",
		},
		{
			name:    "docker-local rejects storage_class",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "docker-local", StorageClass: "longhorn"},
			wantErr: "'storage_class' must not be set for scheduler 'docker-local'",
		},
		{
			name:    "docker-local rejects a relative path",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "docker-local", Path: "./data"},
			wantErr: "'path' must be an absolute path or a docker named volume",
		},
		{
			name: "docker-local accepts a named volume",
			task: StorageEntryTask{Name: "app-data", Scheduler: "docker-local", Path: "app-data-volume"},
		},
		{
			name:    "k3s requires size",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s"},
			wantErr: "'size' is required for scheduler 'k3s'",
		},
		{
			name:    "k3s rejects storage_class with a path",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", StorageClass: "longhorn", Path: "/mnt/data"},
			wantErr: "'storage_class' and 'path' must not both be set",
		},
		{
			name:    "k3s rejects an unknown access_mode",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", AccessMode: "ReadWriteEverything"},
			wantErr: "'access_mode' must be one of",
		},
		{
			name:    "k3s rejects a relative path",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", Path: "data"},
			wantErr: "'path' must be an absolute path",
		},
		{
			name:    "k3s rejects an unknown reclaim_policy",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", ReclaimPolicy: "Recycle"},
			wantErr: "'reclaim_policy' must be one of Retain, Delete",
		},
		{
			name: "k3s full surface",
			task: StorageEntryTask{
				Name:          "app-data",
				Scheduler:     "k3s",
				Size:          "2Gi",
				AccessMode:    "ReadWriteMany",
				Namespace:     "apps",
				ReclaimPolicy: "Delete",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.task.State = StatePresent
			err := test.task.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got none", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected error containing %q, got: %v", test.wantErr, err)
			}
		})
	}
}

func TestStorageEntryValidateRejectsCreateOptionsWhenAbsent(t *testing.T) {
	// storage:destroy takes only the entry name, so a create-time option
	// alongside state 'absent' would be silently dropped.
	tests := []struct {
		name    string
		task    StorageEntryTask
		wantErr string
	}{
		{"path", StorageEntryTask{Name: "app-data", Path: "/mnt/data"}, "'path' must not be set for state 'absent'"},
		{"chown", StorageEntryTask{Name: "app-data", Chown: "herokuish"}, "'chown' must not be set for state 'absent'"},
		{"annotations", StorageEntryTask{Name: "app-data", Annotations: map[string]string{"a": "b"}}, "'annotations' must not be set for state 'absent'"},
		{"labels", StorageEntryTask{Name: "app-data", Labels: map[string]string{"a": "b"}}, "'labels' must not be set for state 'absent'"},
		{
			"several at once",
			StorageEntryTask{Name: "app-data", Size: "2Gi", Chown: "root"},
			"'size', 'chown' must not be set for state 'absent'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.task.State = StateAbsent
			err := test.task.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got none", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected error containing %q, got: %v", test.wantErr, err)
			}
		})
	}
}

func TestStorageEntryValidateAbsentWithOnlyNameIsValid(t *testing.T) {
	// The scheduler default must not count as a supplied create-time
	// option, or every destroy would fail validation.
	task := StorageEntryTask{Name: "app-data", Scheduler: "docker-local", State: StateAbsent}
	if err := task.Validate(); err != nil {
		t.Errorf("expected a bare destroy to validate, got: %v", err)
	}
}

func TestStorageEntryValidateMapKeysAndValues(t *testing.T) {
	// dokku splits each pair on its first '=' and the flag is comma-split
	// through a CSV reader before dokku sees it, so neither delimiter can
	// survive inside a key or value.
	tests := []struct {
		name    string
		task    StorageEntryTask
		wantErr string
	}{
		{
			name:    "empty annotation key",
			task:    StorageEntryTask{Annotations: map[string]string{"": "value"}},
			wantErr: "'annotations' keys must not be empty",
		},
		{
			name:    "annotation key with an equals",
			task:    StorageEntryTask{Annotations: map[string]string{"a=b": "value"}},
			wantErr: `'annotations' key "a=b" must not contain '='`,
		},
		{
			name:    "annotation key with a comma",
			task:    StorageEntryTask{Annotations: map[string]string{"a,b": "value"}},
			wantErr: `'annotations' key "a,b" must not contain ',' or '"'`,
		},
		{
			name:    "annotation value with a comma",
			task:    StorageEntryTask{Annotations: map[string]string{"team": "a,b"}},
			wantErr: `'annotations' value for "team" must not contain ',' or '"'`,
		},
		{
			name:    "annotation value with a double quote",
			task:    StorageEntryTask{Annotations: map[string]string{"team": `a"b`}},
			wantErr: `'annotations' value for "team" must not contain ',' or '"'`,
		},
		{
			name:    "label key with a double quote",
			task:    StorageEntryTask{Labels: map[string]string{`a"b`: "value"}},
			wantErr: `'labels' key "a\"b" must not contain ',' or '"'`,
		},
		{
			name:    "label value with a comma",
			task:    StorageEntryTask{Labels: map[string]string{"tier": "a,b"}},
			wantErr: `'labels' value for "tier" must not contain ',' or '"'`,
		},
		{
			name: "an equals in a value is fine",
			task: StorageEntryTask{Annotations: map[string]string{"team": "a=b"}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			test.task.Name = "app-data"
			test.task.State = StatePresent
			err := test.task.Validate()
			if test.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error containing %q, got none", test.wantErr)
			}
			if !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("expected error containing %q, got: %v", test.wantErr, err)
			}
		})
	}
}

func TestStorageEntryPlanReportsValidationError(t *testing.T) {
	// Validate runs before the probe, so an invalid input never reaches
	// the server.
	task := StorageEntryTask{Name: "app-data", Chown: "packeto", State: StatePresent}
	plan := task.Plan()
	if plan.Status != PlanStatusError {
		t.Fatalf("expected plan status %q, got %q", PlanStatusError, plan.Status)
	}
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), "'chown' must be one of") {
		t.Errorf("expected a chown error, got: %v", plan.Error)
	}
}

func TestStorageEntryPlanCreateCarriesEveryFlag(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet storage:list-entries --format json": `[]`,
	}))()

	task := StorageEntryTask{
		Name:        "app-data",
		Chown:       "herokuish",
		Annotations: map[string]string{"team": "platform"},
		State:       StatePresent,
	}
	plan := task.Plan()
	if plan.Status != PlanStatusCreate {
		t.Fatalf("expected plan status %q, got %q", PlanStatusCreate, plan.Status)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected one command, got %v", plan.Commands)
	}
	for _, want := range []string{"--chown herokuish", "--annotation team=platform"} {
		if !strings.Contains(plan.Commands[0], want) {
			t.Errorf("expected command to contain %q, got %q", want, plan.Commands[0])
		}
	}
}

// storageEntriesFixture is the storage:list-entries payload the export
// tests read: one docker-local entry carrying ownership, one k3s entry
// carrying the cluster attributes, and one auto-generated legacy entry.
func storageEntriesFixture() map[string]string {
	return map[string]string{
		"--quiet storage:list-entries --format json": `[
			{
				"name": "legacy-abc123",
				"scheduler": "docker-local",
				"host_path": "/var/lib/dokku/data/storage/legacy",
				"schema_version": 1
			},
			{
				"name": "k3s-data",
				"scheduler": "k3s",
				"size": "2Gi",
				"access_mode": "ReadWriteOnce",
				"storage_class": "longhorn",
				"namespace": "apps",
				"reclaim_policy": "Retain",
				"annotations": {"team": "platform"},
				"labels": {"tier": "data"},
				"schema_version": 1
			},
			{
				"name": "app-data",
				"scheduler": "docker-local",
				"host_path": "/var/lib/dokku/data/storage/app-data",
				"chown": "herokuish",
				"schema_version": 1
			}
		]`,
	}
}

func TestStorageEntryExportGlobal(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(storageEntriesFixture()))()

	exported, err := StorageEntryTask{}.ExportGlobal()
	if err != nil {
		t.Fatalf("ExportGlobal returned an error: %v", err)
	}
	if len(exported) != 2 {
		t.Fatalf("expected 2 entries (the legacy- entry is reconstructed by dokku_storage_mount), got %d: %v", len(exported), exported)
	}

	first, ok := exported[0].(StorageEntryTask)
	if !ok {
		t.Fatalf("expected a StorageEntryTask, got %T", exported[0])
	}
	// Ownership is what makes the host directory's export lossless.
	want := StorageEntryTask{
		Name:      "app-data",
		Path:      "/var/lib/dokku/data/storage/app-data",
		Scheduler: "docker-local",
		Chown:     "herokuish",
	}
	if first.Name != want.Name || first.Path != want.Path || first.Scheduler != want.Scheduler || first.Chown != want.Chown {
		t.Errorf("expected %+v, got %+v", want, first)
	}

	second, ok := exported[1].(StorageEntryTask)
	if !ok {
		t.Fatalf("expected a StorageEntryTask, got %T", exported[1])
	}
	if second.Name != "k3s-data" || second.Size != "2Gi" || second.AccessMode != "ReadWriteOnce" ||
		second.StorageClass != "longhorn" || second.Namespace != "apps" || second.ReclaimPolicy != "Retain" {
		t.Errorf("k3s attributes did not round-trip: %+v", second)
	}
	if second.Annotations["team"] != "platform" {
		t.Errorf("expected annotation team=platform, got %v", second.Annotations)
	}
	if second.Labels["tier"] != "data" {
		t.Errorf("expected label tier=data, got %v", second.Labels)
	}
	// An exported k3s entry must satisfy the task's own rules, or the
	// recipe export writes would not apply.
	if err := second.Validate(); err != nil {
		t.Errorf("exported k3s entry does not validate: %v", err)
	}
}
