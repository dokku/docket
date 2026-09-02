package commands

import (
	"io"
	"os"
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// withStdinArgs drives a command the way the real CLI does: os.Args
// carries the full argv (FlagSet reads it before pflag parses, to
// preregister the recipe's inputs as flags) and os.Stdin carries the
// recipe. Returns the MockUi so callers can assert on output.
//
// resetStdinRecipe bookends the swap because readStdinRecipe memoizes
// its own pipe and argv, so two of these can no longer serve each other the
// wrong bytes through a process-wide memo.
func withStdinArgs(t *testing.T, recipe string, args []string, fn func(stdin io.Reader, argv []string)) {
	t.Helper()

	r, w, err := os.Pipe()
	if err != nil {
		t.Fatalf("stdin pipe: %v", err)
	}
	t.Cleanup(func() { r.Close() })

	go func() {
		_, _ = w.WriteString(recipe)
		w.Close()
	}()

	fn(r, append([]string{"docket", "validate"}, args...))
}

// runValidateOverStdin pipes recipe into `docket validate <args>` and
// returns the exit code plus the combined MockUi output.
func runValidateOverStdin(t *testing.T, recipe string, args []string) (int, string) {
	t.Helper()

	var exit int
	var out string
	withStdinArgs(t, recipe, args, func(stdin io.Reader, argv []string) {
		ui := cli.NewMockUi()
		c := &ValidateCommand{Meta: command.Meta{Ui: ui}, Argv: argv, Stdin: stdin}
		exit = c.Run(args)
		out = ui.OutputWriter.String() + ui.ErrorWriter.String()
	})
	return exit, out
}

const stdinYAMLRecipe = `---
- tasks:
    - name: create app
      dokku_app:
        app: api
`

// A top-level flow sequence: valid YAML, but it opens with "[" so the
// content sniff calls it JSON5. The --tasks-format escape hatch exists
// for exactly this.
const stdinFlowYAMLRecipe = `[{tasks: [{name: flow, dokku_app: {app: api}}]}]`

const stdinJSON5Recipe = `[
  // a comment, so this can only be JSON5
  { tasks: [{ name: "create app", dokku_app: { app: "api" } }] },
]
`

func TestValidateReadsYAMLFromStdin(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{{"-"}, {"--tasks", "-"}} {
		t.Run(strings.Join(args, " "), func(t *testing.T) {
			exit, out := runValidateOverStdin(t, stdinYAMLRecipe, args)
			if exit != 0 {
				t.Fatalf("exit = %d, want 0; output:\n%s", exit, out)
			}
			if !strings.Contains(out, "is valid") {
				t.Errorf("output should report success, got:\n%s", out)
			}
			// stdin has no path, so it must not print a bare "-".
			if !strings.Contains(out, "<stdin>") {
				t.Errorf("output should name the source as <stdin>, got:\n%s", out)
			}
		})
	}
}

func TestValidateSniffsJSON5FromStdin(t *testing.T) {
	t.Parallel()
	exit, out := runValidateOverStdin(t, stdinJSON5Recipe, []string{"-"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", exit, out)
	}
	if !strings.Contains(out, "is valid") {
		t.Errorf("output should report success, got:\n%s", out)
	}
}

// TestValidateStdinFormatOverridesSniff is the case the sniff alone
// cannot get right: a flow-style YAML recipe opens with "[" and would
// be handed to the JSON5 parser without an explicit --tasks-format.
func TestValidateStdinFormatOverridesSniff(t *testing.T) {
	t.Parallel()
	exit, out := runValidateOverStdin(t, stdinFlowYAMLRecipe, []string{"--tasks-format", "yaml", "-"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", exit, out)
	}
	if !strings.Contains(out, "is valid") {
		t.Errorf("output should report success, got:\n%s", out)
	}
}

func TestValidateRejectsEmptyStdin(t *testing.T) {
	t.Parallel()
	exit, out := runValidateOverStdin(t, "", []string{"-"})
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", exit, out)
	}
	// "no recipe found in tasks file" is what the parser would say, and
	// it is misleading when there was no file.
	if !strings.Contains(out, "stdin is empty") {
		t.Errorf("output should say stdin was empty, got:\n%s", out)
	}
}

func TestValidateRejectsUnknownTasksFormat(t *testing.T) {
	t.Parallel()
	exit, out := runValidateOverStdin(t, stdinYAMLRecipe, []string{"--tasks-format", "toml", "-"})
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", exit, out)
	}
	if !strings.Contains(out, "yaml, json5") {
		t.Errorf("output should name the valid formats, got:\n%s", out)
	}
}

func TestValidateRejectsStdinTwice(t *testing.T) {
	t.Parallel()
	exit, out := runValidateOverStdin(t, stdinYAMLRecipe, []string{"--tasks", "-", "-"})
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; output:\n%s", exit, out)
	}
	if !strings.Contains(out, "both --tasks and a positional") {
		t.Errorf("output should reject the duplicate source, got:\n%s", out)
	}
}

// TestValidateStdinRegistersRecipeInputs is the load-bearing one: it
// proves FlagSet's pre-parse read and Run's read agree about stdin. The
// input flag only exists because FlagSet parsed the piped recipe, and
// the strict check only passes because Run saw the same bytes.
func TestValidateStdinRegistersRecipeInputs(t *testing.T) {
	t.Parallel()
	recipe := `---
- inputs:
    - name: app
      required: true
  tasks:
    - name: create {{ .app }}
      dokku_app:
        app: "{{ .app }}"
`
	args := []string{"--strict", "--app", "piped-in", "-"}
	exit, out := runValidateOverStdin(t, recipe, args)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", exit, out)
	}
	if !strings.Contains(out, "is valid") {
		t.Errorf("output should report success, got:\n%s", out)
	}
}

// TestPreloadRecipeForFlagsSkipsMissingFile: a recipe that cannot be
// read must not stop the flag set being built - Run re-resolves and
// reports the real error there.
func TestPreloadRecipeForFlagsSkipsMissingFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	data, format, source := preloadRecipeForFlags(dir, []string{"docket", "validate"}, false, newStdinRecipeSource(nil))
	if data != nil {
		t.Errorf("data = %q, want nil when no recipe exists", data)
	}
	if format != "" {
		t.Errorf("format = %q, want empty when no recipe exists", format)
	}
	if source != defaultTaskFileCandidates[0] {
		t.Errorf("source = %q, want the probed default %q", source, defaultTaskFileCandidates[0])
	}
}
