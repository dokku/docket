package tasks

import (
	"context"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestStorageEntryTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := StorageEntryTask{Name: "test-entry", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestStorageEntryAbsentStateAllowed(t *testing.T) {
	t.Parallel()
	// Absent is a valid state, unlike storage_ensure. The task will fail
	// because dokku isn't reachable, but the failure must not be the
	// "absent state is not supported" sentinel.
	task := StorageEntryTask{Name: "test-entry", State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error != nil && result.Error.Error() == "the absent state is not supported for storage:ensure" {
		t.Errorf("absent should be supported for storage_entry, got: %v", result.Error)
	}
}

func TestStorageEntryRegistered(t *testing.T) {
	t.Parallel()
	if _, ok := RegisteredTasks["dokku_storage_entry"]; !ok {
		t.Fatal("expected dokku_storage_entry to be registered")
	}
}

func TestStorageEntryCreateArgsMinimal(t *testing.T) {
	t.Parallel()
	// An omitted scheduler leaves the flag off entirely so dokku applies
	// its own default rather than docket restating it.
	task := StorageEntryTask{Name: "app-data", State: StatePresent}
	want := []string{"--quiet", "storage:create", "app-data"}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryCreateArgsPathIsTrailingPositional(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

func TestStorageEntryCreateArgsCarriesMode(t *testing.T) {
	t.Parallel()
	// --mode sits between --chown and --reclaim-policy, matching dokku's own
	// flag order, and is rendered in the 4 digit form the registry records so
	// the create command reads the same as the storage:set a later converge
	// would issue.
	task := StorageEntryTask{
		Name:  "app-data",
		Chown: "herokuish",
		Mode:  "755",
		State: StatePresent,
	}
	want := []string{
		"--quiet", "storage:create",
		"--chown", "herokuish",
		"--mode", "0755",
		"app-data",
	}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryCreateArgsKeepsAFourDigitMode(t *testing.T) {
	t.Parallel()
	task := StorageEntryTask{Name: "app-data", Mode: "0777", State: StatePresent}
	want := []string{"--quiet", "storage:create", "--mode", "0777", "app-data"}
	if got := task.createArgs(); !equalStrings(got, want) {
		t.Errorf("expected args %v, got %v", want, got)
	}
}

func TestStorageEntryValidModeValues(t *testing.T) {
	t.Parallel()
	// The same shape dokku's directoryModeRegexp accepts: 3 or 4 octal digits.
	for _, mode := range []string{"000", "755", "777", "0000", "0755", "0777", "2775", "7777"} {
		task := StorageEntryTask{Name: "app-data", Mode: mode, State: StatePresent}
		if err := task.Validate(); err != nil {
			t.Errorf("mode %q should be valid, got: %v", mode, err)
		}
	}
}

func TestStorageEntryInvalidModeValue(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"8", "99", "12345", "0888", "0o755", "-755", "+755", "07 5", "abc", "0x1ff", "u+rwx"} {
		task := StorageEntryTask{Name: "app-data", Mode: mode, State: StatePresent}
		err := task.Validate()
		if err == nil {
			t.Errorf("mode %q should be rejected", mode)
			continue
		}
		if !strings.Contains(err.Error(), "'mode' must be a 3 or 4 digit octal directory mode") {
			t.Errorf("mode %q: unexpected error %v", mode, err)
		}
	}
}

func TestStorageEntryOmittedModeAllowed(t *testing.T) {
	t.Parallel()
	task := StorageEntryTask{Name: "app-data", State: StatePresent}
	if err := task.Validate(); err != nil {
		t.Errorf("an omitted mode should be valid, got: %v", err)
	}
}

func TestStorageEntryValidChownValues(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	task := StorageEntryTask{Name: "app-data", State: StatePresent}
	if err := task.Validate(); err != nil {
		t.Errorf("an omitted chown should be valid, got: %v", err)
	}
}

func TestStorageEntryValidateSchedulerRules(t *testing.T) {
	t.Parallel()
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
			name:    "k3s rejects mode",
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", Mode: "0777"},
			wantErr: "'mode' must not be set for scheduler 'k3s'",
		},
		{
			name: "docker-local accepts mode",
			task: StorageEntryTask{Name: "app-data", Scheduler: "docker-local", Mode: "0777"},
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
	t.Parallel()
	// storage:destroy takes only the entry name, so a create-time option
	// alongside state 'absent' would be silently dropped.
	tests := []struct {
		name    string
		task    StorageEntryTask
		wantErr string
	}{
		{"path", StorageEntryTask{Name: "app-data", Path: "/mnt/data"}, "'path' must not be set for state 'absent'"},
		{"chown", StorageEntryTask{Name: "app-data", Chown: "herokuish"}, "'chown' must not be set for state 'absent'"},
		{"mode", StorageEntryTask{Name: "app-data", Mode: "0777"}, "'mode' must not be set for state 'absent'"},
		{"annotations", StorageEntryTask{Name: "app-data", Annotations: map[string]string{"a": "b"}}, "'annotations' must not be set for state 'absent'"},
		{"labels", StorageEntryTask{Name: "app-data", Labels: map[string]string{"a": "b"}}, "'labels' must not be set for state 'absent'"},
		{
			"several at once",
			StorageEntryTask{Name: "app-data", Size: "2Gi", Chown: "root", Mode: "0777"},
			"'size', 'chown', 'mode' must not be set for state 'absent'",
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
	t.Parallel()
	// The scheduler default must not count as a supplied create-time
	// option, or every destroy would fail validation.
	task := StorageEntryTask{Name: "app-data", Scheduler: "docker-local", State: StateAbsent}
	if err := task.Validate(); err != nil {
		t.Errorf("expected a bare destroy to validate, got: %v", err)
	}
}

func TestStorageEntryValidateRejectsDestroyHostDirWhenPresent(t *testing.T) {
	t.Parallel()
	// storage:destroy is the only command that takes the flag, so asking for
	// it under 'present' asks for a removal that will never happen.
	task := StorageEntryTask{Name: "app-data", DestroyHostDir: true, State: StatePresent}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected destroy_host_dir under state 'present' to be rejected")
	}
	if !strings.Contains(err.Error(), "'destroy_host_dir' must not be set for state 'present'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestStorageEntryValidateAcceptsDestroyHostDirWhenAbsent(t *testing.T) {
	t.Parallel()
	// It is the one field state 'absent' wants rather than refuses, so it must
	// not be swept up by the create-time option guard.
	task := StorageEntryTask{Name: "app-data", DestroyHostDir: true, State: StateAbsent}
	if err := task.Validate(); err != nil {
		t.Errorf("expected destroy_host_dir to validate under state 'absent', got: %v", err)
	}
}

func TestStorageEntryValidateMapKeysAndValues(t *testing.T) {
	t.Parallel()
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
			// storage:annotations:set reads an empty value as a delete, so a
			// pair declared empty could never be stored and the convergence
			// pass would clear it and miss it again on every run.
			name:    "empty annotation value",
			task:    StorageEntryTask{Annotations: map[string]string{"team": ""}},
			wantErr: `'annotations' value for "team" must not be empty`,
		},
		{
			name:    "empty label value",
			task:    StorageEntryTask{Labels: map[string]string{"tier": ""}},
			wantErr: `'labels' value for "tier" must not be empty`,
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
	t.Parallel()
	// Validate runs before the probe, so an invalid input never reaches
	// the server.
	task := StorageEntryTask{Name: "app-data", Chown: "packeto", State: StatePresent}
	plan := task.Plan(testCtx())
	if plan.Status != PlanStatusError {
		t.Fatalf("expected plan status %q, got %q", PlanStatusError, plan.Status)
	}
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), "'chown' must be one of") {
		t.Errorf("expected a chown error, got: %v", plan.Error)
	}
}

func TestStorageEntryPlanCreateCarriesEveryFlag(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet storage:list-entries --format json": `[]`,
	}))

	task := StorageEntryTask{
		Name:        "app-data",
		Chown:       "herokuish",
		Annotations: map[string]string{"team": "platform"},
		State:       StatePresent,
	}
	plan := task.Plan(ctx)
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
				"mode": "0777",
				"schema_version": 1
			}
		]`,
	}
}

func TestStorageEntryExportGlobal(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(storageEntriesFixture()))

	exported, err := StorageEntryTask{}.ExportGlobal(ctx)
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
	// Ownership and permissions are what make the host directory's export
	// lossless: an entry re-applied elsewhere without them lands at dokku's
	// default 0755 herokuish directory rather than the one it was exported from.
	want := StorageEntryTask{
		Name:      "app-data",
		Path:      "/var/lib/dokku/data/storage/app-data",
		Scheduler: "docker-local",
		Chown:     "herokuish",
		Mode:      "0777",
	}
	if first.Name != want.Name || first.Path != want.Path || first.Scheduler != want.Scheduler ||
		first.Chown != want.Chown || first.Mode != want.Mode {
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

// singleStorageEntryFixture is a storage:list-entries payload holding one
// entry, for the convergence tests that compare a recipe against what the
// registry records.
func singleStorageEntryFixture(entryJSON string) map[string]string {
	return map[string]string{
		"--quiet storage:list-entries --format json": "[" + entryJSON + "]",
	}
}

const dockerLocalEntryJSON = `{
	"name": "app-data",
	"scheduler": "docker-local",
	"host_path": "/var/lib/dokku/data/storage/app-data",
	"chown": "herokuish",
	"schema_version": 1
}`

func TestStorageEntryPlanInSyncWhenEveryDeclaredFieldMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryJSON)))

	task := StorageEntryTask{
		Name:      "app-data",
		Scheduler: "docker-local",
		Path:      "/var/lib/dokku/data/storage/app-data",
		Chown:     "herokuish",
		State:     StatePresent,
	}
	plan := task.Plan(ctx)
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("expected an in-sync plan, got status %q reason %q", plan.Status, plan.Reason)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("an in-sync plan must issue no commands, got %v", plan.Commands)
	}
}

func TestStorageEntryPlanLeavesUndeclaredAttributesAlone(t *testing.T) {
	t.Parallel()
	// The recipe names only the fields it manages. A size, namespace and
	// annotation it never mentions are neither compared nor cleared, which
	// is what stops a partially-declared recipe from destroying attributes
	// set elsewhere.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(`{
		"name": "app-data",
		"scheduler": "k3s",
		"size": "2Gi",
		"namespace": "apps",
		"chown": "herokuish",
		"annotations": {"team": "platform"},
		"labels": {"tier": "data"},
		"schema_version": 1
	}`)))

	task := StorageEntryTask{
		Name:      "app-data",
		Scheduler: "k3s",
		Size:      "2Gi",
		Chown:     "herokuish",
		State:     StatePresent,
	}
	plan := task.Plan(ctx)
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("expected an in-sync plan, got status %q reason %q mutations %v", plan.Status, plan.Reason, plan.Mutations)
	}
}

func TestStorageEntryPlanConvergesChown(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryJSON)))

	task := StorageEntryTask{Name: "app-data", Chown: "root", State: StatePresent}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusModify {
		t.Fatalf("expected plan status %q, got %q", PlanStatusModify, plan.Status)
	}
	if plan.InSync {
		t.Error("a chown change must not report in sync")
	}
	want := []string{"dokku --quiet storage:set app-data chown root"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
	wantMutations := []string{`set chown=root (was "herokuish")`}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("expected mutations %v, got %v", wantMutations, plan.Mutations)
	}
	if plan.Reason != "1 attribute(s) to set" {
		t.Errorf("unexpected reason %q", plan.Reason)
	}
}

// dockerLocalEntryWithModeJSON records a mode, for the convergence and
// normalization tests that compare a recipe against it.
const dockerLocalEntryWithModeJSON = `{
	"name": "app-data",
	"scheduler": "docker-local",
	"host_path": "/var/lib/dokku/data/storage/app-data",
	"chown": "herokuish",
	"mode": "0755",
	"schema_version": 1
}`

func TestStorageEntryPlanConvergesMode(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryWithModeJSON)))

	task := StorageEntryTask{Name: "app-data", Mode: "0777", State: StatePresent}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusModify {
		t.Fatalf("expected plan status %q, got %q (error %v)", PlanStatusModify, plan.Status, plan.Error)
	}
	want := []string{"dokku --quiet storage:set app-data mode 0777"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
	wantMutations := []string{`set mode=0777 (was "0755")`}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("expected mutations %v, got %v", wantMutations, plan.Mutations)
	}
}

func TestStorageEntryPlanReadsAThreeDigitModeAsItsFourDigitForm(t *testing.T) {
	t.Parallel()
	// dokku records 0755 whether the caller wrote 755 or 0755. Comparing the
	// raw recipe value against the recorded one would report drift on every
	// run and re-apply a mode that was already correct.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryWithModeJSON)))

	task := StorageEntryTask{Name: "app-data", Mode: "755", State: StatePresent}
	plan := task.Plan(ctx)
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("expected an in-sync plan, got status %q mutations %v", plan.Status, plan.Mutations)
	}
}

func TestStorageEntryPlanNormalizesAThreeDigitModeItSets(t *testing.T) {
	t.Parallel()
	// The other half of the same rule: a drifted three digit mode converges to
	// the four digit form, so the next run settles rather than looping.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryWithModeJSON)))

	task := StorageEntryTask{Name: "app-data", Mode: "700", State: StatePresent}
	plan := task.Plan(ctx)
	want := []string{"dokku --quiet storage:set app-data mode 0700"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
}

func TestStorageEntryPlanConvergesChownBeforeMode(t *testing.T) {
	t.Parallel()
	// Struct field order, so plan and apply build byte-identical argv across
	// runs. The k3s ordering test below cannot cover this pair: k3s rejects
	// mode outright.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryWithModeJSON)))

	task := StorageEntryTask{Name: "app-data", Chown: "root", Mode: "0777", State: StatePresent}
	plan := task.Plan(ctx)
	want := []string{
		"dokku --quiet storage:set app-data chown root",
		"dokku --quiet storage:set app-data mode 0777",
	}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands\n%v\ngot\n%v", want, plan.Commands)
	}
	if plan.Reason != "2 attribute(s) to set" {
		t.Errorf("unexpected reason %q", plan.Reason)
	}
}

func TestStorageEntryPlanConvergesEveryMutableAttributeInFieldOrder(t *testing.T) {
	t.Parallel()
	// One command per drifted field, in struct field order with sorted map
	// keys, so plan and apply build byte-identical argv across runs.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(`{
		"name": "app-data",
		"scheduler": "k3s",
		"size": "2Gi",
		"namespace": "apps",
		"chown": "herokuish",
		"reclaim_policy": "Retain",
		"annotations": {"team": "infra"},
		"schema_version": 1
	}`)))

	task := StorageEntryTask{
		Name:          "app-data",
		Scheduler:     "k3s",
		Size:          "4Gi",
		Namespace:     "data",
		Chown:         "root",
		ReclaimPolicy: "Delete",
		Annotations:   map[string]string{"zeta": "1", "team": "platform"},
		Labels:        map[string]string{"tier": "data"},
		State:         StatePresent,
	}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusModify {
		t.Fatalf("expected plan status %q, got %q (error %v)", PlanStatusModify, plan.Status, plan.Error)
	}
	want := []string{
		"dokku --quiet storage:set app-data size 4Gi",
		"dokku --quiet storage:set app-data namespace data",
		"dokku --quiet storage:set app-data chown root",
		"dokku --quiet storage:set app-data reclaim-policy Delete",
		"dokku --quiet storage:annotations:set app-data team platform",
		"dokku --quiet storage:annotations:set app-data zeta 1",
		"dokku --quiet storage:labels:set app-data tier data",
	}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands\n%v\ngot\n%v", want, plan.Commands)
	}
	if plan.Reason != "7 attribute(s) to set" {
		t.Errorf("unexpected reason %q", plan.Reason)
	}
	wantMutations := []string{
		`set size=4Gi (was "2Gi")`,
		`set namespace=data (was "apps")`,
		`set chown=root (was "herokuish")`,
		`set reclaim_policy=Delete (was "Retain")`,
		`set annotation team=platform (was "infra")`,
		"set annotation zeta=1 (new)",
		"set label tier=data (new)",
	}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("expected mutations\n%v\ngot\n%v", wantMutations, plan.Mutations)
	}
}

func TestStorageEntryPlanConvergesMapKeysWithoutDisturbingSiblings(t *testing.T) {
	t.Parallel()
	// The per-key subcommands are what make an omitted key unmanaged; the
	// wholesale --annotation flag would replace the entire map and drop the
	// key the recipe does not name.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(`{
		"name": "app-data",
		"scheduler": "docker-local",
		"host_path": "/var/lib/dokku/data/storage/app-data",
		"annotations": {"team": "platform", "owner": "sre"},
		"schema_version": 1
	}`)))

	task := StorageEntryTask{
		Name:        "app-data",
		Annotations: map[string]string{"team": "data"},
		State:       StatePresent,
	}
	plan := task.Plan(ctx)
	want := []string{"dokku --quiet storage:annotations:set app-data team data"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
	for _, command := range plan.Commands {
		if strings.Contains(command, "owner") {
			t.Errorf("an undeclared annotation must be left alone, got %q", command)
		}
	}
}

func TestStorageEntryPlanRejectsImmutableDrift(t *testing.T) {
	t.Parallel()
	// dokku refuses an access-mode or storage-class swap in place, and has
	// no command at all for the scheduler or the host path, so the plan
	// errors rather than reporting a change it could never apply.
	tests := []struct {
		name    string
		entry   string
		task    StorageEntryTask
		wantErr string
	}{
		{
			name:    "scheduler",
			entry:   dockerLocalEntryJSON,
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi"},
			wantErr: `records scheduler "docker-local", recipe declares "k3s"`,
		},
		{
			name:    "path",
			entry:   dockerLocalEntryJSON,
			task:    StorageEntryTask{Name: "app-data", Path: "/mnt/app-data"},
			wantErr: `records path "/var/lib/dokku/data/storage/app-data", recipe declares "/mnt/app-data"`,
		},
		{
			name:    "access_mode",
			entry:   `{"name": "app-data", "scheduler": "k3s", "size": "2Gi", "access_mode": "ReadWriteOnce", "schema_version": 1}`,
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", AccessMode: "ReadWriteMany"},
			wantErr: `records access_mode "ReadWriteOnce", recipe declares "ReadWriteMany"`,
		},
		{
			name:    "storage_class",
			entry:   `{"name": "app-data", "scheduler": "k3s", "size": "2Gi", "schema_version": 1}`,
			task:    StorageEntryTask{Name: "app-data", Scheduler: "k3s", Size: "2Gi", StorageClass: "longhorn"},
			wantErr: `records storage_class "", recipe declares "longhorn"`,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(test.entry)))

			test.task.State = StatePresent
			plan := test.task.Plan(ctx)
			if plan.Status != PlanStatusError {
				t.Fatalf("expected plan status %q, got %q", PlanStatusError, plan.Status)
			}
			if plan.Error == nil || !strings.Contains(plan.Error.Error(), test.wantErr) {
				t.Errorf("expected an error containing %q, got: %v", test.wantErr, plan.Error)
			}
			if len(plan.Commands) != 0 {
				t.Errorf("a refused plan must issue no commands, got %v", plan.Commands)
			}
		})
	}
}

func TestStorageEntryPlanReadsAnOmittedSchedulerAsDockerLocal(t *testing.T) {
	t.Parallel()
	// createArgs drops the flag when the scheduler is empty and dokku
	// applies docker-local, so the comparison has to read it the same way
	// rather than treating an empty scheduler as unmanaged.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(
		`{"name": "app-data", "scheduler": "k3s", "size": "2Gi", "schema_version": 1}`,
	)))

	task := StorageEntryTask{Name: "app-data", State: StatePresent}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusError {
		t.Fatalf("expected plan status %q, got %q", PlanStatusError, plan.Status)
	}
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), `recipe declares "docker-local"`) {
		t.Errorf("expected the omitted scheduler to read as docker-local, got: %v", plan.Error)
	}
}

func TestStorageEntryPlanAbsentIgnoresAttributes(t *testing.T) {
	t.Parallel()
	// Attributes are rejected by Validate under state 'absent', so the
	// destroy branch never compares them - it plans on presence alone.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryJSON)))

	task := StorageEntryTask{Name: "app-data", State: StateAbsent}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusDestroy {
		t.Fatalf("expected plan status %q, got %q", PlanStatusDestroy, plan.Status)
	}
	want := []string{"dokku --quiet storage:destroy --force app-data"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
}

func TestStorageEntryPlanAbsentDestroysTheHostDirectoryOnRequest(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryJSON)))

	task := StorageEntryTask{Name: "app-data", DestroyHostDir: true, State: StateAbsent}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusDestroy {
		t.Fatalf("expected plan status %q, got %q (error %v)", PlanStatusDestroy, plan.Status, plan.Error)
	}
	want := []string{"dokku --quiet storage:destroy --force --destroy-host-dir app-data"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
	// The plan has to name the directory: it is the data the apply removes.
	wantMutations := []string{"destroy storage entry app-data and its host directory /var/lib/dokku/data/storage/app-data"}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("expected mutations %v, got %v", wantMutations, plan.Mutations)
	}
}

func TestStorageEntryPlanAbsentReportsAReclaimDeleteRemoval(t *testing.T) {
	t.Parallel()
	// dokku removes the host directory for a Delete entry with no flag at all,
	// so a plan reporting only what the recipe asked for would stay silent
	// about a directory that is about to go.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(`{
		"name": "app-data",
		"scheduler": "docker-local",
		"host_path": "/var/lib/dokku/data/storage/app-data",
		"reclaim_policy": "Delete",
		"schema_version": 1
	}`)))

	task := StorageEntryTask{Name: "app-data", State: StateAbsent}
	plan := task.Plan(ctx)
	want := []string{"dokku --quiet storage:destroy --force app-data"}
	if !equalStrings(plan.Commands, want) {
		t.Errorf("expected commands %v, got %v", want, plan.Commands)
	}
	wantMutations := []string{"destroy storage entry app-data and its host directory /var/lib/dokku/data/storage/app-data"}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("expected mutations %v, got %v", wantMutations, plan.Mutations)
	}
}

func TestStorageEntryPlanAbsentLeavesTheHostDirectoryAloneByDefault(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(dockerLocalEntryJSON)))

	task := StorageEntryTask{Name: "app-data", State: StateAbsent}
	plan := task.Plan(ctx)
	wantMutations := []string{"destroy storage entry app-data"}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("expected mutations %v, got %v", wantMutations, plan.Mutations)
	}
}

func TestStorageEntryPlanAbsentRejectsDestroyHostDirOnANonDockerLocalEntry(t *testing.T) {
	t.Parallel()
	// dokku refuses the flag outright there, so the plan says so rather than
	// letting the apply fail part way through.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(singleStorageEntryFixture(
		`{"name": "app-data", "scheduler": "k3s", "size": "2Gi", "reclaim_policy": "Delete", "schema_version": 1}`,
	)))

	task := StorageEntryTask{Name: "app-data", DestroyHostDir: true, State: StateAbsent}
	plan := task.Plan(ctx)
	if plan.Status != PlanStatusError {
		t.Fatalf("expected plan status %q, got %q", PlanStatusError, plan.Status)
	}
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), `records scheduler "k3s"`) {
		t.Errorf("expected a scheduler error, got: %v", plan.Error)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("a refused plan must issue no commands, got %v", plan.Commands)
	}
}

func TestStorageEntryPlanAbsentIgnoresDestroyHostDirWhenTheEntryIsGone(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet storage:list-entries --format json": `[]`,
	}))

	task := StorageEntryTask{Name: "app-data", DestroyHostDir: true, State: StateAbsent}
	plan := task.Plan(ctx)
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("expected an in-sync plan, got status %q (error %v)", plan.Status, plan.Error)
	}
}

func TestStorageEntryExecuteAppliesAttributeDrift(t *testing.T) {
	t.Parallel()
	// The apply path runs what the plan rendered, and reports the entry as
	// still present afterwards rather than newly created.
	responses := singleStorageEntryFixture(dockerLocalEntryJSON)
	var dispatched [][]string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		dispatched = append(dispatched, in.Args)
		return subprocess.ExecCommandResponse{Stdout: responses[strings.Join(in.Args, " ")]}, nil
	})

	task := StorageEntryTask{Name: "app-data", Chown: "root", State: StatePresent}
	result := task.Execute(ctx)
	if result.Error != nil {
		t.Fatalf("expected the apply to succeed, got: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for an attribute change")
	}
	if result.State != StatePresent {
		t.Errorf("expected state %q, got %q", StatePresent, result.State)
	}
	want := []string{"--quiet", "storage:set", "app-data", "chown", "root"}
	if len(dispatched) != 2 || !equalStrings(dispatched[1], want) {
		t.Errorf("expected the probe then %v, got %v", want, dispatched)
	}
}
