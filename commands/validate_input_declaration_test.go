package commands

import (
	"os"
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// validate_input_declaration_test.go covers the two diagnostics #493 added.
// Before them, `type: bogus` and a `default:` that could not be parsed as the
// type it was declared under produced no message at all: registerInputFlags was
// the only code that read an input's type, and every FlagSet() caller discarded
// the error it returned. The recipe linted clean and then behaved as though it
// had declared no inputs.

// runValidate drives a ValidateCommand against the tasks file at path, the way
// runApply and runPlan drive theirs. FlagSet() resolves --tasks out of os.Args
// rather than from the parsed flags, so the argv is staged first.
func runValidate(t *testing.T, path string, args ...string) (string, string, int) {
	t.Helper()
	origArgs := os.Args
	os.Args = []string{"docket-test", "validate", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	c := &ValidateCommand{Meta: command.Meta{Ui: cli.NewMockUi()}}
	all := append([]string{"--tasks", path}, args...)
	exit := c.Run(all)
	ui := c.Ui.(*cli.MockUi)
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit
}

func TestValidateReportsInvalidInputDeclarations(t *testing.T) {
	tests := map[string]struct {
		recipe  string
		code    string
		message string
		hint    string
	}{
		"unknown type": {
			recipe: `---
- inputs:
    - { name: port, type: intt, default: "8080" }
  tasks: []
`,
			code:    "invalid_input_type",
			message: `input "port" declares unknown type "intt"`,
			hint:    `did you mean "int"?`,
		},
		"unknown type with no near miss": {
			recipe: `---
- inputs:
    - { name: port, type: duration }
  tasks: []
`,
			code:    "invalid_input_type",
			message: `input "port" declares unknown type "duration"`,
			hint:    "use one of bool, float, int, string",
		},
		"int default": {
			recipe: `---
- inputs:
    - { name: port, type: int, default: abc }
  tasks: []
`,
			code:    "invalid_input_default",
			message: `input "port" declares type int but its default "abc" is not a valid int`,
			hint:    "use a whole number",
		},
		"float default": {
			recipe: `---
- inputs:
    - { name: ratio, type: float, default: half }
  tasks: []
`,
			code:    "invalid_input_default",
			message: `input "ratio" declares type float but its default "half" is not a valid float`,
			hint:    "use a number",
		},
		"bool default": {
			recipe: `---
- inputs:
    - { name: debug, type: bool, default: maybe }
  tasks: []
`,
			code:    "invalid_input_default",
			message: `input "debug" declares type bool but its default "maybe" is not a valid bool`,
			hint:    "use one of true, yes, on, y",
		},
	}

	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeTasksFile(t, tt.recipe)

			stdout, stderr, exit := runValidate(t, path)
			if exit != 1 {
				t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
			}
			human := stdout + stderr
			if !strings.Contains(human, tt.message) {
				t.Errorf("human output missing %q; got:\n%s", tt.message, human)
			}
			if !strings.Contains(human, tt.hint) {
				t.Errorf("human output missing hint %q; got:\n%s", tt.hint, human)
			}

			jsonOut, jsonErr, exit := runValidate(t, path, "--json")
			if exit != 1 {
				t.Fatalf("--json exit = %d, want 1; stdout=%s stderr=%s", exit, jsonOut, jsonErr)
			}
			if !strings.Contains(jsonOut, `"code":"`+tt.code+`"`) {
				t.Errorf("--json output missing code %q; got:\n%s", tt.code, jsonOut)
			}
			assertLinesMatchSchema(t, validateSchemaPath, jsonOut)
		})
	}
}

// TestValidateAcceptsOmittedDefaults: an omitted `default:` is the zero value
// for the type, which is what the inputs table has always documented. It must
// not be mistaken for an unparseable one.
func TestValidateAcceptsOmittedDefaults(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: debug, type: bool }
    - { name: replicas, type: int }
    - { name: ratio, type: float }
    - { name: app_name, type: string }
  tasks:
    - dokku_app: { app: web }
`)

	stdout, stderr, exit := runValidate(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
}

// TestApplyRejectsInvalidInputDeclarationOffline: plan and apply must fail with
// the same message validate prints, before any server is contacted - the
// contract invalid_input_name already holds to.
func TestApplyRejectsInvalidInputDeclarationOffline(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: port, type: int, default: abc }
  tasks:
    - dokku_app: { app: web }
`)

	for _, tt := range []struct {
		name string
		run  func(t *testing.T, path string, args ...string) (string, string, int)
	}{
		{"apply", runApply},
		{"plan", runPlan},
	} {
		stdout, stderr, exit := tt.run(t, path, "--list-tasks")
		if exit == 0 {
			t.Fatalf("%s: exit = 0, want non-zero; stdout=%s", tt.name, stdout)
		}
		if !strings.Contains(stderr, `default "abc" is not a valid int`) {
			t.Errorf("%s: stderr does not name the offending input; got:\n%s", tt.name, stderr)
		}
		if strings.Contains(stderr, "panic:") || strings.Contains(stderr, "unknown flag") {
			t.Errorf("%s: unexpected failure mode; got:\n%s", tt.name, stderr)
		}
	}
}
