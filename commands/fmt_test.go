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
// with "[", so the JSON5 codec's Sniff claims it and that formatter
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

// --- conversion (#418) ---------------------------------------------------

// commentedTasksYAML and its JSON5 twin carry a comment at each anchor
// point, so a conversion that drops or duplicates one is visible. The two
// are exact images of each other: each converts to the other byte for
// byte, which is what makes them usable as both directions' expectation.
//
// The file-level comment sits inside the [ ] rather than above it because
// that is where it actually belongs - yaml.v3 attaches a comment above the
// first list item to that item, not to the list - and the JSON5 rendering
// keeps it attached to the same play.
const commentedTasksYAML = `# top of file
- name: web # the public app
  tasks:
    # scale it up first
    - name: scale
      dokku_ps_scale:
        app: web
        web: 2
`

const commentedTasksJSON5 = `[
  // top of file
  {
    name: "web", // the public app
    tasks: [
      // scale it up first
      {
        name: "scale",
        dokku_ps_scale: {
          app: "web",
          web: 2,
        },
      },
    ],
  },
]
`

func TestFmtStdinConvertsYAMLToJSON5(t *testing.T) {
	t.Parallel()
	out, exit := withStdinAndStdout(t, commentedTasksYAML, func(in io.Reader, w io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin, c.Stdout = in, w
		return c.Run([]string{"--format", "json5", "-"})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if out != commentedTasksJSON5 {
		t.Errorf("output mismatch:\nwant:\n%s\ngot:\n%s", commentedTasksJSON5, out)
	}
}

func TestFmtStdinConvertsJSON5ToYAML(t *testing.T) {
	t.Parallel()
	out, exit := withStdinAndStdout(t, commentedTasksJSON5, func(in io.Reader, w io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin, c.Stdout = in, w
		return c.Run([]string{"--format", "yaml", "-"})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	// The `---` marker is not restored: it is a property of the bytes that
	// were read, and these came from JSON5. Everything else round-trips.
	if out != commentedTasksYAML {
		t.Errorf("output mismatch:\nwant:\n%s\ngot:\n%s", commentedTasksYAML, out)
	}
}

// TestFmtStdinFormatComposesWithTasksFormat covers the explicit both-sides
// form: --tasks-format states what was read, --format what to write.
func TestFmtStdinFormatComposesWithTasksFormat(t *testing.T) {
	t.Parallel()
	// Flow style opens with [, so the sniff would call it JSON5 without
	// --tasks-format saying otherwise.
	const flowYAML = "[{name: web, tasks: []}]\n"
	out, exit := withStdinAndStdout(t, flowYAML, func(in io.Reader, w io.Writer) int {
		c := newTestFmtCommand()
		c.Stdin, c.Stdout = in, w
		return c.Run([]string{"--tasks-format", "yaml", "--format", "json5", "-"})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if !strings.Contains(out, `name: "web"`) {
		t.Errorf("output is not JSON5:\n%s", out)
	}
}

func TestFmtConvertsInPlaceAndWarnsAboutTheExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(commentedTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--format", "json5", path}); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != commentedTasksJSON5 {
		t.Errorf("file mismatch:\nwant:\n%s\ngot:\n%s", commentedTasksJSON5, got)
	}
	stderr := c.Ui.(*cli.MockUi).ErrorWriter.String()
	if !strings.Contains(stderr, "--tasks-format json5") {
		t.Errorf("expected a warning that the extension now lies, got:\n%s", stderr)
	}
}

// TestFmtNonConvertingWriteStaysSilent is the guard on the warning's
// condition. A --tasks-format that disagrees with the extension has always
// been legal and quiet; --format must not make it start warning.
func TestFmtNonConvertingWriteStaysSilent(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "recipe.yml")
	if err := os.WriteFile(path, []byte(messyTasksJSON5), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--tasks-format", "json5", path}); exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if stderr := c.Ui.(*cli.MockUi).ErrorWriter.String(); strings.Contains(stderr, "does not match") {
		t.Errorf("a non-converting write warned:\n%s", stderr)
	}
}

func TestFmtOutputWritesNewFileAndLeavesSource(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "tasks.yml")
	target := filepath.Join(dir, "tasks.json5")
	if err := os.WriteFile(source, []byte(commentedTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--format", "json5", "--output", target, source}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != commentedTasksJSON5 {
		t.Errorf("target mismatch:\nwant:\n%s\ngot:\n%s", commentedTasksJSON5, got)
	}
	src, err := os.ReadFile(source)
	if err != nil {
		t.Fatalf("read source: %v", err)
	}
	if string(src) != commentedTasksYAML {
		t.Errorf("source was modified:\n%s", src)
	}
}

// TestFmtOutputInfersFormatFromExtension covers the resolution step below
// --format: the target's own extension says what to write.
func TestFmtOutputInfersFormatFromExtension(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "tasks.yml")
	target := filepath.Join(dir, "tasks.json5")
	if err := os.WriteFile(source, []byte(commentedTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--output", target, source}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != commentedTasksJSON5 {
		t.Errorf("target mismatch:\nwant:\n%s\ngot:\n%s", commentedTasksJSON5, got)
	}
}

// TestFmtOutputStdoutKeepsTheInputFormat pins why resolveRecipeOutput could
// not be reused: its stdout branch answers "yaml", which would convert a
// JSON5 recipe the user only asked to print.
func TestFmtOutputStdoutKeepsTheInputFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "tasks.json")
	if err := os.WriteFile(source, []byte(messyTasksJSON5), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, exit := captureStdout(t, func(w io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = w
		return c.Run([]string{"--output", "-", source})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if out != canonicalTasksJSON5 {
		t.Errorf("streamed output mismatch:\nwant:\n%s\ngot:\n%s", canonicalTasksJSON5, out)
	}
}

func TestFmtOutputRefusesToOverwriteWithoutForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	source := filepath.Join(dir, "tasks.yml")
	target := filepath.Join(dir, "tasks.json5")
	if err := os.WriteFile(source, []byte(commentedTasksYAML), 0o644); err != nil {
		t.Fatalf("write source: %v", err)
	}
	if err := os.WriteFile(target, []byte("// existing\n[]\n"), 0o644); err != nil {
		t.Fatalf("write target: %v", err)
	}

	c := newTestFmtCommand()
	if exit := c.Run([]string{"--format", "json5", "--output", target, source}); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(c.Ui.(*cli.MockUi).ErrorWriter.String(), "pass --force to overwrite") {
		t.Errorf("expected the --force hint, got:\n%s", c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	kept, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(kept) != "// existing\n[]\n" {
		t.Errorf("target was overwritten anyway:\n%s", kept)
	}

	forced := newTestFmtCommand()
	if exit := forced.Run([]string{"--format", "json5", "--output", target, "--force", source}); exit != 0 {
		t.Fatalf("forced exit = %d, want 0: %s", exit, forced.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	got, err := os.ReadFile(target)
	if err != nil {
		t.Fatalf("read target: %v", err)
	}
	if string(got) != commentedTasksJSON5 {
		t.Errorf("target mismatch after --force:\n%s", got)
	}
}

// TestFmtOutputEqualToSourceNeedsNoForce keeps the guard from firing on a
// write that is in-place by another spelling.
func TestFmtOutputEqualToSourceNeedsNoForce(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--output", path, path}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != canonicalTasksYAML {
		t.Errorf("file mismatch:\n%s", got)
	}
}

func TestFmtRejectedFlagCombinations(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		args []string
		want string
	}{
		{"force without output", []string{"--force"}, "--force only applies to --output"},
		{"output with check", []string{"--output", "x.json5", "--check"}, "--output cannot be used with --check"},
		{"output with diff", []string{"--output", "x.json5", "--diff"}, "--output cannot be used with --diff"},
		{"force with stdout output", []string{"--output", "-", "--force"}, "--force cannot be used with --output -"},
		{"invalid format", []string{"--format", "toml"}, `invalid --format "toml"`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte(messyTasksYAML), 0o644); err != nil {
				t.Fatalf("write: %v", err)
			}
			c := newTestFmtCommand(dir)
			if exit := c.Run(tc.args); exit != 1 {
				t.Fatalf("exit = %d, want 1", exit)
			}
			if stderr := c.Ui.(*cli.MockUi).ErrorWriter.String(); !strings.Contains(stderr, tc.want) {
				t.Errorf("stderr = %q, want it to mention %q", stderr, tc.want)
			}
		})
	}
}

func TestFmtOutputRejectsMultiplePaths(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for _, name := range []string{"a.yml", "b.yml"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(messyTasksYAML), 0o644); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	c := newTestFmtCommand(dir)
	if exit := c.Run([]string{"--output", "out.json5", "a.yml", "b.yml"}); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	if !strings.Contains(c.Ui.(*cli.MockUi).ErrorWriter.String(), "--output takes a single recipe") {
		t.Errorf("stderr = %q", c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
}

// TestFmtCheckRejectsAConvertingFormat covers the rule chosen for #418:
// --check asks whether a recipe is already canonical, which a conversion
// cannot answer, so it is refused rather than always reporting a failure.
func TestFmtCheckRejectsAConvertingFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--check", "--format", "json5", path}); exit != 1 {
		t.Fatalf("exit = %d, want 1", exit)
	}
	stderr := c.Ui.(*cli.MockUi).ErrorWriter.String()
	if !strings.Contains(stderr, "a conversion is never a no-op") {
		t.Errorf("stderr = %q", stderr)
	}
	if !strings.Contains(stderr, "--tasks-format json5") {
		t.Errorf("expected the --tasks-format hint, got %q", stderr)
	}
}

// TestFmtCheckAcceptsANonConvertingFormat is the other half: naming the
// format a recipe is already in leaves --check doing exactly what it did.
func TestFmtCheckAcceptsANonConvertingFormat(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(canonicalTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand()
	if exit := c.Run([]string{"--check", "--format", "yaml", path}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
}

// TestFmtDiffOnAConversionWritesNothing keeps --diff useful as a preview of
// what a conversion would do.
func TestFmtDiffOnAConversionWritesNothing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(commentedTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	out, exit := captureStdout(t, func(w io.Writer) int {
		c := newTestFmtCommand()
		c.Stdout = w
		return c.Run([]string{"--diff", "--format", "json5", path})
	})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if !strings.Contains(out, "@@") || !strings.Contains(out, `+    name: "web",`) {
		t.Errorf("diff does not show the conversion:\n%s", out)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != commentedTasksYAML {
		t.Errorf("--diff wrote to the file:\n%s", got)
	}
}

// TestFmtOutputSpelledDifferentlyIsStillInPlace covers samePath: an
// --output that names the source by another spelling is the in-place write
// it looks like, not an overwrite of a third file.
func TestFmtOutputSpelledDifferentlyIsStillInPlace(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte(messyTasksYAML), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	c := newTestFmtCommand(dir)
	if exit := c.Run([]string{"--output", "./tasks.yml", "tasks.yml"}); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, c.Ui.(*cli.MockUi).ErrorWriter.String())
	}
	if out := c.Ui.(*cli.MockUi).OutputWriter.String(); !strings.Contains(out, "Formatted") {
		t.Errorf("output = %q, want it reported as an in-place format", out)
	}
	got, err := os.ReadFile(filepath.Join(dir, "tasks.yml"))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != canonicalTasksYAML {
		t.Errorf("file mismatch:\n%s", got)
	}
}
