package commands

import (
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// sensitive_default_masking_test.go covers #490: the value a `sensitive: true`
// input resolves to from its own `default:`, rather than from a --vars-file or
// a --name=value flag.
//
// Two halves, because they run at different times. At run time the default is
// registered like any other resolved value, but every other masking test
// supplies its secret on the command line, so the default-only path was
// unasserted. At help time nothing is registered at all - pflag renders
// `(default <DefValue>)` straight off the flag, before a recipe is parsed - so
// the default printed in the clear until registerInputFlags started rewriting
// that field.

// helpCommand is the slice of a command that a --help page needs. Every
// command that can register recipe-input flags implements it.
type helpCommand interface {
	Help() string
}

// helpCommands builds each command whose --help page can carry input flags.
// The three share registerInputFlags but reach it from three separate
// FlagSet() implementations, so each is exercised on its own.
var helpCommands = map[string]func(argv []string) helpCommand{
	"apply": func(argv []string) helpCommand {
		return &ApplyCommand{Meta: command.Meta{Ui: cli.NewMockUi()}, Argv: argv}
	},
	"plan": func(argv []string) helpCommand {
		return &PlanCommand{Meta: command.Meta{Ui: cli.NewMockUi()}, Argv: argv}
	},
	"validate": func(argv []string) helpCommand {
		return &ValidateCommand{Meta: command.Meta{Ui: cli.NewMockUi()}, Argv: argv}
	},
}

// helpText renders one command's --help page for the recipe at path. FlagSet()
// resolves the recipe out of the command's argv rather than from the parsed
// flags, so the argv is handed over the way runApply and runPlan hand it.
func helpText(t *testing.T, name, path string) string {
	t.Helper()
	build, ok := helpCommands[name]
	if !ok {
		t.Fatalf("no help command registered for %q", name)
	}
	argv := []string{"docket-test", name, "--tasks", path}

	return build(argv).Help()
}

// TestHelpMasksSensitiveInputDefault is the #490 regression. A recipe that
// writes a secret into `default:` used to have it echoed verbatim by
// `docket apply --help`, on a path no MaskString could reach: help is rendered
// before any recipe is parsed, so subprocess's registry is still empty.
func TestHelpMasksSensitiveInputDefault(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: token, default: helpdefaultzzz, sensitive: true }
  tasks:
    - dokku_stub: { key: "{{ .token }}" }
`)

	for _, name := range []string{"apply", "plan", "validate"} {
		t.Run(name, func(t *testing.T) {
			out := helpText(t, name, path)
			if !strings.Contains(out, "--token") {
				t.Fatalf("%s --help did not register the input flag at all; got:\n%s", name, out)
			}
			if strings.Contains(out, "helpdefaultzzz") {
				t.Errorf("%s --help leaked the sensitive input's default; got:\n%s", name, out)
			}
			if !strings.Contains(out, `(default "***")`) {
				t.Errorf("%s --help should render the default as the mask placeholder; got:\n%s", name, out)
			}
		})
	}
}

// TestHelpKeepsNonSensitiveInputDefault pins the other side of the rewrite: it
// applies to `sensitive: true` inputs only, and only to those that declared a
// default. An ordinary input still advertises its default - that is what the
// help page is for - and a sensitive input with nothing to hide gains no
// spurious placeholder.
func TestHelpKeepsNonSensitiveInputDefault(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: app, default: web }
    - { name: token, required: true, sensitive: true }
  tasks:
    - dokku_stub: { key: "{{ .app }}" }
`)

	for _, name := range []string{"apply", "plan", "validate"} {
		t.Run(name, func(t *testing.T) {
			out := helpText(t, name, path)
			if !strings.Contains(out, `(default "web")`) {
				t.Errorf("%s --help should still advertise an ordinary input's default; got:\n%s", name, out)
			}
			if strings.Contains(out, `(default "***")`) {
				t.Errorf("%s --help masked a default that was never declared; got:\n%s", name, out)
			}
		})
	}
}

// TestListTasksMasksSensitiveInputResolvedFromDefault is the run-time half.
// The listing renders resolved values, and with no override the resolved value
// is the declared default - which reaches the mask registry through
// Argument.StringValue() exactly as a --vars-file or CLI value would.
func TestListTasksMasksSensitiveInputResolvedFromDefault(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, default: defaultsecretzzz, sensitive: true }
  tasks:
    - dokku_stub: { key: "{{ .secret_value }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "defaultsecretzzz") {
		t.Errorf("listing leaked a sensitive input resolved from its default; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "dokku_stub[key=***]") {
		t.Errorf("expected the address to render with a masked key; got:\n%s", stdout)
	}

	stdout, stderr, exit = runApply(t, path, "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "defaultsecretzzz") {
		t.Errorf("--json listing leaked a sensitive input resolved from its default; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"dokku_stub[key=***]"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}
