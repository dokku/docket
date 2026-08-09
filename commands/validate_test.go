package commands

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

func TestValidateCommandMetadata(t *testing.T) {
	c := &ValidateCommand{}
	if c.Name() != "validate" {
		t.Errorf("Name = %q, want \"validate\"", c.Name())
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

func TestValidateCommandExamples(t *testing.T) {
	c := &ValidateCommand{}
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

func TestValidateCommandHelpDoesNotPanic(t *testing.T) {
	c := &ValidateCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked without tasks.yml on disk: %v", r)
		}
	}()
	_ = c.FlagSet()
}

// TestFormatProblemHumanOutput exercises the human formatter end-to-end so
// the issue's example output (line N column M, "did you mean" hint) keeps
// rendering as documented.
func TestFormatProblemHumanOutput(t *testing.T) {
	tests := []struct {
		name string
		p    tasks.Problem
		want []string
	}{
		{
			name: "task with line/column",
			p: tasks.Problem{
				Task:    "task #2",
				Line:    8,
				Column:  7,
				Message: "unknown task type \"dokku_appp\"",
				Hint:    "did you mean \"dokku_app\"?",
			},
			want: []string{"task #2", "line 8:7", "unknown task type", "did you mean"},
		},
		{
			name: "play-level problem with no task",
			p: tasks.Problem{
				Line:    3,
				Column:  7,
				Message: "input \"app\" is required",
			},
			want: []string{"line 3:7", "is required"},
		},
		{
			name: "no position info",
			p: tasks.Problem{
				Code:    "yaml_parse",
				Message: "broken",
			},
			want: []string{"broken"},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatProblem(tt.p)
			for _, fragment := range tt.want {
				if !strings.Contains(got, fragment) {
					t.Errorf("formatProblem output missing %q\nfull: %q", fragment, got)
				}
			}
		})
	}
}

// TestValidateJSONEventShape runs `validate --json` over recipes that
// trip a spread of problem categories and holds every emitted line to
// docs/schemas/validate-v1.schema.json. That schema is what an
// ansible-dokku-style wrapper parses to turn a bad module argument into
// an Ansible failure, so a renamed field or an unlisted `code` must fail
// here rather than in the wrapper.
func TestValidateJSONEventShape(t *testing.T) {
	recipes := map[string]struct {
		recipe string
		codes  []string
	}{
		"unknown task type": {
			recipe: `---
- tasks:
    - name: typo
      dokku_appp:
        app: api
`,
			codes: []string{"unknown_task_type"},
		},
		"missing required field": {
			recipe: `---
- tasks:
    - name: no app
      dokku_config:
        restart: true
`,
			codes: []string{"missing_required_field"},
		},
		"conditional input rule": {
			recipe: `---
- tasks:
    - name: cert without material
      dokku_certs:
        app: api
`,
			codes: []string{"invalid_task_input"},
		},
		"no task-type key": {
			recipe: `---
- tasks:
    - name: nothing here
`,
			codes: []string{"task_entry_shape"},
		},
		"empty task body": {
			recipe: `---
- tasks:
    - name: null body
      dokku_app:
`,
			codes: []string{"empty_task_body"},
		},
		"reserved input name": {
			recipe: `---
- inputs:
    - name: tasks
  tasks:
    - name: create app
      dokku_app:
        app: api
`,
			codes: []string{"reserved_input_name"},
		},
	}

	for name, tt := range recipes {
		t.Run(name, func(t *testing.T) {
			exit, out := runValidateOverStdin(t, tt.recipe, []string{"-", "--json"})
			if exit != 1 {
				t.Fatalf("exit = %d, want 1; output:\n%s", exit, out)
			}
			assertLinesMatchSchema(t, validateSchemaPath, out)

			var codes []string
			for _, line := range jsonLines(out) {
				var ev map[string]interface{}
				if err := json.Unmarshal([]byte(line), &ev); err != nil {
					t.Fatalf("invalid JSON line %q: %v", line, err)
				}
				// Not a type assertion with the comma-ok dropped:
				// assertLinesMatchSchema above reports a missing or
				// non-string `code` with t.Errorf, so execution
				// reaches here and a bare assertion would panic the
				// whole test binary instead of failing this case.
				code, ok := ev["code"].(string)
				if !ok {
					t.Fatalf("line %q has no string \"code\" field", line)
				}
				codes = append(codes, code)
			}
			for _, want := range tt.codes {
				if !slices.Contains(codes, want) {
					t.Errorf("expected a %q problem, got codes %v\noutput:\n%s", want, codes, out)
				}
			}
		})
	}
}

// TestValidateJSONEmitsNothingOnSuccess pins the other half of the
// contract: a clean recipe produces an empty stdout and exit 0, so a
// wrapper can treat "any output at all" as failure.
func TestValidateJSONEmitsNothingOnSuccess(t *testing.T) {
	exit, out := runValidateOverStdin(t, stdinYAMLRecipe, []string{"-", "--json"})
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; output:\n%s", exit, out)
	}
	if strings.TrimSpace(out) != "" {
		t.Errorf("expected no output on a clean recipe, got:\n%s", out)
	}
}

// TestValidateWarnsOnAmbiguousDefaultProbe is the second half of #420:
// "tasks.yml is valid" reads as a verdict on the whole directory, and
// with a tasks.json sitting next to it that is wrong. The warning goes
// to stderr, so a --json consumer reading stdout is unaffected.
func TestValidateWarnsOnAmbiguousDefaultProbe(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	writeAmbiguousRecipes(t, dir)

	ui := cli.NewMockUi()
	c := &ValidateCommand{Meta: command.Meta{Ui: ui}}
	if exit := c.Run(nil); exit != 0 {
		t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
	}

	warn := ui.ErrorWriter.String()
	for _, want := range []string{"tasks.yml", "tasks.json", "both exist", "--tasks"} {
		if !strings.Contains(warn, want) {
			t.Errorf("warning missing %q:\n%s", want, warn)
		}
	}
	if out := ui.OutputWriter.String(); !strings.Contains(out, "tasks.yml is valid") {
		t.Errorf("validate should still report on tasks.yml:\n%s", out)
	}
}

// TestValidateDoesNotWarnWhenTasksIsExplicit: naming the recipe is the
// remedy the warning suggests, so it must not fire once the user has
// taken it. Nor should a directory with a single candidate warn.
func TestValidateDoesNotWarnWhenTasksIsExplicit(t *testing.T) {
	t.Run("explicit --tasks", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		writeAmbiguousRecipes(t, dir)

		ui := cli.NewMockUi()
		c := &ValidateCommand{Meta: command.Meta{Ui: ui}}
		if exit := c.Run([]string{"--tasks", "tasks.json"}); exit != 0 {
			t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
		}
		if warn := ui.ErrorWriter.String(); strings.Contains(warn, "both exist") {
			t.Errorf("an explicit --tasks resolves the ambiguity; should not warn:\n%s", warn)
		}
	})

	t.Run("single candidate", func(t *testing.T) {
		dir := t.TempDir()
		t.Chdir(dir)
		if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte(stdinYAMLRecipe), 0o644); err != nil {
			t.Fatalf("write tasks.yml: %v", err)
		}

		ui := cli.NewMockUi()
		c := &ValidateCommand{Meta: command.Meta{Ui: ui}}
		if exit := c.Run(nil); exit != 0 {
			t.Fatalf("exit = %d, want 0: %s", exit, ui.ErrorWriter.String())
		}
		if warn := ui.ErrorWriter.String(); warn != "" {
			t.Errorf("one candidate is unambiguous; should not warn:\n%s", warn)
		}
	})
}

// writeAmbiguousRecipes drops a valid tasks.yml and a valid tasks.json
// into dir, the directory shape the probe cannot resolve on its own.
func writeAmbiguousRecipes(t *testing.T, dir string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "tasks.yml"), []byte(stdinYAMLRecipe), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "tasks.json"), []byte(stdinJSON5Recipe), 0o644); err != nil {
		t.Fatalf("write tasks.json: %v", err)
	}
}
