package tasks

import (
	"archive/tar"
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"testing"
)

// writeTempTarball writes a tar archive built from name->body pairs to a temp
// file and returns its path. docket reads the file locally and streams it to
// the plugin over stdin, so the file only needs to be readable by the test
// process.
func writeTempTarball(t *testing.T, entries map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "maintenance.tar")

	names := make([]string, 0, len(entries))
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)

	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	for _, name := range names {
		body := entries[name]
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body))}); err != nil {
			t.Fatalf("write header %q: %v", name, err)
		}
		if _, err := tw.Write([]byte(body)); err != nil {
			t.Fatalf("write body %q: %v", name, err)
		}
	}
	if err := tw.Close(); err != nil {
		t.Fatalf("close tar: %v", err)
	}
	if err := os.WriteFile(path, buf.Bytes(), 0o644); err != nil {
		t.Fatalf("write tarball: %v", err)
	}
	return path
}

func TestIntegrationMaintenanceCustomPage(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "maintenance")

	appName := "docket-test-maintenance-custom-page"

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// A freshly-created app has no custom page. If the installed plugin does
	// not report the checksum, idempotency cannot be verified, so skip.
	checksum, reported, err := maintenanceCustomPageState(appName)
	if err != nil {
		t.Fatalf("maintenanceCustomPageState failed: %v", err)
	}
	if !reported {
		t.Skip("skipping: installed dokku-maintenance does not report custom-page-sha256")
	}
	if checksum != "" {
		t.Fatalf("expected newly-created app to have no custom page, got checksum %q", checksum)
	}

	// --- inline content source ---
	content := "<html><body>inline down for maintenance</body></html>\n"
	setInline := MaintenanceCustomPageTask{App: appName, Content: content, State: StatePresent}
	result := setInline.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set inline custom page: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first inline set")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// cross-check: the live report checksum must equal the locally-computed one
	inlineTar, err := buildMaintenancePageTarball(content)
	if err != nil {
		t.Fatalf("buildMaintenancePageTarball failed: %v", err)
	}
	wantInline, err := maintenanceTarballChecksum(inlineTar)
	if err != nil {
		t.Fatalf("maintenanceTarballChecksum failed: %v", err)
	}
	gotInline, _, err := maintenanceCustomPageState(appName)
	if err != nil {
		t.Fatalf("maintenanceCustomPageState failed: %v", err)
	}
	if gotInline != wantInline {
		t.Errorf("report checksum %q != locally-computed %q", gotInline, wantInline)
	}

	// reapply identical content - idempotent
	result = setInline.Execute()
	if result.Error != nil {
		t.Fatalf("failed second inline set: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent inline set")
	}

	// change the content - should update
	setInlineV2 := MaintenanceCustomPageTask{App: appName, Content: content + "<!-- v2 -->\n", State: StatePresent}
	result = setInlineV2.Execute()
	if result.Error != nil {
		t.Fatalf("failed to update inline custom page: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when content changes")
	}

	// --- tarball source (multi-file) ---
	tarPath := writeTempTarball(t, map[string]string{
		"maintenance.html": "<html><body>tarball down</body></html>\n",
		"assets/style.css": "body { color: #333; }\n",
	})
	setTarball := MaintenanceCustomPageTask{App: appName, Tarball: tarPath, State: StatePresent}
	result = setTarball.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set tarball custom page: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when switching to tarball page")
	}

	rawTar, err := os.ReadFile(tarPath)
	if err != nil {
		t.Fatalf("read tarball: %v", err)
	}
	wantTarball, err := maintenanceTarballChecksum(rawTar)
	if err != nil {
		t.Fatalf("maintenanceTarballChecksum failed: %v", err)
	}
	gotTarball, _, err := maintenanceCustomPageState(appName)
	if err != nil {
		t.Fatalf("maintenanceCustomPageState failed: %v", err)
	}
	if gotTarball != wantTarball {
		t.Errorf("report checksum %q != locally-computed %q", gotTarball, wantTarball)
	}

	// reapply identical tarball - idempotent
	result = setTarball.Execute()
	if result.Error != nil {
		t.Fatalf("failed second tarball set: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent tarball set")
	}

	// --- remove ---
	removeTask := MaintenanceCustomPageTask{App: appName, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove custom page: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	gotAfter, _, err := maintenanceCustomPageState(appName)
	if err != nil {
		t.Fatalf("maintenanceCustomPageState failed: %v", err)
	}
	if gotAfter != "" {
		t.Errorf("expected no custom page after remove, got checksum %q", gotAfter)
	}

	// remove again - idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent remove")
	}
}
