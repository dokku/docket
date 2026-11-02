package commands

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
)

// export_progress_test.go covers the progress export prints while it reads the
// server (#670): a line per scope by default, a line per exporter under
// --verbose, nothing under --quiet, and always on stderr so a recipe streamed
// with --output - is the only thing on stdout.

// runExportWithProgress runs export against the command fixture and returns
// what reached stderr, the Ui output, and the exit code.
func runExportWithProgress(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	dir := t.TempDir()
	c, ui := exportWithRunner(fakeExecRunner(exportCommandFixture()), dir)
	var stderr bytes.Buffer
	c.Stderr = &stderr
	c.Stdout = &bytes.Buffer{}
	code := c.Run(append([]string{"--output", filepath.Join(dir, "tasks.yml")}, args...))
	return stderr.String(), ui.OutputWriter.String(), code
}

func TestExportCommandReportsEachScope(t *testing.T) {
	t.Parallel()
	stderr, out, code := runExportWithProgress(t)
	if code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}
	want := "==> Reading global resources\n==> Reading app web (1/1)\n"
	if stderr != want {
		t.Errorf("stderr = %q, want %q", stderr, want)
	}
	if strings.Contains(out, "Reading") {
		t.Errorf("progress should not reach the Ui output:\n%s", out)
	}
}

func TestExportCommandVerboseReportsEachExporter(t *testing.T) {
	t.Parallel()
	stderr, _, code := runExportWithProgress(t, "--verbose")
	if code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}
	appLine := strings.Index(stderr, "==> Reading app web (1/1)\n")
	configLine := strings.Index(stderr, "\n    dokku_config\n")
	if appLine < 0 || configLine < appLine {
		t.Errorf("--verbose should list dokku_config under the web scope:\n%s", stderr)
	}
	if !strings.Contains(stderr, "==> Reading global resources\n    dokku_plugin\n") {
		t.Errorf("--verbose should list the global exporters under the global scope:\n%s", stderr)
	}
}

func TestExportCommandQuietReportsNothing(t *testing.T) {
	t.Parallel()
	stderr, _, code := runExportWithProgress(t, "--quiet")
	if code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}
	if stderr != "" {
		t.Errorf("--quiet should leave stderr empty, got %q", stderr)
	}
}

// TestExportCommandStreamedRecipeKeepsProgressOffStdout is why progress goes to
// its own writer: `docket export --output - | docket apply -` reads stdout.
func TestExportCommandStreamedRecipeKeepsProgressOffStdout(t *testing.T) {
	t.Parallel()
	c, ui := exportWithRunner(fakeExecRunner(exportCommandFixture()), t.TempDir())
	var stdout, stderr bytes.Buffer
	c.Stdout = &stdout
	c.Stderr = &stderr
	if code := c.Run([]string{"--output", "-"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}
	if strings.Contains(stdout.String(), "Reading") {
		t.Errorf("stdout should hold only the recipe:\n%s", stdout.String())
	}
	if !strings.HasPrefix(stdout.String(), "---") && !strings.HasPrefix(stdout.String(), "- ") {
		t.Errorf("stdout should open with the recipe:\n%s", stdout.String())
	}
	if !strings.Contains(stderr.String(), "==> Reading app web (1/1)") {
		t.Errorf("progress should reach stderr:\n%s", stderr.String())
	}
}

// TestExportCommandRejectsVerboseWithQuiet installs no fake exec runner, so
// passing proves the rejection lands before the server is read.
func TestExportCommandRejectsVerboseWithQuiet(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	c, ui := newExportCommand(dir)
	if code := c.Run([]string{"--verbose", "--quiet", "--output", filepath.Join(dir, "tasks.yml")}); code != 1 {
		t.Fatalf("exit = %d, want 1", code)
	}
	if errOut := ui.ErrorWriter.String(); !strings.Contains(errOut, "--verbose and --quiet cannot be combined") {
		t.Errorf("error should name both flags:\n%s", errOut)
	}
}
