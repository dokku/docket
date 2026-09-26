package tasks

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestStorageMountTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/host",
		ContainerDir: "/container",
		State:        "invalid",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestStorageMountRequiresExactlyOneSource(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name string
		task StorageMountTask
		want string
	}{
		{
			name: "neither set",
			task: StorageMountTask{App: "test-app", ContainerDir: "/c", State: StatePresent},
			want: "exactly one of 'entry_name' or 'host_dir' is required",
		},
		{
			name: "both set",
			task: StorageMountTask{App: "test-app", EntryName: "e", HostDir: "/h", ContainerDir: "/c", State: StatePresent},
			want: "'entry_name' and 'host_dir' are mutually exclusive",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			result := tc.task.Plan(testCtx())
			if result.Error == nil {
				t.Fatalf("expected error %q, got nil", tc.want)
			}
			if !strings.Contains(result.Error.Error(), tc.want) {
				t.Errorf("expected error to contain %q, got %q", tc.want, result.Error.Error())
			}
		})
	}
}

func TestStorageMountRejectsInvalidPhase(t *testing.T) {
	t.Parallel()
	task := StorageMountTask{
		App:          "test-app",
		EntryName:    "data",
		ContainerDir: "/app/storage",
		Phases:       []string{"deploy", "boot"},
		State:        StatePresent,
	}
	result := task.Plan(testCtx())
	if result.Error == nil {
		t.Fatal("expected error for invalid phase")
	}
	if !strings.Contains(result.Error.Error(), `invalid phase "boot"`) {
		t.Errorf("expected invalid phase error, got %q", result.Error.Error())
	}
}

func TestStorageMountNamedEntryCommandShape(t *testing.T) {
	t.Parallel()
	task := StorageMountTask{
		App:           "test-app",
		EntryName:     "data",
		ContainerDir:  "/app/storage",
		Phases:        []string{"deploy", "run"},
		ProcessType:   "web",
		Subpath:       "sub",
		Readonly:      true,
		VolumeChown:   "herokuish",
		VolumeOptions: "noexec,nosuid",
	}
	got := firstMountCommands(task, nil)
	want := []string{
		"--quiet storage:mount test-app data --container-dir /app/storage --phase deploy --phase run --process-type web --volume-subpath sub --volume-readonly --volume-chown herokuish --volume-options noexec,nosuid",
	}
	if !equalStrings(got, want) {
		t.Errorf("first mount mismatch:\n  got: %v\n want: %v", got, want)
	}
}

// firstMountCommands renders task.firstMountInputs as one string per command.
func firstMountCommands(task StorageMountTask, attachments []reportAttachment) []string {
	var out []string
	for _, in := range task.firstMountInputs(attachments) {
		out = append(out, strings.Join(in.Args, " "))
	}
	return out
}

func TestStorageMountLegacyEntryName(t *testing.T) {
	t.Parallel()
	// Hashes computed with `printf '<input>' | shasum`, matching dokku's
	// LegacyMountToEntry.
	cases := map[string]string{
		"/var/data": "legacy-a62ee098ef",
		"myvolume":  "legacy-d2d3fba93a", // hashed as "vol:myvolume"
	}
	for host, want := range cases {
		if got := legacyStorageEntryName(host); got != want {
			t.Errorf("legacyStorageEntryName(%q) = %q, want %q", host, got, want)
		}
	}
}

func TestStorageMountLegacyFirstMountCommands(t *testing.T) {
	t.Parallel()
	const legacy = "legacy-a62ee098ef"
	base := StorageMountTask{App: "test-app", HostDir: "/var/data", ContainerDir: "/app/storage"}
	cases := []struct {
		name        string
		edit        func(*StorageMountTask)
		attachments []reportAttachment
		want        []string
	}{
		{
			name: "plain",
			want: []string{"--quiet storage:mount test-app /var/data:/app/storage"},
		},
		{
			name: "volume options ride in the colon spec",
			edit: func(t *StorageMountTask) { t.VolumeOptions = "Z" },
			want: []string{"--quiet storage:mount test-app /var/data:/app/storage:Z"},
		},
		{
			name: "readonly rides in the colon spec",
			edit: func(t *StorageMountTask) { t.Readonly = true },
			want: []string{"--quiet storage:mount test-app /var/data:/app/storage:ro"},
		},
		{
			name: "readonly and volume options",
			edit: func(t *StorageMountTask) { t.Readonly = true; t.VolumeOptions = "Z" },
			want: []string{"--quiet storage:mount test-app /var/data:/app/storage:ro,Z"},
		},
		{
			name: "both phases listed need no upsert",
			edit: func(t *StorageMountTask) { t.Phases = []string{"run", "deploy"} },
			want: []string{"--quiet storage:mount test-app /var/data:/app/storage"},
		},
		{
			name: "fields the colon form drops are upserted",
			edit: func(t *StorageMountTask) {
				t.Phases = []string{"run"}
				t.Subpath = "sub"
				t.VolumeChown = "herokuish"
				t.Readonly = true
			},
			want: []string{
				"--quiet storage:mount test-app /var/data:/app/storage:ro",
				"--quiet storage:mount test-app " + legacy + " --container-dir /app/storage --phase run --volume-subpath sub --volume-readonly --volume-chown herokuish",
			},
		},
		{
			name: "a named process type moves off _default_",
			edit: func(t *StorageMountTask) { t.ProcessType = "web" },
			want: []string{
				"--quiet storage:mount test-app /var/data:/app/storage",
				"--quiet storage:unmount test-app " + legacy + " --container-dir /app/storage",
				"--quiet storage:mount test-app " + legacy + " --container-dir /app/storage --process-type web",
			},
		},
		{
			name:        "an already registered entry skips the colon form",
			edit:        func(t *StorageMountTask) { t.ProcessType = "web" },
			attachments: []reportAttachment{{EntryName: legacy, HostPath: "/var/data", ContainerPath: "/app/storage", ProcessType: "worker"}},
			want:        []string{"--quiet storage:mount test-app " + legacy + " --container-dir /app/storage --process-type web"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			task := base
			if tc.edit != nil {
				tc.edit(&task)
			}
			if got := firstMountCommands(task, tc.attachments); !equalStrings(got, tc.want) {
				t.Errorf("first mount mismatch:\n  got: %v\n want: %v", got, tc.want)
			}
		})
	}
}

func TestStorageMountNamedRemediationFromLegacy(t *testing.T) {
	t.Parallel()
	// Recipe uses host_dir; storage:report discovered the auto-named
	// entry. Drift remediation must upsert via the named-entry CLI.
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/var/data",
		ContainerDir: "/app/storage",
		// VolumeOptions intentionally empty: this represents the user
		// dropping options and expecting dokku to clear them on re-mount.
	}
	args := task.namedMountArgs("legacy-abc123def4")
	want := []string{
		"--quiet", "storage:mount", "test-app", "legacy-abc123def4",
		"--container-dir", "/app/storage",
	}
	if !equalStrings(args, want) {
		t.Errorf("namedMountArgs (drift clear) mismatch:\n  got: %v\n want: %v", args, want)
	}
}

func TestStorageMountNamedRemediationWithOptions(t *testing.T) {
	t.Parallel()
	task := StorageMountTask{
		App:           "test-app",
		HostDir:       "/var/data",
		ContainerDir:  "/app/storage",
		VolumeOptions: "noexec,nosuid",
	}
	args := task.namedMountArgs("legacy-abc123def4")
	want := []string{
		"--quiet", "storage:mount", "test-app", "legacy-abc123def4",
		"--container-dir", "/app/storage",
		"--volume-options", "noexec,nosuid",
	}
	if !equalStrings(args, want) {
		t.Errorf("namedMountArgs (drift set) mismatch:\n  got: %v\n want: %v", args, want)
	}
}

func TestStorageMountNamedUnmount(t *testing.T) {
	t.Parallel()
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/var/data",
		ContainerDir: "/app/storage",
	}
	args := task.namedUnmountArgs("legacy-abc123def4")
	want := []string{"--quiet", "storage:unmount", "test-app", "legacy-abc123def4", "--container-dir", "/app/storage"}
	if !equalStrings(args, want) {
		t.Errorf("namedUnmountArgs mismatch:\n  got: %v\n want: %v", args, want)
	}
}

// exportMounts runs ExportApp against a canned storage:report payload and
// returns the reconstructed tasks, so the export reconstruction can be asserted
// without a live server.
func exportMounts(t *testing.T, app, report string) []StorageMountTask {
	t.Helper()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet storage:report " + app + " --format json": report,
	}))

	bodies, err := StorageMountTask{}.ExportApp(ctx, app)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	out := make([]StorageMountTask, len(bodies))
	for i, b := range bodies {
		mt, ok := b.(StorageMountTask)
		if !ok {
			t.Fatalf("export body %d is %T, want StorageMountTask", i, b)
		}
		out[i] = mt
	}
	return out
}

// exportOneScope asserts ExportApp returned a single mounts-list task with
// state set and returns it.
func exportOneScope(t *testing.T, tasks []StorageMountTask) StorageMountTask {
	t.Helper()
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	if got.State != StateSet {
		t.Errorf("expected state set, got %q", got.State)
	}
	if got.hasSingleMount() {
		t.Errorf("expected the mounts list form, got single-mount fields: %+v", got)
	}
	return got
}

func TestStorageMountExportNamedEntryFullFidelity(t *testing.T) {
	t.Parallel()
	report := `{
		"attachment.1.entry-name": "data",
		"attachment.1.host-path": "/var/lib/dokku/data/storage/data",
		"attachment.1.container-path": "/app/storage",
		"attachment.1.phases": "deploy,run",
		"attachment.1.process-type": "web",
		"attachment.1.subpath": "sub",
		"attachment.1.readonly": "true",
		"attachment.1.volume-options": "noexec,nosuid",
		"attachment.1.volume-chown": "herokuish",
		"deploy-mounts": "/var/lib/dokku/data/storage/data:/app/storage"
	}`
	task := exportOneScope(t, exportMounts(t, "node-js-app", report))
	if task.App != "node-js-app" {
		t.Errorf("app = %q, want node-js-app", task.App)
	}
	if task.ProcessType != "web" {
		t.Errorf("process_type = %q, want web", task.ProcessType)
	}
	if len(task.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(task.Mounts))
	}
	got := task.Mounts[0]
	checks := []struct {
		name      string
		got, want string
	}{
		{"entry_name", got.EntryName, "data"},
		{"host_dir", got.HostDir, ""},
		{"container_dir", got.ContainerDir, "/app/storage"},
		{"subpath", got.Subpath, "sub"},
		{"volume_chown", got.VolumeChown, "herokuish"},
		{"volume_options", got.VolumeOptions, "noexec,nosuid"},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %q, want %q", c.name, c.got, c.want)
		}
	}
	if !got.Readonly {
		t.Error("expected readonly true, got false")
	}
	// phases {deploy,run} is the dokku default, so it is omitted.
	if len(got.Phases) != 0 {
		t.Errorf("expected default phases to be omitted, got %v", got.Phases)
	}
}

func TestStorageMountExportSinglePhaseAndDefaultProcessType(t *testing.T) {
	t.Parallel()
	report := `{
		"attachment.1.entry-name": "logs",
		"attachment.1.host-path": "/var/lib/dokku/data/storage/logs",
		"attachment.1.container-path": "/app/logs",
		"attachment.1.phases": "run",
		"attachment.1.process-type": "_default_",
		"attachment.1.readonly": "false"
	}`
	task := exportOneScope(t, exportMounts(t, "node-js-app", report))
	if task.ProcessType != "" {
		t.Errorf("expected default process_type to be omitted, got %q", task.ProcessType)
	}
	if len(task.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(task.Mounts))
	}
	got := task.Mounts[0]
	if !equalStrings(got.Phases, []string{"run"}) {
		t.Errorf("expected phases [run], got %v", got.Phases)
	}
	if got.Readonly {
		t.Errorf("expected readonly false, got true")
	}
	if got.EntryName != "logs" || got.HostDir != "" {
		t.Errorf("expected named-entry form, got entry_name=%q host_dir=%q", got.EntryName, got.HostDir)
	}
}

func TestStorageMountExportLegacyEntryUsesHostDir(t *testing.T) {
	t.Parallel()
	report := `{
		"attachment.1.entry-name": "legacy-abc123def4",
		"attachment.1.host-path": "/var/data",
		"attachment.1.container-path": "/app/storage",
		"attachment.1.phases": "deploy,run"
	}`
	task := exportOneScope(t, exportMounts(t, "node-js-app", report))
	if len(task.Mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(task.Mounts))
	}
	got := task.Mounts[0]
	if got.HostDir != "/var/data" {
		t.Errorf("expected host_dir /var/data, got %q", got.HostDir)
	}
	if got.EntryName != "" {
		t.Errorf("legacy entry must not surface entry_name, got %q", got.EntryName)
	}
}

func TestStorageMountExportSortsByContainerPath(t *testing.T) {
	t.Parallel()
	// Indices are intentionally out of container-path order to prove the
	// exporter sorts deterministically rather than trusting report order.
	report := `{
		"attachment.1.entry-name": "data",
		"attachment.1.host-path": "/h/data",
		"attachment.1.container-path": "/app/z",
		"attachment.1.phases": "deploy,run",
		"attachment.2.entry-name": "cache",
		"attachment.2.host-path": "/h/cache",
		"attachment.2.container-path": "/app/a",
		"attachment.2.phases": "deploy,run"
	}`
	task := exportOneScope(t, exportMounts(t, "node-js-app", report))
	if len(task.Mounts) != 2 {
		t.Fatalf("expected 2 mounts, got %d", len(task.Mounts))
	}
	if task.Mounts[0].ContainerDir != "/app/a" || task.Mounts[1].ContainerDir != "/app/z" {
		t.Errorf("expected container dirs sorted [/app/a /app/z], got [%s %s]",
			task.Mounts[0].ContainerDir, task.Mounts[1].ContainerDir)
	}
}

func TestStorageMountExportGroupsByProcessType(t *testing.T) {
	t.Parallel()
	// Each process type is its own --replace scope, so each exports as its own
	// state: set task, sorted by scope with _default_ (process_type omitted)
	// first.
	report := `{
		"attachment.1.entry-name": "data",
		"attachment.1.host-path": "/h/data",
		"attachment.1.container-path": "/app/storage",
		"attachment.1.phases": "deploy,run",
		"attachment.1.process-type": "worker",
		"attachment.2.entry-name": "data",
		"attachment.2.host-path": "/h/data",
		"attachment.2.container-path": "/app/data",
		"attachment.2.phases": "deploy,run",
		"attachment.2.process-type": "_default_",
		"attachment.3.entry-name": "cache",
		"attachment.3.host-path": "/h/cache",
		"attachment.3.container-path": "/app/cache",
		"attachment.3.phases": "deploy,run",
		"attachment.3.process-type": "worker"
	}`
	tasks := exportMounts(t, "node-js-app", report)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ProcessType != "" || tasks[1].ProcessType != "worker" {
		t.Fatalf("expected scopes [_default_ worker], got [%q %q]", tasks[0].ProcessType, tasks[1].ProcessType)
	}
	for _, task := range tasks {
		if task.State != StateSet {
			t.Errorf("process_type %q: expected state set, got %q", task.ProcessType, task.State)
		}
	}
	if len(tasks[0].Mounts) != 1 || tasks[0].Mounts[0].ContainerDir != "/app/data" {
		t.Errorf("unexpected _default_ mounts: %+v", tasks[0].Mounts)
	}
	if len(tasks[1].Mounts) != 2 || tasks[1].Mounts[0].ContainerDir != "/app/cache" || tasks[1].Mounts[1].ContainerDir != "/app/storage" {
		t.Errorf("unexpected worker mounts: %+v", tasks[1].Mounts)
	}
}

func TestStorageMountExportFallsBackForInexpressibleScope(t *testing.T) {
	t.Parallel()
	// A key=value volume option cannot be written as a --replace spec token,
	// so the whole web scope falls back to single-mount tasks with state
	// present, while the _default_ scope still exports as a set.
	report := `{
		"attachment.1.entry-name": "nfs",
		"attachment.1.host-path": "/h/nfs",
		"attachment.1.container-path": "/app/nfs",
		"attachment.1.phases": "deploy,run",
		"attachment.1.process-type": "web",
		"attachment.1.volume-options": "addr=10.0.0.1",
		"attachment.2.entry-name": "data",
		"attachment.2.host-path": "/h/data",
		"attachment.2.container-path": "/app/storage",
		"attachment.2.phases": "deploy,run",
		"attachment.2.process-type": "web",
		"attachment.3.entry-name": "data",
		"attachment.3.host-path": "/h/data",
		"attachment.3.container-path": "/app/data",
		"attachment.3.phases": "deploy,run"
	}`
	tasks := exportMounts(t, "node-js-app", report)
	if len(tasks) != 3 {
		t.Fatalf("expected 3 tasks, got %d: %+v", len(tasks), tasks)
	}
	if tasks[0].State != StateSet || len(tasks[0].Mounts) != 1 {
		t.Errorf("expected the _default_ scope as a set, got %+v", tasks[0])
	}
	for _, got := range tasks[1:] {
		if got.State != StatePresent || len(got.Mounts) != 0 || got.ProcessType != "web" {
			t.Errorf("expected a single-mount web task with state present, got %+v", got)
		}
		if err := got.Validate(); err != nil {
			t.Errorf("fallback task must validate: %v", err)
		}
	}
	if tasks[1].ContainerDir != "/app/nfs" || tasks[1].VolumeOptions != "addr=10.0.0.1" {
		t.Errorf("expected the nfs mount first, got %+v", tasks[1])
	}
	if tasks[2].ContainerDir != "/app/storage" {
		t.Errorf("expected the storage mount second, got %+v", tasks[2])
	}
}

func TestStorageMountExportRecipeEmitsFields(t *testing.T) {
	t.Parallel()
	// End-to-end through ExportRecipe: the reconstructed fields must survive
	// YAML marshaling in the user-facing recipe (bool readonly, single-phase
	// list, process_type).
	report := `{
		"attachment.1.entry-name": "data",
		"attachment.1.host-path": "/h/data",
		"attachment.1.container-path": "/app/storage",
		"attachment.1.phases": "deploy",
		"attachment.1.process-type": "web",
		"attachment.1.readonly": "true"
	}`
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet apps:list": "node-js-app",
		"--quiet storage:report node-js-app --format json": report,
	}))

	res, err := ExportRecipe(ctx, ExportOptions{Apps: []string{"node-js-app"}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}
	recipe, err := res.MarshalRecipe("yaml")
	if err != nil {
		t.Fatalf("MarshalRecipe: %v", err)
	}
	out := string(recipe)
	for _, want := range []string{
		"dokku_storage_mount",
		"entry_name: data",
		"container_dir: /app/storage",
		"process_type: web",
		"readonly: true",
		"- deploy",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("recipe missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "- run") {
		t.Errorf("deploy-only mount must not emit the run phase:\n%s", out)
	}
}

func TestStorageMountPlanFindsRunOnlyMount(t *testing.T) {
	t.Parallel()
	// storage:list hides run-only mounts (it reports the deploy phase only);
	// findMount now reads storage:report, so a run-only mount is discovered and
	// the recipe reports InSync instead of a perpetual create.
	report := `{
		"attachment.1.entry-name": "data",
		"attachment.1.host-path": "/h/data",
		"attachment.1.container-path": "/app/x",
		"attachment.1.phases": "run",
		"attachment.1.readonly": "false"
	}`
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet storage:report node-js-app --format json": report,
	}))

	task := StorageMountTask{
		App:          "node-js-app",
		EntryName:    "data",
		ContainerDir: "/app/x",
		Phases:       []string{"run"},
		State:        StatePresent,
	}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan returned error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("expected run-only mount to be InSync, got status %v reason %q", plan.Status, plan.Reason)
	}
}

func TestStorageMountPlanVolumeOptionsDriftReportsModify(t *testing.T) {
	t.Parallel()
	// An attachment already exists and only volume_options drifted: the plan
	// remediates it in place, so it must render the modify marker (~), not the
	// create marker (+). Regression guard for the hardcoded PlanStatusCreate.
	report := `{
		"attachment.1.entry-name": "data",
		"attachment.1.host-path": "/h/data",
		"attachment.1.container-path": "/app/storage",
		"attachment.1.phases": "deploy,run",
		"attachment.1.volume-options": "Z",
		"attachment.1.readonly": "false"
	}`
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet storage:report node-js-app --format json": report,
	}))

	task := StorageMountTask{
		App:           "node-js-app",
		EntryName:     "data",
		ContainerDir:  "/app/storage",
		VolumeOptions: "noexec,nosuid",
		State:         StatePresent,
	}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan returned error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when volume_options differ on an existing mount")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("expected Modify status for in-place volume_options drift, got %q", plan.Status)
	}
	if !strings.Contains(plan.Reason, "volume_options drift") {
		t.Errorf("expected reason to mention volume_options drift, got %q", plan.Reason)
	}
}

func TestStorageMountPlanMissingReportsCreate(t *testing.T) {
	t.Parallel()
	// No attachment matches the recipe, so the mount is brand new and must
	// render the create marker (+). Pins the default create branch.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet storage:report node-js-app --format json": "{}",
	}))

	task := StorageMountTask{
		App:          "node-js-app",
		EntryName:    "data",
		ContainerDir: "/app/storage",
		State:        StatePresent,
	}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan returned error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when no attachment exists")
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected Create status for a brand-new mount, got %q", plan.Status)
	}
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// storageReport renders a storage:report JSON payload for the given
// attachments, the shape readStorageAttachments parses.
func storageReport(attachments ...reportAttachment) string {
	payload := map[string]string{}
	for i, a := range attachments {
		prefix := fmt.Sprintf("attachment.%d.", i+1)
		payload[prefix+"entry-name"] = a.EntryName
		payload[prefix+"host-path"] = a.HostPath
		payload[prefix+"container-path"] = a.ContainerPath
		payload[prefix+"phases"] = strings.Join(canonicalStoragePhases(a.Phases), ",")
		payload[prefix+"process-type"] = effectiveStorageProcessType(a.ProcessType)
		payload[prefix+"subpath"] = a.Subpath
		payload[prefix+"readonly"] = fmt.Sprintf("%t", a.Readonly)
		payload[prefix+"volume-options"] = a.VolumeOptions
		payload[prefix+"volume-chown"] = a.VolumeChown
	}
	out, err := json.Marshal(payload)
	if err != nil {
		panic(err)
	}
	return string(out)
}

const storageReportKey = "--quiet storage:report node-js-app --format json"

// applyStorageMounts plans and applies task against the given report and
// returns the plan plus every dokku call apply made after the probe.
func applyStorageMounts(t *testing.T, task StorageMountTask, report string) (PlanResult, []string) {
	t.Helper()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(map[string]string{storageReportKey: report}, &calls))
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("Plan returned error: %v", plan.Error)
	}
	calls = nil
	if !plan.InSync {
		if result := plan.apply(ctx); result.Error != nil {
			t.Fatalf("apply returned error: %v", result.Error)
		}
	}
	return plan, calls
}

func TestStorageMountValidatesForms(t *testing.T) {
	t.Parallel()
	data := StorageMount{EntryName: "data", ContainerDir: "/app/storage"}
	cases := []struct {
		name string
		task StorageMountTask
		want string
	}{
		{
			name: "nothing set",
			task: StorageMountTask{App: "a", State: StatePresent},
			want: "either 'mounts' or one of 'entry_name' or 'host_dir' is required",
		},
		{
			name: "single form missing container_dir",
			task: StorageMountTask{App: "a", EntryName: "data", State: StatePresent},
			want: "'container_dir' is required",
		},
		{
			name: "mixed forms",
			task: StorageMountTask{App: "a", EntryName: "data", ContainerDir: "/c", Mounts: []StorageMount{data}, State: StatePresent},
			want: "'mounts' and the single-mount fields",
		},
		{
			name: "set with the single form",
			task: StorageMountTask{App: "a", EntryName: "data", ContainerDir: "/c", State: StateSet},
			want: "must not be set for state 'set'; use 'mounts'",
		},
		{
			name: "clear with the single form",
			task: StorageMountTask{App: "a", Readonly: true, State: StateClear},
			want: "must not be set for state 'clear'; use 'mounts'",
		},
		{
			name: "set without mounts",
			task: StorageMountTask{App: "a", State: StateSet},
			want: "'mounts' must not be empty for state 'set'",
		},
		{
			name: "clear with mounts",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{data}, State: StateClear},
			want: "'mounts' must not be set for state 'clear'",
		},
		{
			name: "mount without a source",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{ContainerDir: "/c"}}, State: StateSet},
			want: "mounts[0]: exactly one of 'entry_name' or 'host_dir' is required",
		},
		{
			name: "mount with both sources",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", HostDir: "/h", ContainerDir: "/c"}}, State: StateSet},
			want: "mounts[0]: 'entry_name' and 'host_dir' are mutually exclusive",
		},
		{
			name: "mount without container_dir",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e"}}, State: StateSet},
			want: "mounts[0]: 'container_dir' is required",
		},
		{
			name: "relative container_dir",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "app"}}, State: StateSet},
			want: `container_dir "app" must start with '/'`,
		},
		{
			name: "container_dir with a colon",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/a:b"}}, State: StateSet},
			want: `container_dir "/a:b" must start with '/' and must not contain ':'`,
		},
		{
			name: "entry_name that reads as a host path",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "/e", ContainerDir: "/c"}}, State: StateSet},
			want: `entry_name "/e" must not start with '/'`,
		},
		{
			name: "relative host_dir",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{HostDir: "h", ContainerDir: "/c"}}, State: StateSet},
			want: `host_dir "h" must start with '/'`,
		},
		{
			name: "duplicate container_dir",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{data, {EntryName: "other", ContainerDir: "/app/storage"}}, State: StatePresent},
			want: `mounts[1]: container_dir "/app/storage" is listed more than once`,
		},
		{
			name: "invalid phase",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", Phases: []string{"boot"}}}, State: StateSet},
			want: `invalid phase "boot"`,
		},
		{
			name: "duplicate phase",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", Phases: []string{"run", "run"}}}, State: StateSet},
			want: `phase "run" is listed more than once`,
		},
		{
			name: "subpath with a comma",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", Subpath: "a,b"}}, State: StateSet},
			want: `subpath "a,b" must not contain ','`,
		},
		{
			name: "chown with a comma",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", VolumeChown: "1,2"}}, State: StateSet},
			want: `volume_chown "1,2" must not contain ','`,
		},
		{
			name: "key=value volume option",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", VolumeOptions: "Z,addr=10.0.0.1"}}, State: StatePresent},
			want: `key=value option "addr=10.0.0.1"`,
		},
		{
			name: "ro volume option",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", VolumeOptions: "ro"}}, State: StateSet},
			want: `option "ro"; use readonly instead`,
		},
		{
			name: "empty volume option",
			task: StorageMountTask{App: "a", Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", VolumeOptions: "Z,,noexec"}}, State: StateSet},
			want: "has an empty option",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.task.Validate()
			if err == nil {
				t.Fatalf("expected error containing %q, got nil", tc.want)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("expected error to contain %q, got %q", tc.want, err.Error())
			}
		})
	}
}

func TestStorageMountAbsentIgnoresMountTimeFields(t *testing.T) {
	t.Parallel()
	// Under absent a mount is identified by source and container_dir alone, so
	// fields that would not survive a --replace spec are not an error there.
	task := StorageMountTask{
		App:    "a",
		Mounts: []StorageMount{{EntryName: "e", ContainerDir: "/c", VolumeOptions: "addr=1", Phases: []string{"boot"}}},
		State:  StateAbsent,
	}
	if err := task.Validate(); err != nil {
		t.Errorf("expected absent to ignore mount-time fields, got %v", err)
	}
}

func TestStorageMountSpecRendering(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		mount StorageMount
		want  string
	}{
		{"named entry, defaults", StorageMount{EntryName: "data", ContainerDir: "/app/storage"}, "data:/app/storage"},
		{"host dir", StorageMount{HostDir: "/var/data", ContainerDir: "/app/storage"}, "/var/data:/app/storage"},
		{"default phases in any order", StorageMount{EntryName: "data", ContainerDir: "/c", Phases: []string{"run", "deploy"}}, "data:/c"},
		{
			"every token",
			StorageMount{EntryName: "data", ContainerDir: "/c", Phases: []string{"run"}, Readonly: true, Subpath: "uploads", VolumeChown: "herokuish", VolumeOptions: "Z,noexec"},
			"data:/c:ro,phase=run,volume-subpath=uploads,volume-chown=herokuish,Z,noexec",
		},
	}
	for _, tc := range cases {
		if got := tc.mount.spec(); got != tc.want {
			t.Errorf("%s: spec = %q, want %q", tc.name, got, tc.want)
		}
	}

	// A retained legacy attachment is written back by its registered entry
	// name, not its host path.
	legacy := reportAttachment{EntryName: "legacy-abc123def4", HostPath: "/var/data", ContainerPath: "/app/storage", Phases: []string{"deploy"}}
	got, err := attachmentSpec(legacy)
	if err != nil {
		t.Fatalf("attachmentSpec: %v", err)
	}
	if want := "legacy-abc123def4:/app/storage:phase=deploy"; got != want {
		t.Errorf("attachmentSpec = %q, want %q", got, want)
	}
}

func TestStorageMountSetInSync(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage", Subpath: "s", Readonly: true, VolumeOptions: "Z"},
		reportAttachment{EntryName: "legacy-abc123def4", HostPath: "/var/data", ContainerPath: "/app/uploads", Phases: []string{"run"}},
		// Another scope is never part of the _default_ set.
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/web", ProcessType: "web"},
	)
	task := StorageMountTask{
		App: "node-js-app",
		Mounts: []StorageMount{
			{HostDir: "/var/data", ContainerDir: "/app/uploads", Phases: []string{"run"}},
			{EntryName: "data", ContainerDir: "/app/storage", Subpath: "s", Readonly: true, VolumeOptions: "Z"},
		},
		State: StateSet,
	}
	plan, calls := applyStorageMounts(t, task, report)
	if !plan.InSync {
		t.Errorf("expected in sync, got %v %v", plan.Status, plan.Mutations)
	}
	if len(calls) != 0 {
		t.Errorf("expected no commands, got %v", calls)
	}
}

func TestStorageMountSetDetectsFieldDrift(t *testing.T) {
	t.Parallel()
	base := reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage"}
	mount := StorageMount{EntryName: "data", ContainerDir: "/app/storage"}
	cases := map[string]func(a *reportAttachment){
		"phases":         func(a *reportAttachment) { a.Phases = []string{"deploy"} },
		"subpath":        func(a *reportAttachment) { a.Subpath = "old" },
		"readonly":       func(a *reportAttachment) { a.Readonly = true },
		"volume_chown":   func(a *reportAttachment) { a.VolumeChown = "heroku" },
		"volume_options": func(a *reportAttachment) { a.VolumeOptions = "Z" },
	}
	for name, drift := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			a := base
			drift(&a)
			task := StorageMountTask{App: "node-js-app", Mounts: []StorageMount{mount}, State: StateSet}
			plan, calls := applyStorageMounts(t, task, storageReport(a))
			if plan.InSync {
				t.Fatal("expected drift")
			}
			if plan.Status != PlanStatusModify {
				t.Errorf("expected Modify, got %q", plan.Status)
			}
			if want := []string{"remount data at /app/storage on node-js-app"}; !equalStrings(plan.Mutations, want) {
				t.Errorf("mutations = %v, want %v", plan.Mutations, want)
			}
			want := "--quiet storage:mount node-js-app data:/app/storage --replace --process-type _default_"
			if len(calls) != 1 || calls[0] != want {
				t.Errorf("calls = %v, want [%s]", calls, want)
			}
		})
	}
}

func TestStorageMountSetReplacesSourceAndRemovesExtras(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "old", HostPath: "/h/old", ContainerPath: "/app/storage"},
		reportAttachment{EntryName: "extra", HostPath: "/h/extra", ContainerPath: "/app/extra"},
		reportAttachment{EntryName: "legacy-abc123def4", HostPath: "/var/data", ContainerPath: "/app/legacy"},
		reportAttachment{EntryName: "keep", HostPath: "/h/keep", ContainerPath: "/app/web", ProcessType: "web"},
	)
	task := StorageMountTask{
		App: "node-js-app",
		Mounts: []StorageMount{
			{EntryName: "new", ContainerDir: "/app/storage"},
			{EntryName: "cache", ContainerDir: "/app/cache", Phases: []string{"run"}},
		},
		State: StateSet,
	}
	plan, calls := applyStorageMounts(t, task, report)
	if plan.Status != PlanStatusModify {
		t.Errorf("expected Modify, got %q", plan.Status)
	}
	wantMutations := []string{
		"mount cache at /app/cache on node-js-app",
		"mount new at /app/storage on node-js-app",
		"unmount extra at /app/extra on node-js-app",
		"unmount /var/data:/app/legacy on node-js-app",
		"unmount old at /app/storage on node-js-app",
	}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("mutations:\n  got: %v\n want: %v", plan.Mutations, wantMutations)
	}
	// The command carries the declared list in declared order.
	want := "--quiet storage:mount node-js-app new:/app/storage cache:/app/cache:phase=run --replace --process-type _default_"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("calls = %v, want [%s]", calls, want)
	}
}

func TestStorageMountSetHostDirDoesNotMatchNamedEntry(t *testing.T) {
	t.Parallel()
	// A /host spec always resolves to dokku's legacy-<hash> entry, so a named
	// entry that happens to share the host path is not the declared mount.
	report := storageReport(reportAttachment{EntryName: "data", HostPath: "/var/data", ContainerPath: "/app/storage"})
	task := StorageMountTask{
		App:    "node-js-app",
		Mounts: []StorageMount{{HostDir: "/var/data", ContainerDir: "/app/storage"}},
		State:  StateSet,
	}
	plan, _ := applyStorageMounts(t, task, report)
	want := []string{"mount /var/data:/app/storage on node-js-app", "unmount data at /app/storage on node-js-app"}
	if !equalStrings(plan.Mutations, want) {
		t.Errorf("mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestStorageMountSetOnEmptyScopeIsACreate(t *testing.T) {
	t.Parallel()
	// A mount in another scope does not make the web scope non-empty.
	report := storageReport(reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage"})
	task := StorageMountTask{
		App:         "node-js-app",
		ProcessType: "web",
		Mounts:      []StorageMount{{EntryName: "data", ContainerDir: "/app/web"}},
		State:       StateSet,
	}
	plan, calls := applyStorageMounts(t, task, report)
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected Create, got %q", plan.Status)
	}
	want := "--quiet storage:mount node-js-app data:/app/web --replace --process-type web"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("calls = %v, want [%s]", calls, want)
	}
}

func TestStorageMountClear(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "b", HostPath: "/h/b", ContainerPath: "/app/b"},
		reportAttachment{EntryName: "a", HostPath: "/h/a", ContainerPath: "/app/a"},
		reportAttachment{EntryName: "w", HostPath: "/h/w", ContainerPath: "/app/w", ProcessType: "web"},
	)
	task := StorageMountTask{App: "node-js-app", State: StateClear}
	plan, calls := applyStorageMounts(t, task, report)
	if plan.Status != PlanStatusDestroy {
		t.Errorf("expected Destroy, got %q", plan.Status)
	}
	wantMutations := []string{"unmount a at /app/a on node-js-app", "unmount b at /app/b on node-js-app"}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("mutations = %v, want %v", plan.Mutations, wantMutations)
	}
	// Without --process-type dokku removes every scope, so _default_ is
	// always named.
	want := "--quiet storage:unmount node-js-app --all --process-type _default_"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("calls = %v, want [%s]", calls, want)
	}

	webOnly := storageReport(reportAttachment{EntryName: "w", HostPath: "/h/w", ContainerPath: "/app/w", ProcessType: "web"})
	if plan, _ := applyStorageMounts(t, task, webOnly); !plan.InSync {
		t.Errorf("expected clear of an empty _default_ scope to be in sync, got %v", plan.Mutations)
	}
}

func TestStorageMountPresentMergesIntoScope(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "keep", HostPath: "/h/keep", ContainerPath: "/app/keep", Readonly: true, VolumeChown: "heroku"},
		reportAttachment{EntryName: "legacy-abc123def4", HostPath: "/var/data", ContainerPath: "/app/legacy", Phases: []string{"run"}},
		reportAttachment{EntryName: "old", HostPath: "/h/old", ContainerPath: "/app/storage"},
	)
	task := StorageMountTask{
		App: "node-js-app",
		Mounts: []StorageMount{
			{EntryName: "new", ContainerDir: "/app/storage"},
			{EntryName: "cache", ContainerDir: "/app/cache"},
		},
		State: StatePresent,
	}
	plan, calls := applyStorageMounts(t, task, report)
	if plan.Status != PlanStatusModify {
		t.Errorf("expected Modify for a source swap, got %q", plan.Status)
	}
	wantMutations := []string{
		"mount cache at /app/cache on node-js-app",
		"mount new at /app/storage on node-js-app",
		"unmount old at /app/storage on node-js-app",
	}
	if !equalStrings(plan.Mutations, wantMutations) {
		t.Errorf("mutations:\n  got: %v\n want: %v", plan.Mutations, wantMutations)
	}
	// Retained mounts keep report order and every attribute; the override sits
	// in place and the new mount is appended.
	want := "--quiet storage:mount node-js-app keep:/app/keep:ro,volume-chown=heroku legacy-abc123def4:/app/legacy:phase=run new:/app/storage cache:/app/cache --replace --process-type _default_"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("calls:\n  got: %v\n want: [%s]", calls, want)
	}
}

func TestStorageMountPresentOnlyNewMountsIsACreate(t *testing.T) {
	t.Parallel()
	report := storageReport(reportAttachment{EntryName: "keep", HostPath: "/h/keep", ContainerPath: "/app/keep"})
	task := StorageMountTask{App: "node-js-app", Mounts: []StorageMount{{EntryName: "cache", ContainerDir: "/app/cache"}}, State: StatePresent}
	plan, _ := applyStorageMounts(t, task, report)
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected Create, got %q", plan.Status)
	}
	task.Mounts = []StorageMount{{EntryName: "keep", ContainerDir: "/app/keep"}}
	if plan, _ := applyStorageMounts(t, task, report); !plan.InSync {
		t.Errorf("expected a present mount to be in sync, got %v", plan.Mutations)
	}
}

func TestStorageMountAbsentKeepsTheRestOfTheScope(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "keep", HostPath: "/h/keep", ContainerPath: "/app/keep", Subpath: "s"},
		reportAttachment{EntryName: "legacy-abc123def4", HostPath: "/var/data", ContainerPath: "/app/legacy"},
		reportAttachment{EntryName: "other", HostPath: "/h/other", ContainerPath: "/app/other"},
	)
	task := StorageMountTask{
		App: "node-js-app",
		Mounts: []StorageMount{
			// Fields other than the source are ignored under absent.
			{HostDir: "/var/data", ContainerDir: "/app/legacy", Readonly: true},
			// A different source at the path is not the listed mount.
			{EntryName: "not-other", ContainerDir: "/app/other"},
			{EntryName: "missing", ContainerDir: "/app/missing"},
		},
		State: StateAbsent,
	}
	plan, calls := applyStorageMounts(t, task, report)
	if plan.Status != PlanStatusDestroy {
		t.Errorf("expected Destroy, got %q", plan.Status)
	}
	if want := []string{"unmount /var/data:/app/legacy on node-js-app"}; !equalStrings(plan.Mutations, want) {
		t.Errorf("mutations = %v, want %v", plan.Mutations, want)
	}
	want := "--quiet storage:mount node-js-app keep:/app/keep:volume-subpath=s other:/app/other --replace --process-type _default_"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("calls:\n  got: %v\n want: [%s]", calls, want)
	}
}

func TestStorageMountAbsentEmptyingTheScopeUnmountsAll(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage", ProcessType: "web"},
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/other"},
	)
	task := StorageMountTask{
		App:         "node-js-app",
		ProcessType: "web",
		Mounts:      []StorageMount{{EntryName: "data", ContainerDir: "/app/storage"}},
		State:       StateAbsent,
	}
	_, calls := applyStorageMounts(t, task, report)
	want := "--quiet storage:unmount node-js-app --all --process-type web"
	if len(calls) != 1 || calls[0] != want {
		t.Errorf("calls = %v, want [%s]", calls, want)
	}

	task.Mounts = []StorageMount{{EntryName: "data", ContainerDir: "/app/missing"}}
	if plan, _ := applyStorageMounts(t, task, report); !plan.InSync {
		t.Errorf("expected absent of an unmounted path to be in sync, got %v", plan.Mutations)
	}
}

func TestStorageMountRetainedMountThatCannotBeCarried(t *testing.T) {
	t.Parallel()
	// A key=value option set through --volume-options on the single-entry form
	// cannot be written back as a --replace spec token.
	report := storageReport(
		reportAttachment{EntryName: "nfs", HostPath: "/h/nfs", ContainerPath: "/app/nfs", VolumeOptions: "addr=10.0.0.1"},
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage"},
	)
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{storageReportKey: report}))
	for _, state := range []State{StatePresent, StateAbsent} {
		mounts := []StorageMount{{EntryName: "cache", ContainerDir: "/app/cache"}}
		if state == StateAbsent {
			mounts = []StorageMount{{EntryName: "data", ContainerDir: "/app/storage"}}
		}
		task := StorageMountTask{App: "node-js-app", Mounts: mounts, State: state}
		plan := task.Plan(ctx)
		if plan.Error == nil {
			t.Fatalf("%s: expected an error for the nfs mount", state)
		}
		if !strings.Contains(plan.Error.Error(), "mount nfs at /app/nfs cannot be carried") {
			t.Errorf("%s: unexpected error %q", state, plan.Error.Error())
		}
	}

	// In sync needs no write, so the unrelated mount is not an error, and set
	// drops the mount rather than writing it back.
	inSync := StorageMountTask{App: "node-js-app", Mounts: []StorageMount{{EntryName: "data", ContainerDir: "/app/storage"}}, State: StatePresent}
	if plan := inSync.Plan(ctx); plan.Error != nil || !plan.InSync {
		t.Errorf("expected in sync without error, got %v / %v", plan.Error, plan.Mutations)
	}
	set := StorageMountTask{App: "node-js-app", Mounts: []StorageMount{{EntryName: "data", ContainerDir: "/app/storage"}}, State: StateSet}
	if plan := set.Plan(ctx); plan.Error != nil {
		t.Errorf("expected set to drop the mount without error, got %v", plan.Error)
	}
}

func TestStorageMountSinglePlanRespectsProcessType(t *testing.T) {
	t.Parallel()
	// dokku keys an attachment on (entry, container path, process type): the
	// web mount does not satisfy a worker recipe for the same entry and path.
	report := storageReport(reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage", ProcessType: "web"})
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{storageReportKey: report}))
	task := StorageMountTask{App: "node-js-app", EntryName: "data", ContainerDir: "/app/storage", ProcessType: "worker", State: StatePresent}
	if plan := task.Plan(ctx); plan.InSync || plan.Status != PlanStatusCreate {
		t.Errorf("expected a worker mount to be created, got in_sync=%v status=%q", plan.InSync, plan.Status)
	}
	task.ProcessType = "web"
	if plan := task.Plan(ctx); !plan.InSync {
		t.Errorf("expected the web mount to be in sync, got %v", plan.Mutations)
	}
	task.State = StateAbsent
	task.ProcessType = ""
	if plan := task.Plan(ctx); !plan.InSync {
		t.Errorf("expected absent in the _default_ scope to leave the web mount alone, got %v", plan.Mutations)
	}
}

func TestStorageMountSingleDetectsFieldDrift(t *testing.T) {
	t.Parallel()
	base := reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage"}
	cases := []struct {
		name   string
		report func(a *reportAttachment)
		recipe func(t *StorageMountTask)
		reason string
		want   string
	}{
		{
			name:   "phases",
			report: func(a *reportAttachment) { a.Phases = []string{"deploy"} },
			reason: `phases drift (have "deploy", want "deploy,run")`,
			want:   "--quiet storage:mount node-js-app data --container-dir /app/storage",
		},
		{
			name:   "subpath",
			recipe: func(t *StorageMountTask) { t.Subpath = "new" },
			report: func(a *reportAttachment) { a.Subpath = "old" },
			reason: `subpath drift (have "old", want "new")`,
			want:   "--quiet storage:mount node-js-app data --container-dir /app/storage --volume-subpath new",
		},
		{
			name:   "readonly set",
			recipe: func(t *StorageMountTask) { t.Readonly = true },
			reason: "readonly drift (have false, want true)",
			want:   "--quiet storage:mount node-js-app data --container-dir /app/storage --volume-readonly",
		},
		{
			name:   "readonly cleared",
			report: func(a *reportAttachment) { a.Readonly = true },
			reason: "readonly drift (have true, want false)",
			want:   "--quiet storage:mount node-js-app data --container-dir /app/storage",
		},
		{
			name:   "volume_chown",
			recipe: func(t *StorageMountTask) { t.VolumeChown = "herokuish" },
			reason: `volume_chown drift (have "", want "herokuish")`,
			want:   "--quiet storage:mount node-js-app data --container-dir /app/storage --volume-chown herokuish",
		},
		{
			name: "every drifted field is named",
			recipe: func(t *StorageMountTask) {
				t.Phases = []string{"run"}
				t.VolumeOptions = "Z"
			},
			report: func(a *reportAttachment) { a.Subpath = "old" },
			reason: `phases drift (have "deploy,run", want "run"); subpath drift (have "old", want ""); volume_options drift (have "", want "Z")`,
			want:   "--quiet storage:mount node-js-app data --container-dir /app/storage --phase run --volume-options Z",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a := base
			if tc.report != nil {
				tc.report(&a)
			}
			task := StorageMountTask{App: "node-js-app", EntryName: "data", ContainerDir: "/app/storage", State: StatePresent}
			if tc.recipe != nil {
				tc.recipe(&task)
			}
			plan, calls := applyStorageMounts(t, task, storageReport(a))
			if plan.InSync {
				t.Fatal("expected drift")
			}
			if plan.Status != PlanStatusModify {
				t.Errorf("expected Modify, got %q", plan.Status)
			}
			if plan.Reason != tc.reason {
				t.Errorf("reason = %q, want %q", plan.Reason, tc.reason)
			}
			if want := []string{"remount data at /app/storage on node-js-app"}; !equalStrings(plan.Mutations, want) {
				t.Errorf("mutations = %v, want %v", plan.Mutations, want)
			}
			if len(calls) != 1 || calls[0] != tc.want {
				t.Errorf("calls = %v, want [%s]", calls, tc.want)
			}
		})
	}
}

func TestStorageMountSingleInSyncWithEveryField(t *testing.T) {
	t.Parallel()
	report := storageReport(reportAttachment{
		EntryName: "legacy-a62ee098ef", HostPath: "/var/data", ContainerPath: "/app/storage", ProcessType: "web",
		Phases: []string{"deploy", "run"}, Subpath: "s", Readonly: true, VolumeChown: "herokuish", VolumeOptions: "Z",
	})
	task := StorageMountTask{
		App: "node-js-app", HostDir: "/var/data", ContainerDir: "/app/storage", ProcessType: "web",
		Phases: []string{"run", "deploy"}, Subpath: "s", Readonly: true, VolumeChown: "herokuish", VolumeOptions: "Z",
		State: StatePresent,
	}
	plan, calls := applyStorageMounts(t, task, report)
	if !plan.InSync {
		t.Errorf("expected in sync, got %q", plan.Reason)
	}
	if len(calls) != 0 {
		t.Errorf("expected no commands, got %v", calls)
	}
}

func TestStorageMountSingleAbsentStaysInItsScope(t *testing.T) {
	t.Parallel()
	web := reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage", ProcessType: "web"}
	worker := web
	worker.ProcessType = "worker"
	webOther := reportAttachment{EntryName: "cache", HostPath: "/h/cache", ContainerPath: "/app/cache", ProcessType: "web", Subpath: "c"}
	task := StorageMountTask{App: "node-js-app", EntryName: "data", ContainerDir: "/app/storage", ProcessType: "web", State: StateAbsent}

	cases := []struct {
		name   string
		report string
		want   string
	}{
		{
			name:   "sole holder uses the entry unmount",
			report: storageReport(web, webOther),
			want:   "--quiet storage:unmount node-js-app data --container-dir /app/storage",
		},
		{
			name:   "shared path empties the scope",
			report: storageReport(web, worker),
			want:   "--quiet storage:unmount node-js-app --all --process-type web",
		},
		{
			name:   "shared path rewrites the rest of the scope",
			report: storageReport(web, worker, webOther),
			want:   "--quiet storage:mount node-js-app cache:/app/cache:volume-subpath=c --replace --process-type web",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			plan, calls := applyStorageMounts(t, task, tc.report)
			if plan.InSync || plan.Status != PlanStatusDestroy {
				t.Fatalf("expected a destroy, got in_sync=%v status=%q", plan.InSync, plan.Status)
			}
			if want := []string{"unmount data at /app/storage on node-js-app"}; !equalStrings(plan.Mutations, want) {
				t.Errorf("mutations = %v, want %v", plan.Mutations, want)
			}
			if len(calls) != 1 || calls[0] != tc.want {
				t.Errorf("calls = %v, want [%s]", calls, tc.want)
			}
		})
	}
}

func TestStorageMountSingleAbsentUncarriableScope(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage", ProcessType: "web"},
		reportAttachment{EntryName: "data", HostPath: "/h/data", ContainerPath: "/app/storage", ProcessType: "worker"},
		reportAttachment{EntryName: "nfs", ContainerPath: "/app/nfs", ProcessType: "web", VolumeOptions: "addr=10.0.0.1"},
	)
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{storageReportKey: report}))
	task := StorageMountTask{App: "node-js-app", EntryName: "data", ContainerDir: "/app/storage", ProcessType: "web", State: StateAbsent}
	plan := task.Plan(ctx)
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), "another process type also mounts data at /app/storage") {
		t.Errorf("expected an uncarriable-scope error, got %v", plan.Error)
	}
}

func TestStorageMountPlanMutationsAreStableAcrossRuns(t *testing.T) {
	t.Parallel()
	report := storageReport(
		reportAttachment{EntryName: "d", HostPath: "/h/d", ContainerPath: "/app/d"},
		reportAttachment{EntryName: "b", HostPath: "/h/b", ContainerPath: "/app/b"},
		reportAttachment{EntryName: "e", HostPath: "/h/e", ContainerPath: "/app/e", Readonly: true},
		reportAttachment{EntryName: "a", HostPath: "/h/a", ContainerPath: "/app/a"},
	)
	cases := map[string]StorageMountTask{
		"set": {App: "node-js-app", Mounts: []StorageMount{
			{EntryName: "z", ContainerDir: "/app/z"},
			{EntryName: "e", ContainerDir: "/app/e"},
			{EntryName: "c", ContainerDir: "/app/c"},
		}, State: StateSet},
		"present": {App: "node-js-app", Mounts: []StorageMount{
			{EntryName: "z", ContainerDir: "/app/z"},
			{EntryName: "y", ContainerDir: "/app/y"},
			{EntryName: "e", ContainerDir: "/app/e"},
		}, State: StatePresent},
		"absent": {App: "node-js-app", Mounts: []StorageMount{
			{EntryName: "d", ContainerDir: "/app/d"},
			{EntryName: "a", ContainerDir: "/app/a"},
		}, State: StateAbsent},
		"clear": {App: "node-js-app", State: StateClear},
	}
	for name, task := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{storageReportKey: report}))
			first := task.Plan(ctx)
			if first.Error != nil {
				t.Fatalf("unexpected plan error: %v", first.Error)
			}
			if len(first.Mutations) < 2 {
				t.Fatalf("need several mutations to detect a reordering, got %v", first.Mutations)
			}
			for i := 0; i < 50; i++ {
				got := task.Plan(ctx)
				if !reflect.DeepEqual(got.Mutations, first.Mutations) || !reflect.DeepEqual(got.Commands, first.Commands) {
					t.Fatalf("run %d reordered the plan: %v %v, want %v %v", i+2, got.Mutations, got.Commands, first.Mutations, first.Commands)
				}
			}
		})
	}
}
