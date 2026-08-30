package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	flag "github.com/spf13/pflag"
)

// input_defaults_test.go covers #493. A `type: bool` input that declared no
// `default:` used to make registerInputFlags return an error, and all three
// FlagSet() callers discarded it - so the recipe came back carrying only the
// built-in flags. The blast radius was the whole recipe: one input written the
// obvious way cost every *other* input its flag, with nothing printed to say
// why. `int` and `float` had the same hole, reached through strconv.
//
// A JSON5 recipe fell into the same hole from a different direction, by writing
// the natural `default: 8080` into a field that is a Go string.
//
// The declaration itself is now judged by checkInputDeclarations, so the tests
// for a malformed `type:` / `default:` live in tasks/input_declaration_test.go
// and validate_input_declaration_test.go; what is pinned here is that
// registration never loses a flag over one, and that every downstream consumer
// of the argument map - --help, --list-tasks, --vars-file - sees it.

// issue493Recipe is the recipe from the issue, verbatim in shape: a bool input
// with no default sitting beside an unrelated, correctly declared one.
const issue493Recipe = `---
- inputs:
    - { name: debug, type: bool }
    - { name: app_name, default: web }
  tasks:
    - dokku_app: { app: "{{ .app_name }}" }
`

func TestRegisterInputFlagsBoolWithoutDefault(t *testing.T) {
	f := flag.NewFlagSet("apply", flag.ContinueOnError)
	arguments, err := registerInputFlags(f, []byte(issue493Recipe), tasks.FormatYAML)
	if err != nil {
		t.Fatalf("registerInputFlags returned error: %v", err)
	}

	for _, name := range []string{"debug", "app_name"} {
		if f.Lookup(name) == nil {
			t.Errorf("flag --%s was not registered", name)
		}
		if _, ok := arguments[name]; !ok {
			t.Errorf("argument %q is missing from the argument map", name)
		}
	}

	debug, ok := arguments["debug"]
	if !ok {
		t.Fatal("no argument registered for the bool input")
	}
	if debug.Type != "bool" {
		t.Errorf("debug.Type = %q, want %q", debug.Type, "bool")
	}
	if debug.HasDefault {
		t.Error("debug.HasDefault = true, want false for an input that declared no default")
	}
	if v, isBool := debug.GetValue().(*bool); !isBool || v == nil || *v {
		t.Errorf("debug did not register as false; GetValue() = %#v", debug.GetValue())
	}
}

// TestRegisterInputFlagsZeroValueDefaults pins the documented "zero value for
// the type" for every type that can omit a default.
func TestRegisterInputFlagsZeroValueDefaults(t *testing.T) {
	recipe := `---
- inputs:
    - { name: b, type: bool }
    - { name: i, type: int }
    - { name: fl, type: float }
    - { name: s, type: string }
  tasks: []
`
	f := flag.NewFlagSet("apply", flag.ContinueOnError)
	arguments, err := registerInputFlags(f, []byte(recipe), tasks.FormatYAML)
	if err != nil {
		t.Fatalf("registerInputFlags returned error: %v", err)
	}

	tests := []struct {
		name string
		want string
	}{
		{"b", "false"},
		{"i", "0"},
		{"fl", "0"},
		{"s", ""},
	}
	for _, tt := range tests {
		arg, ok := arguments[tt.name]
		if !ok {
			t.Errorf("argument %q is missing", tt.name)
			continue
		}
		if got := arg.StringValue(); got != tt.want {
			t.Errorf("input %q resolved to %q, want %q", tt.name, got, tt.want)
		}
		if arg.HasDefault {
			t.Errorf("input %q reports HasDefault, but declared no default", tt.name)
		}
	}
}

// TestRegisterInputFlagsKeepsSurfaceOnMalformedInput: a malformed declaration
// is rejected by the loader, not by dropping flags. Every input still gets one,
// so `--other=x` parses and the operator reads the real diagnostic instead of
// "unknown flag".
func TestRegisterInputFlagsKeepsSurfaceOnMalformedInput(t *testing.T) {
	recipes := map[string]string{
		"unknown type": `---
- inputs:
    - { name: broken, type: bogus }
    - { name: other, default: web }
  tasks: []
`,
		"unparseable int default": `---
- inputs:
    - { name: broken, type: int, default: abc }
    - { name: other, default: web }
  tasks: []
`,
		"unparseable bool default": `---
- inputs:
    - { name: broken, type: bool, default: maybe }
    - { name: other, default: web }
  tasks: []
`,
	}
	for name, recipe := range recipes {
		t.Run(name, func(t *testing.T) {
			f := flag.NewFlagSet("apply", flag.ContinueOnError)
			arguments, err := registerInputFlags(f, []byte(recipe), tasks.FormatYAML)
			if err != nil {
				t.Fatalf("registerInputFlags returned error: %v", err)
			}
			for _, want := range []string{"broken", "other"} {
				if f.Lookup(want) == nil {
					t.Errorf("flag --%s was not registered", want)
				}
				if _, ok := arguments[want]; !ok {
					t.Errorf("argument %q is missing from the argument map", want)
				}
			}
			if err := f.Parse([]string{"--other=override"}); err != nil {
				t.Fatalf("parsing an unrelated input flag failed: %v", err)
			}
		})
	}
}

// TestApplyListTasksWithBoolInputWithoutDefault is the issue's own
// reproduction: `--app_name=override` used to fail with "unknown flag".
func TestApplyListTasksWithBoolInputWithoutDefault(t *testing.T) {
	path := writeTasksFile(t, issue493Recipe)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--app_name=override")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=override]") {
		t.Errorf("the input override did not reach the listing; got:\n%s", stdout)
	}
}

func TestApplyParsesBoolInputFlagWithoutDefault(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: debug, type: bool }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`)

	for _, tt := range []struct {
		args []string
		want string
	}{
		{[]string{"--list-tasks"}, "dokku_app[app=web-false]"},
		{[]string{"--list-tasks", "--debug=true"}, "dokku_app[app=web-true]"},
		{[]string{"--list-tasks", "--debug"}, "dokku_app[app=web-true]"},
	} {
		stdout, stderr, exit := runApply(t, path, tt.args...)
		if exit != 0 {
			t.Fatalf("%v: exit = %d, want 0; stderr=%s", tt.args, exit, stderr)
		}
		if !strings.Contains(stdout, tt.want) {
			t.Errorf("%v: want %q in output; got:\n%s", tt.args, tt.want, stdout)
		}
	}
}

// TestHelpListsInputsAlongsideABoolWithoutDefault pins the other half of the
// #493 symptom: neither input reached `--help` either. Asserted on all three
// commands, which reach registerInputFlags through separate FlagSet() bodies.
func TestHelpListsInputsAlongsideABoolWithoutDefault(t *testing.T) {
	path := writeTasksFile(t, issue493Recipe)

	for name := range helpCommands {
		out := helpText(t, name, path)
		for _, want := range []string{"--debug", "--app_name"} {
			if !strings.Contains(out, want) {
				t.Errorf("%s --help omits %s; got:\n%s", name, want, out)
			}
		}
	}
}

// TestRegisterInputFlagsJSON5NonStringDefault is the same symptom reached
// through a second door. `Input.Default` is a string field, and encoding/json
// refuses to put a number or a bare boolean into one - so a JSON5 recipe
// writing the natural `default: 8080` failed to decode, and the only caller
// that noticed was flag registration, which discarded the error. YAML never had
// the problem, because yaml.v3 assigns an unquoted scalar to a string field
// verbatim; the JSON5 path now normalises through YAML so both agree.
func TestRegisterInputFlagsJSON5NonStringDefault(t *testing.T) {
	recipe := `[
  { inputs: [{ name: "port", type: "int", default: 8080 },
             { name: "debug", type: "bool", default: true },
             { name: "app_name", default: "web" }],
    tasks: [{ dokku_app: { app: "web" } }] },
]`
	f := flag.NewFlagSet("apply", flag.ContinueOnError)
	arguments, err := registerInputFlags(f, []byte(recipe), tasks.FormatNameJSON5)
	if err != nil {
		t.Fatalf("registerInputFlags returned error: %v", err)
	}
	for _, name := range []string{"port", "debug", "app_name"} {
		if f.Lookup(name) == nil {
			t.Errorf("flag --%s was not registered", name)
		}
	}
	if got := arguments["port"].StringValue(); got != "8080" {
		t.Errorf("port resolved to %q, want %q", got, "8080")
	}
	if got := arguments["debug"].StringValue(); got != "true" {
		t.Errorf("debug resolved to %q, want %q", got, "true")
	}
	if !arguments["port"].HasDefault {
		t.Error("port.HasDefault = false, but the recipe declared one")
	}
}

// TestVarsFileSuppliesInputWithNoDefault: a --vars-file key is matched against
// the registered argument names, so an input that never registered was reported
// as unknown with no did-you-mean to offer. Every typed input with no default
// used to be in that state.
func TestVarsFileSuppliesInputWithNoDefault(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: debug, type: bool }
    - { name: replicas, type: int }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}-{{ .replicas }}" }
`)
	vars := filepath.Join(filepath.Dir(path), "vars.yml")
	if err := os.WriteFile(vars, []byte("debug: true\nreplicas: 5\n"), 0o644); err != nil {
		t.Fatalf("write vars.yml: %v", err)
	}

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--vars-file", vars)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=web-true-5]") {
		t.Errorf("vars-file values did not reach the listing; got:\n%s", stdout)
	}
}

// TestSensitiveInputDoesNotRegisterAnImplicitZero: newly reachable once a
// non-string input can omit its default. Registering the zero value as a secret
// would hand the masker "0" and blank out every unrelated digit in the output.
func TestSensitiveInputDoesNotRegisterAnImplicitZero(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: port, type: int, sensitive: true }
  tasks:
    - dokku_app: { app: web-0 }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=web-0]") {
		t.Errorf("an implicit zero was registered as a secret and masked unrelated output; got:\n%s", stdout)
	}
}
