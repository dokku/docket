package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestStorageMountTaskInvalidState(t *testing.T) {
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/host",
		ContainerDir: "/container",
		State:        "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestStorageMountRequiresExactlyOneSource(t *testing.T) {
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
			result := tc.task.Plan()
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
	task := StorageMountTask{
		App:          "test-app",
		EntryName:    "data",
		ContainerDir: "/app/storage",
		Phases:       []string{"deploy", "boot"},
		State:        StatePresent,
	}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("expected error for invalid phase")
	}
	if !strings.Contains(result.Error.Error(), `invalid phase "boot"`) {
		t.Errorf("expected invalid phase error, got %q", result.Error.Error())
	}
}

func TestStorageMountNamedEntryCommandShape(t *testing.T) {
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
	args := task.mountArgs()
	want := []string{
		"--quiet", "storage:mount", "test-app", "data",
		"--container-dir", "/app/storage",
		"--phase", "deploy", "--phase", "run",
		"--process-type", "web",
		"--volume-subpath", "sub",
		"--volume-readonly",
		"--volume-chown", "herokuish",
		"--volume-options", "noexec,nosuid",
	}
	if !equalStrings(args, want) {
		t.Errorf("mountArgs mismatch:\n  got: %v\n want: %v", args, want)
	}
}

func TestStorageMountLegacyFirstMountCommandShape(t *testing.T) {
	task := StorageMountTask{
		App:          "test-app",
		HostDir:      "/var/data",
		ContainerDir: "/app/storage",
	}
	args := task.mountArgs()
	want := []string{"--quiet", "storage:mount", "test-app", "/var/data:/app/storage"}
	if !equalStrings(args, want) {
		t.Errorf("legacy first-mount args mismatch:\n  got: %v\n want: %v", args, want)
	}
}

func TestStorageMountLegacyFirstMountWithVolumeOptions(t *testing.T) {
	task := StorageMountTask{
		App:           "test-app",
		HostDir:       "/var/data",
		ContainerDir:  "/app/storage",
		VolumeOptions: "Z",
	}
	args := task.mountArgs()
	want := []string{"--quiet", "storage:mount", "test-app", "/var/data:/app/storage:Z"}
	if !equalStrings(args, want) {
		t.Errorf("legacy first-mount with volume_options mismatch:\n  got: %v\n want: %v", args, want)
	}
	for _, a := range args {
		if a == "--volume-options" {
			t.Errorf("legacy first-mount must not emit --volume-options flag (carried in colon spec): %v", args)
		}
	}
}

func TestStorageMountNamedRemediationFromLegacy(t *testing.T) {
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet storage:report " + app + " --format json": report,
	}))()

	bodies, err := StorageMountTask{}.ExportApp(app)
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

func TestStorageMountExportNamedEntryFullFidelity(t *testing.T) {
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
	tasks := exportMounts(t, "node-js-app", report)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	checks := []struct {
		name      string
		got, want string
	}{
		{"app", got.App, "node-js-app"},
		{"entry_name", got.EntryName, "data"},
		{"host_dir", got.HostDir, ""},
		{"container_dir", got.ContainerDir, "/app/storage"},
		{"process_type", got.ProcessType, "web"},
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
	report := `{
		"attachment.1.entry-name": "logs",
		"attachment.1.host-path": "/var/lib/dokku/data/storage/logs",
		"attachment.1.container-path": "/app/logs",
		"attachment.1.phases": "run",
		"attachment.1.process-type": "_default_",
		"attachment.1.readonly": "false"
	}`
	tasks := exportMounts(t, "node-js-app", report)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	if !equalStrings(got.Phases, []string{"run"}) {
		t.Errorf("expected phases [run], got %v", got.Phases)
	}
	if got.ProcessType != "" {
		t.Errorf("expected default process_type to be omitted, got %q", got.ProcessType)
	}
	if got.Readonly {
		t.Errorf("expected readonly false, got true")
	}
	if got.EntryName != "logs" || got.HostDir != "" {
		t.Errorf("expected named-entry form, got entry_name=%q host_dir=%q", got.EntryName, got.HostDir)
	}
}

func TestStorageMountExportLegacyEntryUsesHostDir(t *testing.T) {
	report := `{
		"attachment.1.entry-name": "legacy-abc123def4",
		"attachment.1.host-path": "/var/data",
		"attachment.1.container-path": "/app/storage",
		"attachment.1.phases": "deploy,run"
	}`
	tasks := exportMounts(t, "node-js-app", report)
	if len(tasks) != 1 {
		t.Fatalf("expected 1 task, got %d", len(tasks))
	}
	got := tasks[0]
	if got.HostDir != "/var/data" {
		t.Errorf("expected host_dir /var/data, got %q", got.HostDir)
	}
	if got.EntryName != "" {
		t.Errorf("legacy entry must not surface entry_name, got %q", got.EntryName)
	}
}

func TestStorageMountExportSortsByContainerPath(t *testing.T) {
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
	tasks := exportMounts(t, "node-js-app", report)
	if len(tasks) != 2 {
		t.Fatalf("expected 2 tasks, got %d", len(tasks))
	}
	if tasks[0].ContainerDir != "/app/a" || tasks[1].ContainerDir != "/app/z" {
		t.Errorf("expected container dirs sorted [/app/a /app/z], got [%s %s]",
			tasks[0].ContainerDir, tasks[1].ContainerDir)
	}
}

func TestStorageMountExportRecipeEmitsFields(t *testing.T) {
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet apps:list": "node-js-app",
		"--quiet storage:report node-js-app --format json": report,
	}))()

	res, err := ExportRecipe(ExportOptions{Apps: []string{"node-js-app"}})
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet storage:report node-js-app --format json": report,
	}))()

	task := StorageMountTask{
		App:          "node-js-app",
		EntryName:    "data",
		ContainerDir: "/app/x",
		Phases:       []string{"run"},
		State:        StatePresent,
	}
	plan := task.Plan()
	if plan.Error != nil {
		t.Fatalf("Plan returned error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("expected run-only mount to be InSync, got status %v reason %q", plan.Status, plan.Reason)
	}
}

func TestStorageMountPlanVolumeOptionsDriftReportsModify(t *testing.T) {
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
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet storage:report node-js-app --format json": report,
	}))()

	task := StorageMountTask{
		App:           "node-js-app",
		EntryName:     "data",
		ContainerDir:  "/app/storage",
		VolumeOptions: "noexec,nosuid",
		State:         StatePresent,
	}
	plan := task.Plan()
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
	// No attachment matches the recipe, so the mount is brand new and must
	// render the create marker (+). Pins the default create branch.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet storage:report node-js-app --format json": "{}",
	}))()

	task := StorageMountTask{
		App:          "node-js-app",
		EntryName:    "data",
		ContainerDir: "/app/storage",
		State:        StatePresent,
	}
	plan := task.Plan()
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
