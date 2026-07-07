package tasks

import (
	"archive/tar"
	"bytes"
	"io"
	"sort"
	"strings"
	"testing"
)

// tarFromEntries builds an uncompressed tar archive from name->body pairs for
// use in the checksum tests.
func tarFromEntries(t *testing.T, entries map[string]string) []byte {
	t.Helper()
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
	return buf.Bytes()
}

// The known-answer digests below were computed independently with the
// dokku-maintenance plugin's own shell algorithm
// (fn-maintenance-custom-page-checksum) so they verify the Go reproduction
// against ground truth, not against itself.
const (
	maintenanceTestPage           = "<html><body>down for maintenance</body></html>\n"
	maintenanceTestCSS            = "body { color: red; }\n"
	maintenanceSingleFileChecksum = "7b645f273842a941c68302a4022ed03e219bd8db318ef32a92dddb148a72ef05"
	maintenanceMultiFileChecksum  = "7c9ca1d8a574bc5d575137b8f8289f4687285ee531d60ae0582017760a4e5627"
)

func TestMaintenanceCustomPageTaskInvalidState(t *testing.T) {
	task := MaintenanceCustomPageTask{App: "test-app", Content: maintenanceTestPage, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestMaintenanceCustomPageTaskMissingApp(t *testing.T) {
	task := MaintenanceCustomPageTask{Content: maintenanceTestPage, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMaintenanceCustomPageTaskPresentMissingSource(t *testing.T) {
	task := MaintenanceCustomPageTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without content or tarball should return an error")
	}
	if !strings.Contains(result.Error.Error(), "one of 'content' or 'tarball' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMaintenanceCustomPageTaskPresentBothSources(t *testing.T) {
	task := MaintenanceCustomPageTask{App: "test-app", Content: maintenanceTestPage, Tarball: "/tmp/page.tar", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with both content and tarball should return an error")
	}
	if !strings.Contains(result.Error.Error(), "mutually exclusive") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMaintenanceCustomPageTaskAbsentWithContent(t *testing.T) {
	task := MaintenanceCustomPageTask{App: "test-app", Content: maintenanceTestPage, State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute absent with content should return an error")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when state is 'absent'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestMaintenanceCustomPageTaskAbsentWithTarball(t *testing.T) {
	task := MaintenanceCustomPageTask{App: "test-app", Tarball: "/tmp/page.tar", State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute absent with tarball should return an error")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when state is 'absent'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuildMaintenancePageTarball(t *testing.T) {
	out, err := buildMaintenancePageTarball(maintenanceTestPage)
	if err != nil {
		t.Fatalf("buildMaintenancePageTarball failed: %v", err)
	}

	tr := tar.NewReader(bytes.NewReader(out))
	got := map[string]string{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read failed: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar entry body read failed: %v", err)
		}
		got[hdr.Name] = string(body)
	}
	if len(got) != 1 {
		t.Fatalf("expected exactly one tar entry, got %d", len(got))
	}
	if got["maintenance.html"] != maintenanceTestPage {
		t.Errorf("maintenance.html body = %q, want %q", got["maintenance.html"], maintenanceTestPage)
	}
}

func TestMaintenanceTarballChecksumSingleFile(t *testing.T) {
	tarBytes, err := buildMaintenancePageTarball(maintenanceTestPage)
	if err != nil {
		t.Fatalf("buildMaintenancePageTarball failed: %v", err)
	}
	sum, err := maintenanceTarballChecksum(tarBytes)
	if err != nil {
		t.Fatalf("maintenanceTarballChecksum failed: %v", err)
	}
	if sum != maintenanceSingleFileChecksum {
		t.Errorf("checksum = %q, want %q", sum, maintenanceSingleFileChecksum)
	}
}

func TestMaintenanceTarballChecksumMultiFile(t *testing.T) {
	tarBytes := tarFromEntries(t, map[string]string{
		"maintenance.html": maintenanceTestPage,
		"assets/style.css": maintenanceTestCSS,
	})
	sum, err := maintenanceTarballChecksum(tarBytes)
	if err != nil {
		t.Fatalf("maintenanceTarballChecksum failed: %v", err)
	}
	if sum != maintenanceMultiFileChecksum {
		t.Errorf("checksum = %q, want %q", sum, maintenanceMultiFileChecksum)
	}
}

func TestMaintenanceTarballChecksumDeterministicAndSensitive(t *testing.T) {
	a := tarFromEntries(t, map[string]string{"maintenance.html": maintenanceTestPage})
	b := tarFromEntries(t, map[string]string{"maintenance.html": maintenanceTestPage + "<!-- changed -->"})

	sumA1, err := maintenanceTarballChecksum(a)
	if err != nil {
		t.Fatalf("checksum a: %v", err)
	}
	sumA2, err := maintenanceTarballChecksum(a)
	if err != nil {
		t.Fatalf("checksum a again: %v", err)
	}
	if sumA1 != sumA2 {
		t.Errorf("checksum not deterministic: %q != %q", sumA1, sumA2)
	}
	if len(sumA1) != 64 {
		t.Errorf("checksum length = %d, want 64", len(sumA1))
	}
	sumB, err := maintenanceTarballChecksum(b)
	if err != nil {
		t.Fatalf("checksum b: %v", err)
	}
	if sumA1 == sumB {
		t.Error("checksum should change when content changes")
	}
}

func TestMaintenanceTarballChecksumMissingIndex(t *testing.T) {
	tarBytes := tarFromEntries(t, map[string]string{"index.html": maintenanceTestPage})
	if _, err := maintenanceTarballChecksum(tarBytes); err == nil {
		t.Fatal("expected error when maintenance.html is absent")
	} else if !strings.Contains(err.Error(), "maintenance.html") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestMaintenanceTarballChecksumMalformed(t *testing.T) {
	if _, err := maintenanceTarballChecksum([]byte("not a tar archive at all")); err == nil {
		t.Fatal("expected error for malformed tar bytes")
	}
}

func TestGetTasksMaintenanceCustomPageInlineParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set page
      dokku_maintenance_custom_page:
        app: test-app
        content: |
          <html><body>down</body></html>
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set page")
	if task == nil {
		t.Fatal("task 'set page' not found")
	}
	pageTask, ok := task.(*MaintenanceCustomPageTask)
	if !ok {
		t.Fatalf("task is not a MaintenanceCustomPageTask (type is %T)", task)
	}
	if pageTask.App != "test-app" {
		t.Errorf("App = %q, want %q", pageTask.App, "test-app")
	}
	if !strings.Contains(pageTask.Content, "down") {
		t.Errorf("Content missing expected HTML: %q", pageTask.Content)
	}
	if pageTask.Tarball != "" {
		t.Errorf("expected Tarball empty, got %q", pageTask.Tarball)
	}
	if pageTask.State != StatePresent {
		t.Errorf("State = %q, want %q", pageTask.State, StatePresent)
	}
}

func TestGetTasksMaintenanceCustomPageTarballParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set page
      dokku_maintenance_custom_page:
        app: test-app
        tarball: /etc/dokku/maintenance/test-app.tar
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set page")
	if task == nil {
		t.Fatal("task 'set page' not found")
	}
	pageTask, ok := task.(*MaintenanceCustomPageTask)
	if !ok {
		t.Fatalf("task is not a MaintenanceCustomPageTask (type is %T)", task)
	}
	if pageTask.Tarball != "/etc/dokku/maintenance/test-app.tar" {
		t.Errorf("Tarball = %q, want %q", pageTask.Tarball, "/etc/dokku/maintenance/test-app.tar")
	}
	if pageTask.Content != "" {
		t.Errorf("expected Content empty, got %q", pageTask.Content)
	}
}
