package tasks

import (
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestIntegrationStorageMount(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount"
	hostDir := "/var/lib/dokku/data/storage/docket-test-mount"
	containerDir := "/app/storage"
	entryName := "docket-test-mount-entry"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// ensure storage directory exists
	subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
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
	result := mountTask.Execute(testCtx())
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
	result = mountTask.Execute(testCtx())
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
	result = unmountTask.Execute(testCtx())
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
	result = unmountTask.Execute(testCtx())
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
	if r := entry.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute(testCtx())
	}()

	namedMount := StorageMountTask{
		App:          appName,
		EntryName:    entryName,
		ContainerDir: "/app/named",
		Phases:       []string{"deploy", "run"},
		State:        StatePresent,
	}
	result = namedMount.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to mount named entry: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new named-entry mount")
	}

	// idempotent re-apply
	result = namedMount.Execute(testCtx())
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
	result = namedUnmount.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to unmount named entry: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for named-entry unmount")
	}
	result = namedUnmount.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent named-entry unmount failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent named-entry mount")
	}
}

func TestIntegrationStorageMountVolumeOptions(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount-opts"
	hostDir := "/var/lib/dokku/data/storage/docket-test-mount-opts"
	containerDir := "/app/storage"
	entryName := "docket-test-mount-opts-entry"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "mkdir",
		Args:    []string{"-p", hostDir},
	})

	// legacy form with SELinux Z: mount then verify idempotency
	withOpts := StorageMountTask{
		App:           appName,
		HostDir:       hostDir,
		ContainerDir:  containerDir,
		VolumeOptions: "Z",
		State:         StatePresent,
	}
	result := withOpts.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to mount with volume_options=Z: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new legacy mount with options")
	}
	result = withOpts.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent mount with volume_options failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged legacy mount with options")
	}

	// dropping volume_options should surface as drift on the existing mount,
	// which the plan must render as an in-place modify (~), not a create (+).
	withoutOpts := StorageMountTask{
		App:          appName,
		HostDir:      hostDir,
		ContainerDir: containerDir,
		State:        StatePresent,
	}
	if plan := withoutOpts.Plan(testCtx()); plan.Status != PlanStatusModify {
		t.Errorf("expected Modify status for volume_options drift on an existing mount, got %q (reason %q)", plan.Status, plan.Reason)
	}
	result = withoutOpts.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to re-mount without volume_options: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true when volume_options is dropped")
	}

	// unmount (absent ignores volume_options - identity is source+container)
	unmount := StorageMountTask{
		App:          appName,
		HostDir:      hostDir,
		ContainerDir: containerDir,
		State:        StateAbsent,
	}
	if r := unmount.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to unmount legacy with options: %v", r.Error)
	}

	// named-entry form with multi-option round-trip
	entry := StorageEntryTask{Name: entryName, Chown: "herokuish", State: StatePresent}
	if r := entry.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute(testCtx())
	}()

	namedWithOpts := StorageMountTask{
		App:           appName,
		EntryName:     entryName,
		ContainerDir:  "/app/named",
		VolumeOptions: "noexec,nosuid",
		State:         StatePresent,
	}
	result = namedWithOpts.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to mount named entry with options: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for new named-entry mount with options")
	}
	result = namedWithOpts.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent named-entry mount with options failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for unchanged named-entry mount with options")
	}

	namedUnmount := StorageMountTask{
		App:          appName,
		EntryName:    entryName,
		ContainerDir: "/app/named",
		State:        StateAbsent,
	}
	if r := namedUnmount.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to unmount named entry with options: %v", r.Error)
	}
}

// TestIntegrationStorageMountExportRoundTrip mounts a named entry with the full
// set of mount-time attributes and verifies the exporter reconstructs every one
// of them from storage:report, such that re-planning the exported body reports
// no drift.
func TestIntegrationStorageMountExportRoundTrip(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount-export"
	entryName := "docket-test-mount-export-entry"
	containerDir := "/app/exported"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	entry := StorageEntryTask{Name: entryName, Chown: "herokuish", State: StatePresent}
	if r := entry.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute(testCtx())
	}()

	mount := StorageMountTask{
		App:          appName,
		EntryName:    entryName,
		ContainerDir: containerDir,
		ProcessType:  "web",
		Subpath:      "nested",
		Readonly:     true,
		State:        StatePresent,
	}
	if r := mount.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to mount named entry: %v", r.Error)
	}

	bodies, err := StorageMountTask{}.ExportApp(testCtx(), appName)
	if err != nil {
		t.Fatalf("ExportApp: %v", err)
	}
	if len(bodies) != 1 {
		t.Fatalf("expected 1 exported task, got %d", len(bodies))
	}
	got := bodies[0].(StorageMountTask)
	if got.State != StateSet {
		t.Errorf("expected state set, got %q", got.State)
	}
	if got.ProcessType != "web" {
		t.Errorf("expected process_type web, got %q", got.ProcessType)
	}
	if len(got.Mounts) != 1 {
		t.Fatalf("expected 1 exported mount, got %d", len(got.Mounts))
	}
	m := got.Mounts[0]
	if m.EntryName != entryName {
		t.Errorf("expected entry_name %q, got %q", entryName, m.EntryName)
	}
	if m.ContainerDir != containerDir {
		t.Errorf("expected container_dir %q, got %q", containerDir, m.ContainerDir)
	}
	if m.Subpath != "nested" {
		t.Errorf("expected subpath nested, got %q", m.Subpath)
	}
	if !m.Readonly {
		t.Error("expected readonly true, got false")
	}
	// phases default to {deploy, run}, so the exporter omits the field.
	if len(m.Phases) != 0 {
		t.Errorf("expected default phases to be omitted, got %v", m.Phases)
	}

	if plan := got.Plan(testCtx()); !plan.InSync {
		t.Errorf("exported mounts should report no drift, got status %v mutations %v", plan.Status, plan.Mutations)
	}
}

// TestIntegrationStorageMountList drives the mounts list form through every
// state against a live dokku: set converges the scope in one write (removing
// a mount attached out of band), present and absent change only the listed
// mounts, clear empties the scope, and none of them touch another process
// type's mounts.
func TestIntegrationStorageMountList(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount-list"
	entryName := "docket-test-mount-list-entry"
	hostDir := "/var/lib/dokku/data/storage/docket-test-mount-list"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "mkdir",
		Args:    []string{"-p", hostDir},
	})

	entry := StorageEntryTask{Name: entryName, Chown: "herokuish", State: StatePresent}
	if r := entry.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute(testCtx())
	}()

	mustApply := func(label string, task StorageMountTask, wantChanged bool) {
		t.Helper()
		result := task.Execute(testCtx())
		if result.Error != nil {
			t.Fatalf("%s: %v", label, result.Error)
		}
		if result.Changed != wantChanged {
			t.Errorf("%s: changed = %v, want %v", label, result.Changed, wantChanged)
		}
	}
	scopeMounts := func(scope string) []string {
		t.Helper()
		attachments, err := scopeAttachments(testCtx(), appName, scope)
		if err != nil {
			t.Fatalf("read attachments: %v", err)
		}
		var out []string
		for _, a := range attachments {
			out = append(out, a.ContainerPath)
		}
		return out
	}

	// A web mount the _default_ tasks below must leave alone.
	web := StorageMountTask{
		App:         appName,
		ProcessType: "web",
		Mounts:      []StorageMount{{EntryName: entryName, ContainerDir: "/app/web", Subpath: "web"}},
		State:       StateSet,
	}
	mustApply("set web", web, true)
	mustApply("set web again", web, false)

	set := StorageMountTask{
		App: appName,
		Mounts: []StorageMount{
			{EntryName: entryName, ContainerDir: "/app/uploads", Subpath: "uploads", Readonly: true},
			{EntryName: entryName, ContainerDir: "/app/cache", Subpath: "cache", Phases: []string{"run"}},
			{HostDir: hostDir, ContainerDir: "/app/shared", VolumeOptions: "Z"},
		},
		State: StateSet,
	}
	mustApply("set", set, true)
	if plan := set.Plan(testCtx()); !plan.InSync {
		t.Errorf("set should be in sync after apply, got %v", plan.Mutations)
	}

	// A mount attached out of band is removed by the next set.
	if _, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "storage:mount", appName, entryName, "--container-dir", "/app/stray"},
	}); err != nil {
		t.Fatalf("out-of-band mount: %v", err)
	}
	mustApply("set removes the stray mount", set, true)
	if got, want := scopeMounts(storageDefaultProcessType), []string{"/app/cache", "/app/shared", "/app/uploads"}; !equalStrings(got, want) {
		t.Errorf("_default_ mounts = %v, want %v", got, want)
	}

	// Drift in a per-mount field is detected and converged.
	drifted := set
	drifted.Mounts = append([]StorageMount{}, set.Mounts...)
	drifted.Mounts[0].Readonly = false
	mustApply("set with changed readonly", drifted, true)
	mustApply("set again", drifted, false)

	present := StorageMountTask{
		App:    appName,
		Mounts: []StorageMount{{EntryName: entryName, ContainerDir: "/app/tmp", VolumeOptions: "noexec"}},
		State:  StatePresent,
	}
	mustApply("present", present, true)
	mustApply("present again", present, false)
	if got, want := scopeMounts(storageDefaultProcessType), []string{"/app/cache", "/app/shared", "/app/tmp", "/app/uploads"}; !equalStrings(got, want) {
		t.Errorf("_default_ mounts after present = %v, want %v", got, want)
	}

	absent := StorageMountTask{
		App: appName,
		Mounts: []StorageMount{
			{EntryName: entryName, ContainerDir: "/app/tmp"},
			{HostDir: hostDir, ContainerDir: "/app/shared"},
		},
		State: StateAbsent,
	}
	mustApply("absent", absent, true)
	mustApply("absent again", absent, false)
	if got, want := scopeMounts(storageDefaultProcessType), []string{"/app/cache", "/app/uploads"}; !equalStrings(got, want) {
		t.Errorf("_default_ mounts after absent = %v, want %v", got, want)
	}

	clear := StorageMountTask{App: appName, State: StateClear}
	mustApply("clear", clear, true)
	mustApply("clear again", clear, false)
	if got := scopeMounts(storageDefaultProcessType); len(got) != 0 {
		t.Errorf("_default_ mounts after clear = %v, want none", got)
	}
	if got, want := scopeMounts("web"), []string{"/app/web"}; !equalStrings(got, want) {
		t.Errorf("web mounts = %v, want %v", got, want)
	}
}

// TestIntegrationStorageMountSingleFieldDrift changes each mount-time field of
// a single mount in turn and verifies the plan reports it, the apply converges
// it in place, and a host_dir mount carrying fields the colon form drops
// converges in one apply.
func TestIntegrationStorageMountSingleFieldDrift(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount-drift"
	entryName := "docket-test-mount-drift-entry"
	hostDir := "/var/lib/dokku/data/storage/docket-test-mount-drift"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "mkdir",
		Args:    []string{"-p", hostDir},
	})

	entry := StorageEntryTask{Name: entryName, Chown: "herokuish", State: StatePresent}
	if r := entry.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute(testCtx())
	}()

	mustApply := func(label string, task StorageMountTask, wantChanged bool) {
		t.Helper()
		result := task.Execute(testCtx())
		if result.Error != nil {
			t.Fatalf("%s: %v", label, result.Error)
		}
		if result.Changed != wantChanged {
			t.Errorf("%s: changed = %v, want %v", label, result.Changed, wantChanged)
		}
	}
	stored := func(scope, containerDir string) *reportAttachment {
		t.Helper()
		attachments, err := scopeAttachments(testCtx(), appName, scope)
		if err != nil {
			t.Fatalf("read attachments: %v", err)
		}
		for i := range attachments {
			if attachments[i].ContainerPath == containerDir {
				return &attachments[i]
			}
		}
		return nil
	}

	mount := StorageMountTask{App: appName, EntryName: entryName, ContainerDir: "/app/named", State: StatePresent}
	mustApply("mount", mount, true)

	steps := []struct {
		name  string
		edit  func(*StorageMountTask)
		check func(reportAttachment) bool
	}{
		{"subpath", func(m *StorageMountTask) { m.Subpath = "nested" }, func(a reportAttachment) bool { return a.Subpath == "nested" }},
		{"phases", func(m *StorageMountTask) { m.Phases = []string{"run"} }, func(a reportAttachment) bool { return equalStrings(a.Phases, []string{"run"}) }},
		{"readonly", func(m *StorageMountTask) { m.Readonly = true }, func(a reportAttachment) bool { return a.Readonly }},
		{"volume_chown", func(m *StorageMountTask) { m.VolumeChown = "herokuish" }, func(a reportAttachment) bool { return a.VolumeChown == "herokuish" }},
	}
	for _, step := range steps {
		step.edit(&mount)
		if plan := mount.Plan(testCtx()); plan.Status != PlanStatusModify {
			t.Errorf("%s: expected Modify, got %q (reason %q)", step.name, plan.Status, plan.Reason)
		}
		mustApply(step.name, mount, true)
		mustApply(step.name+" again", mount, false)
		if a := stored(storageDefaultProcessType, "/app/named"); a == nil || !step.check(*a) {
			t.Errorf("%s: stored attachment = %+v", step.name, a)
		}
	}

	// Dropping every field resets the attachment to dokku's defaults.
	reset := StorageMountTask{App: appName, EntryName: entryName, ContainerDir: "/app/named", State: StatePresent}
	mustApply("reset", reset, true)
	mustApply("reset again", reset, false)

	hostMount := StorageMountTask{
		App:          appName,
		HostDir:      hostDir,
		ContainerDir: "/app/host",
		Phases:       []string{"deploy"},
		Readonly:     true,
		VolumeChown:  "herokuish",
		State:        StatePresent,
	}
	mustApply("host_dir with fields", hostMount, true)
	mustApply("host_dir with fields again", hostMount, false)

	webMount := StorageMountTask{App: appName, HostDir: hostDir, ContainerDir: "/app/web-host", ProcessType: "web", State: StatePresent}
	mustApply("host_dir for web", webMount, true)
	mustApply("host_dir for web again", webMount, false)
	if a := stored(storageDefaultProcessType, "/app/web-host"); a != nil {
		t.Errorf("host_dir for web left a _default_ attachment: %+v", a)
	}
	if a := stored("web", "/app/web-host"); a == nil {
		t.Error("host_dir for web is missing from the web scope")
	}
}

// TestIntegrationStorageMountSingleAbsentScope verifies a single-mount absent
// removes only its own process type's attachment when another process type
// mounts the same entry at the same path.
func TestIntegrationStorageMountSingleAbsentScope(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-mount-absent-scope"
	entryName := "docket-test-mount-absent-scope-entry"

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	entry := StorageEntryTask{Name: entryName, Chown: "herokuish", State: StatePresent}
	if r := entry.Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create entry: %v", r.Error)
	}
	defer func() {
		destroy := StorageEntryTask{Name: entryName, State: StateAbsent}
		destroy.Execute(testCtx())
	}()

	for _, processType := range []string{"web", "worker"} {
		mount := StorageMountTask{App: appName, EntryName: entryName, ContainerDir: "/app/shared", ProcessType: processType, State: StatePresent}
		if r := mount.Execute(testCtx()); r.Error != nil {
			t.Fatalf("mount for %s: %v", processType, r.Error)
		}
	}

	unmount := StorageMountTask{App: appName, EntryName: entryName, ContainerDir: "/app/shared", ProcessType: "web", State: StateAbsent}
	if r := unmount.Execute(testCtx()); r.Error != nil || !r.Changed {
		t.Fatalf("absent for web: changed=%v err=%v", r.Changed, r.Error)
	}
	if r := unmount.Execute(testCtx()); r.Error != nil || r.Changed {
		t.Errorf("absent for web again: changed=%v err=%v", r.Changed, r.Error)
	}

	for scope, want := range map[string]int{"web": 0, "worker": 1} {
		attachments, err := scopeAttachments(testCtx(), appName, scope)
		if err != nil {
			t.Fatalf("read %s attachments: %v", scope, err)
		}
		if len(attachments) != want {
			t.Errorf("%s attachments = %+v, want %d", scope, attachments, want)
		}
	}
}
