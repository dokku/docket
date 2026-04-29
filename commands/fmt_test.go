package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/fatih/color"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

const messyTasksYAML = `---
- tasks:
        - dokku_app:
              app: web
          name: configure web
`

const canonicalTasksYAML = `---
- tasks:
    - name: configure web
      dokku_app:
        app: web
`

func TestFmtCommandMetadata(t *testing.T) {
	c := &FmtCommand{}
	if c.Name() != "fmt" {
		t.Errorf("Name = %q, want %q", c.Name(), "fmt")
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestFmtCommandExamples(t *testing.T) {
	c := &FmtCommand{}
	examples := c.Examples()
	if len(examples) == 0 {
		t.Fatal("expected at least one example")
	}
	for label, example := range examples {
		if example == "" {
			t.Errorf("example %q is empty", label)
		}
	}
}

func TestFmtCommandHelpDoesNotPanic(t *testing.T) {
	c := &FmtCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked: %v", r)
		}
	}()
	_ = c.FlagSet()
}

// JSON5 fixtures parallel to the YAML ones. Used to assert fmt picks
// the right formatter based on extension and that the canonical JSON5
// shape matches what FormatJSON5 produces directly.
const messyTasksJSON5 = `[
  // a comment
  {
    tasks: [
      {
        dokku_app: { app: "web" },
        name: "configure web",
      },
    ],
  },
]
`

const canonicalTasksJSON5 = `[
  // a comment
  {
    tasks: [
      {
        name: "configure web",
        dokku_app: {
          app: "web",
        },
      },
    ],
  },
]
`

func TestFmtRewritesJSON5InPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(messyTasksJSON5), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{path}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != canonicalTasksJSON5 {
		t.Errorf("file not canonicalised:\nwant:\n%s\ngot:\n%s", canonicalTasksJSON5, got)
	}
}

func TestFmtJSON5IdempotentOnCanonical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(path, []byte(canonicalTasksJSON5), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--check", path}); exit != 0 {
		t.Errorf("--check on canonical JSON5 exit = %d, want 0", exit)
	}
}

func TestFmtRewritesNonCanonicalInPlace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	c := newTestFmtCommand()
	if exit := c.Run([]string{path}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != canonicalTasksYAML {
		t.Errorf("file not canonicalised:\n%s", got)
	}
}

func TestFmtNoOpPreservesMtime(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	// Backdate so a no-op write would produce a clearly different mtime.
	older := time.Now().Add(-1 * time.Hour)
	if err := os.Chtimes(path, older, older); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	before, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat before: %v", err)
	}

	c := newTestFmtCommand()
	if exit := c.Run([]string{path}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}

	after, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat after: %v", err)
	}
	if !before.ModTime().Equal(after.ModTime()) {
		t.Errorf("mtime should be preserved on no-op format; before=%v after=%v", before.ModTime(), after.ModTime())
	}
}

func TestFmtCheckExitsNonZeroOnNonCanonical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	c := newTestFmtCommand()
	if exit := c.Run([]string{"--check", path}); exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != messyTasksYAML {
		t.Error("--check must not write")
	}
}

func TestFmtCheckExitsZeroOnCanonical(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--check", path}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
}

func TestFmtCheckAloneEmitsNoDiff(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, exit := captureStdout(t, func() int {
		c := newTestFmtCommand()
		return c.Run([]string{"--check", "--color", "never", path})
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if strings.Contains(captured, "@@") || strings.Contains(captured, "+++") {
		t.Errorf("--check alone should not emit a unified diff body:\n%s", captured)
	}
}

func TestFmtDiffPrintsDiffAndDoesNotWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, exit := captureStdout(t, func() int {
		c := newTestFmtCommand()
		return c.Run([]string{"--diff", "--color", "never", path})
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0 for --diff alone on mismatch", exit)
	}
	for _, want := range []string{"--- " + path, "+++ " + path, "@@", "-              app: web", "+        app: web"} {
		if !strings.Contains(captured, want) {
			t.Errorf("diff output missing %q:\n%s", want, captured)
		}
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != messyTasksYAML {
		t.Error("--diff must not write")
	}
}

func TestFmtCheckDiffComposes(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, exit := captureStdout(t, func() int {
		c := newTestFmtCommand()
		return c.Run([]string{"--check", "--diff", "--color", "never", path})
	})
	if exit != 1 {
		t.Errorf("exit = %d, want 1 for --check --diff on mismatch", exit)
	}
	if !strings.Contains(captured, "@@") {
		t.Errorf("--check --diff should emit hunk headers:\n%s", captured)
	}

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != messyTasksYAML {
		t.Error("--check --diff must not write")
	}
}

func TestFmtColorNeverProducesPlainOutput(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, _ := captureStdout(t, func() int {
		c := newTestFmtCommand()
		return c.Run([]string{"--diff", "--color", "never", path})
	})
	if strings.Contains(captured, "\x1b[") {
		t.Errorf("--color never should suppress ANSI escapes:\n%q", captured)
	}
}

func TestFmtColorAlwaysProducesAnsiEvenInPipe(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, _ := captureStdout(t, func() int {
		c := newTestFmtCommand()
		return c.Run([]string{"--diff", "--color", "always", path})
	})
	if !strings.Contains(captured, "\x1b[") {
		t.Errorf("--color always should force ANSI escapes:\n%q", captured)
	}
	// Restore default so other tests are not affected.
	color.NoColor = true
}

func TestFmtColorInvalidValueFails(t *testing.T) {
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--color", "rainbow", "tasks.yml"}); exit != 1 {
		t.Errorf("invalid --color exit = %d, want 1", exit)
	}
}

func TestFmtStdinReadsAndWritesStdout(t *testing.T) {
	captured, exit := withStdinAndStdout(t, messyTasksYAML, func() int {
		c := newTestFmtCommand()
		return c.Run([]string{"-"})
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if captured != canonicalTasksYAML {
		t.Errorf("stdin->stdout output mismatch:\nwant:\n%s\ngot:\n%s", canonicalTasksYAML, captured)
	}
}

func TestFmtGlobExpandsMatches(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"a.yml", "b.yml", "c.yaml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(messyTasksYAML), 0o644); err != nil {
			t.Fatalf("seed write: %v", err)
		}
	}

	c := newTestFmtCommand()
	if exit := c.Run([]string{filepath.Join(dir, "*.yml")}); exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	for _, name := range []string{"a.yml", "b.yml"} {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		if string(got) != canonicalTasksYAML {
			t.Errorf("%s not canonicalised:\n%s", name, got)
		}
	}
	got, err := os.ReadFile(filepath.Join(dir, "c.yaml"))
	if err != nil {
		t.Fatalf("read c.yaml: %v", err)
	}
	if string(got) != messyTasksYAML {
		t.Error("*.yml glob must not match c.yaml")
	}
}

func TestFmtMultiFilePerFileErrorsDoNotAbort(t *testing.T) {
	dir := t.TempDir()
	good := filepath.Join(dir, "good.yml")
	bad := filepath.Join(dir, "missing.yml")
	if err := os.WriteFile(good, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	c := newTestFmtCommand()
	exit := c.Run([]string{bad, good})
	if exit != 1 {
		t.Errorf("exit = %d, want 1 (worst-of)", exit)
	}
	got, err := os.ReadFile(good)
	if err != nil {
		t.Fatalf("read good: %v", err)
	}
	if string(got) != canonicalTasksYAML {
		t.Errorf("good.yml should still be formatted despite missing.yml error:\n%s", got)
	}
}

func TestFmtParseErrorReturnsExit1(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte("- a: [b\n"), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}
	c := newTestFmtCommand()
	exit := c.Run([]string{path})
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
}

// newTestFmtCommand wires up a Meta backed by cli.MockUi so c.Ui.* calls
// don't nil-panic during Run. Tests assert via the file system or
// captured stdout; the MockUi error/output buffers are inspected only
// when needed.
func newTestFmtCommand() *FmtCommand {
	c := &FmtCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	return c
}

// captureStdout swaps os.Stdout for a pipe, runs fn, and returns the
// captured output. Used to assert on diff output.
func captureStdout(t *testing.T, fn func() int) (string, int) {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("pipe: %v", err)
	}
	origStdout := os.Stdout
	os.Stdout = w
	t.Cleanup(func() { os.Stdout = origStdout })

	exitCh := make(chan int, 1)
	go func() {
		exit := fn()
		w.Close()
		exitCh <- exit
	}()

	var buf bytes.Buffer
	if _, err := io.Copy(&buf, r); err != nil {
		t.Fatalf("read pipe: %v", err)
	}
	return buf.String(), <-exitCh
}

// withStdinAndStdout pipes the given input to os.Stdin and captures
// os.Stdout while fn runs. Returns the captured stdout.
func withStdinAndStdout(t *testing.T, input string, fn func() int) (string, int) {
	t.Helper()

	stdinR, stdinW, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	origStdin := os.Stdin
	os.Stdin = stdinR
	t.Cleanup(func() { os.Stdin = origStdin })

	go func() {
		_, _ = stdinW.WriteString(input)
		stdinW.Close()
	}()

	return captureStdout(t, fn)
}
