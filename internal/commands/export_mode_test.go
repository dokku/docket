package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

)

// assertPerm fails unless path has exactly the given permission bits.
func assertPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("%s mode = %04o, want %04o", path, got, want)
	}
}

// umaskPerm returns the mode a 0o644 file actually lands at in dir, which
// depends on the umask of whoever is running the tests. The recipe is written
// with a plain os.WriteFile, so this is what it should match - asserting a
// literal 0644 would fail under a umask of 077.
func umaskPerm(t *testing.T, dir string) os.FileMode {
	t.Helper()
	control := filepath.Join(dir, ".umask-control")
	if err := os.WriteFile(control, nil, 0o644); err != nil {
		t.Fatalf("write control file: %v", err)
	}
	info, err := os.Stat(control)
	if err != nil {
		t.Fatalf("stat control file: %v", err)
	}
	return info.Mode().Perm()
}

// TestExportCommandVarsFileIsPrivate is the point of #489: the vars-file holds
// every config value in the clear, so it is written 0600 while the recipe -
// which carries interpolations rather than values - stays public.
func TestExportCommandVarsFileIsPrivate(t *testing.T) {
	t.Parallel()
	runner := fakeExecRunner(exportCommandFixture())

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, _ := exportWithRunner(runner)
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}

	assertPerm(t, vars, 0o600)
	assertPerm(t, recipe, umaskPerm(t, dir))
}

// TestExportCommandVarsOutputIsPrivate pins that the mode follows the file,
// not the derived default path.
func TestExportCommandVarsOutputIsPrivate(t *testing.T) {
	t.Parallel()
	runner := fakeExecRunner(exportCommandFixture())

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "secrets.yml")

	c, _ := exportWithRunner(runner)
	if code := c.Run([]string{"--output", recipe, "--vars-output", vars}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}

	assertPerm(t, vars, 0o600)
}

// TestExportCommandRedactedVarsFileIsPrivate: a redacted vars-file holds
// placeholders, but it is the file the operator then fills in with the real
// secrets, so splitting the mode by flag would hand them a 0644 file to type
// credentials into.
func TestExportCommandRedactedVarsFileIsPrivate(t *testing.T) {
	t.Parallel()
	runner := fakeExecRunner(exportCommandFixture())

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, _ := exportWithRunner(runner)
	if code := c.Run([]string{"--output", recipe, "--redact"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}

	assertPerm(t, vars, 0o600)
}

// TestExportCommandVarsFileModeResetOnOverwrite covers the half of #489 that
// os.WriteFile cannot do on its own: O_CREATE's mode applies only to a file it
// creates, so a vars-file an older docket left at 0644 would stay readable
// forever.
func TestExportCommandVarsFileModeResetOnOverwrite(t *testing.T) {
	t.Parallel()
	runner := fakeExecRunner(exportCommandFixture())

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")
	if err := os.WriteFile(vars, []byte("OLD\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(vars, 0o644); err != nil {
		t.Fatal(err)
	}

	c, _ := exportWithRunner(runner)
	if code := c.Run([]string{"--output", recipe, "--overwrite"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}

	assertPerm(t, vars, 0o600)
	got, err := os.ReadFile(vars)
	if err != nil {
		t.Fatalf("vars-file not written: %v", err)
	}
	if strings.Contains(string(got), "OLD") {
		t.Errorf("overwrite should replace the vars-file, got %q", got)
	}
	if !strings.Contains(string(got), "abc123") {
		t.Errorf("vars-file should hold the real value:\n%s", got)
	}
}

// TestExportCommandRecipeModeLeftAlone: the recipe is not chmod'd, so an
// operator who locked one down keeps it that way. Only the vars-file's mode is
// docket's business, because only the vars-file's contents ask for one.
func TestExportCommandRecipeModeLeftAlone(t *testing.T) {
	t.Parallel()
	runner := fakeExecRunner(exportCommandFixture())

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(recipe, []byte("OLD\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(recipe, 0o600); err != nil {
		t.Fatal(err)
	}

	c, _ := exportWithRunner(runner)
	if code := c.Run([]string{"--output", recipe, "--overwrite"}); code != 0 {
		t.Fatalf("Run exit = %d, want 0", code)
	}

	assertPerm(t, recipe, 0o600)
}

// TestExportCommandWarnsWhenTheModeCannotBeSet covers the fallback for a
// filesystem that cannot hold permission bits - vfat, some network mounts.
// The export still finishes, because the pair is what was asked for, but the
// operator has to be told this half is in the clear.
func TestExportCommandWarnsWhenTheModeCannotBeSet(t *testing.T) {
	t.Parallel()
	runner := fakeExecRunner(exportCommandFixture())

	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	vars := filepath.Join(dir, "tasks.vars.yml")

	c, ui := exportWithRunner(runner)
	c.ChmodVarsFile = func(*os.File, os.FileMode) error { return errors.New("operation not supported") }
	if code := c.Run([]string{"--output", recipe}); code != 0 {
		t.Fatalf("a mode that cannot be set must not fail the export, got exit %d", code)
	}

	stderr := ui.ErrorWriter.String()
	for _, want := range []string{"could not set mode 0600", vars, "operation not supported"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q:\n%s", want, stderr)
		}
	}

	// The values are still written, which is the point of not failing.
	varsBytes, err := os.ReadFile(vars)
	if err != nil {
		t.Fatalf("vars-file not written: %v", err)
	}
	if !strings.Contains(string(varsBytes), "abc123") {
		t.Errorf("vars-file should hold the real value:\n%s", varsBytes)
	}
}
