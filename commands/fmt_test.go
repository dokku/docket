package commands

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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
	t.Parallel()
	c := &FmtCommand{}
	if c.Name() != "fmt" {
		t.Errorf("Name = %q, want %q", c.Name(), "fmt")
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestFmtCommandExamples(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, exit := captureStdout(t, func(out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = out
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
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, exit := captureStdout(t, func(out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = out
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
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, exit := captureStdout(t, func(out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = out
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
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, _ := captureStdout(t, func(out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = out
		return c.Run([]string{"--diff", "--color", "never", path})
	})
	if strings.Contains(captured, "\x1b[") {
		t.Errorf("--color never should suppress ANSI escapes:\n%q", captured)
	}
}

func TestFmtColorAlwaysProducesAnsiEvenInPipe(t *testing.T) {
	t.Parallel()
	// No save-and-restore any more: --color resolves to a value this run
	// carries, so the test cannot disturb anything else.
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("seed write: %v", err)
	}

	captured, _ := captureStdout(t, func(out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = out
		return c.Run([]string{"--diff", "--color", "always", path})
	})
	if !strings.Contains(captured, "\x1b[") {
		t.Errorf("--color always should force ANSI escapes:\n%q", captured)
	}
}

func TestFmtColorInvalidValueFails(t *testing.T) {
	t.Parallel()
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--color", "rainbow", "tasks.yml"}); exit != 1 {
		t.Errorf("invalid --color exit = %d, want 1", exit)
	}
}

func TestFmtStdinReadsAndWritesStdout(t *testing.T) {
	t.Parallel()
	captured, exit := withStdinAndStdout(t, messyTasksYAML, func(in io.Reader, out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin = in
		c.Stdout = out
		return c.Run([]string{"-"})
	})
	if exit != 0 {
		t.Errorf("exit = %d, want 0", exit)
	}
	if captured != canonicalTasksYAML {
		t.Errorf("stdin->stdout output mismatch:\nwant:\n%s\ngot:\n%s", canonicalTasksYAML, captured)
	}
}

func TestFmtStdinInvalidTasksFormatFails(t *testing.T) {
	t.Parallel()
	captured, exit := withStdinAndStdout(t, messyTasksYAML, func(in io.Reader, out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin = in
		c.Stdout = out
		return c.Run([]string{"--tasks-format", "toml", "-"})
	})
	if exit != 1 {
		t.Errorf("invalid --tasks-format exit = %d, want 1", exit)
	}
	if captured != "" {
		t.Errorf("nothing should be written on a rejected format, got:\n%s", captured)
	}
}

// TestFmtStdinTasksFormatOverridesSniff is the case sniffing alone
// cannot get right: a top-level flow sequence is valid YAML but opens
// with "[", so sniffStdinFormat calls it JSON5 and the JSON5 formatter
// rewrites it into JSON5 syntax. --tasks-format yaml keeps it YAML.
func TestFmtStdinTasksFormatOverridesSniff(t *testing.T) {
	t.Parallel()
	const flowYAML = "[{tasks: [{name: flow, dokku_app: {app: api}}]}]\n"

	sniffed, exit := withStdinAndStdout(t, flowYAML, func(in io.Reader, out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin = in
		c.Stdout = out
		return c.Run([]string{"-"})
	})
	if exit != 0 {
		t.Fatalf("sniffed exit = %d, want 0", exit)
	}
	if !strings.Contains(sniffed, "dokku_app: {") {
		t.Errorf("without an override the sniff should pick JSON5, got:\n%s", sniffed)
	}

	forced, exit := withStdinAndStdout(t, flowYAML, func(in io.Reader, out io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin = in
		c.Stdout = out
		return c.Run([]string{"--tasks-format", "yaml", "-"})
	})
	if exit != 0 {
		t.Fatalf("forced exit = %d, want 0", exit)
	}
	if forced == sniffed {
		t.Errorf("--tasks-format yaml should not produce the JSON5 layout, got:\n%s", forced)
	}
}

func TestFmtTasksFormatOverridesFileExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	// A .yml extension would normally select the YAML formatter; the
	// override sends this JSON5 body to the JSON5 formatter instead.
	path := filepath.Join(dir, "recipe.yml")
	if err := os.WriteFile(path, []byte(messyTasksJSON5), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--tasks-format", "json5", path}); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != canonicalTasksJSON5 {
		t.Errorf("output mismatch:\nwant:\n%s\ngot:\n%s", canonicalTasksJSON5, got)
	}
}

// TestFmtRejectsStdinMixedWithPaths: stdin is one stream, so it cannot
// be combined with named files. Without the guard the "-" fell through
// to expandPaths, matched no glob, and died on os.ReadFile("-").
func TestFmtRejectsStdinMixedWithPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	c := newTestFmtCommand()
	if exit := c.Run([]string{"-", path}); exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	errOut := c.Ui.(*cli.MockUi).ErrorWriter.String()
	if !strings.Contains(errOut, "cannot mix - with other paths") {
		t.Errorf("error should explain the mix, got: %s", errOut)
	}
}

// TestFmtWarnsOnAmbiguousDefaultProbe: `docket fmt` with no argument
// formats exactly one file, chosen by the same silent probe #420 is
// about. A directory holding both candidates gets told which one was
// picked.
func TestFmtWarnsOnAmbiguousDefaultProbe(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(canonicalTasksJSON5), 0o644); err != nil {
		t.Fatalf("write tasks.json: %v", err)
	}

	c := newTestFmtCommand(dir)
	if exit := c.Run([]string{"--check"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	warn := c.Ui.(*cli.MockUi).ErrorWriter.String()
	for _, want := range []string{"tasks.yml", "tasks.json", "both exist"} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning missing %q:\n%s", want, warn)
		}
	}
}

// TestFmtDoesNotWarnForNamedPaths: named arguments select their own
// files, so there is no probe to be ambiguous about.
func TestFmtDoesNotWarnForNamedPaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(canonicalTasksJSON5), 0o644); err != nil {
		t.Fatalf("write tasks.json: %v", err)
	}

	c := newTestFmtCommand(dir)
	if exit := c.Run([]string{"--check", "tasks.json"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	if warn := c.Ui.(*cli.MockUi).ErrorWriter.String(); warn != "" {
		t.Errorf("a named path is unambiguous; should not warn:\n%s", warn)
	}
}

func TestFmtGlobExpandsMatches(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
func newTestFmtCommand(baseDir ...string) *FmtCommand {
	c := &FmtCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	if len(baseDir) > 0 {
		c.BaseDir = baseDir[0]
	}
	return c
}

// captureStdout gives fn a buffer to write to and returns what it wrote plus
// the exit code. The commands stream a recipe, diff or catalog straight to
// their Stdout rather than through the Ui, which is wrapped in a log formatter,
// so fn hands the buffer to whichever command it builds.
//
// It used to swap the os.Stdout package variable for a pipe - process state no
// two tests can hold at once.
func captureStdout(t *testing.T, fn func(out io.Writer) int) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	exit := fn(&buf)
	return buf.String(), exit
}

// withStdinAndStdout gives fn a reader holding input and a buffer to write to,
// and returns what it wrote plus the exit code. fn hands both to the command it
// builds, so neither os.Stdin nor os.Stdout is touched.
func withStdinAndStdout(t *testing.T, input string, fn func(in io.Reader, out io.Writer) int) (string, int) {
	t.Helper()
	var buf bytes.Buffer
	exit := fn(strings.NewReader(input), &buf)
	return buf.String(), exit
}
