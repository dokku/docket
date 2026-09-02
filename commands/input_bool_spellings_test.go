package commands

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	flag "github.com/spf13/pflag"
)

// input_bool_spellings_test.go covers #495. A bool input had three faces and
// each parsed a different vocabulary: `default:` and `--vars-file` read
// true/yes/on/y, pflag read true/false/1/0/t/f on the command line, and neither
// set contained the other - so `default: 1` was an error while `--debug=1` was
// not, and `default: True` was an error while YAML considered it the same value
// as `default: true`. docket's table is now the case-insensitive union, which
// makes it a superset of pflag's.
//
// The second half is that a resolved default has to reach the render as its
// type. An input declared on a play that also has tasks is play-local, which is
// the shape of the issue's own reproduction, and those defaults used to layer
// into the context as the raw text the recipe wrote.

// boolSpellingRecipe declares one bool input with the given default and renders
// it into a task body, so --list-tasks shows what the input resolved to.
func boolSpellingRecipe(def string) string {
	return `---
- inputs:
    - { name: debug, type: bool, default: ` + def + ` }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`
}

func TestBoolDefaultTakesEverySpellingTheCommandLineTakes(t *testing.T) {
	t.Parallel()
	tests := map[string]string{
		"1":     "web-true",
		"t":     "web-true",
		"T":     "web-true",
		"True":  "web-true",
		"TRUE":  "web-true",
		"On":    "web-true",
		"YES":   "web-true",
		"0":     "web-false",
		"f":     "web-false",
		"False": "web-false",
		"FALSE": "web-false",
		"Off":   "web-false",
		"N":     "web-false",
	}
	for def, want := range tests {
		t.Run(def, func(t *testing.T) {
			path := writeTasksFile(t, boolSpellingRecipe(def))

			stdout, stderr, exit := runValidate(t, path)
			if exit != 0 {
				t.Fatalf("validate exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
			}

			stdout, stderr, exit = runApply(t, path, "--list-tasks")
			if exit != 0 {
				t.Fatalf("apply exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
			}
			if !strings.Contains(stdout, "dokku_app[app="+want+"]") {
				t.Errorf("default %q did not resolve to %q; got:\n%s", def, want, stdout)
			}
		})
	}
}

// TestBoolDefaultStillRejectsANearMiss: the table widened, it did not stop
// judging. A spelling in neither vocabulary is still invalid_input_default.
func TestBoolDefaultStillRejectsANearMiss(t *testing.T) {
	t.Parallel()
	for _, def := range []string{"maybe", "2", "yeah"} {
		t.Run(def, func(t *testing.T) {
			path := writeTasksFile(t, boolSpellingRecipe(def))
			stdout, stderr, exit := runValidate(t, path)
			if exit != 1 {
				t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
			}
			human := stdout + stderr
			if !strings.Contains(human, `its default "`+def+`" is not a valid bool`) {
				t.Errorf("output does not name the default; got:\n%s", human)
			}
			if !strings.Contains(human, "use one of true, yes, on, y, t, 1") {
				t.Errorf("output does not carry the widened hint; got:\n%s", human)
			}
		})
	}
}

// TestRegisterInputFlagsWidenedBoolDefault: the flag the recipe registers holds
// the resolved value, so `--help` and an untouched run agree with validate.
func TestRegisterInputFlagsWidenedBoolDefault(t *testing.T) {
	t.Parallel()
	f := flag.NewFlagSet("apply", flag.ContinueOnError)
	arguments, err := registerInputFlags(f, []byte(boolSpellingRecipe("On")), tasks.FormatYAML)
	if err != nil {
		t.Fatalf("registerInputFlags returned error: %v", err)
	}
	debug, ok := arguments["debug"]
	if !ok {
		t.Fatal("no argument registered for the bool input")
	}
	if got := debug.StringValue(); got != "true" {
		t.Errorf("debug resolved to %q, want %q", got, "true")
	}
	if !debug.HasDefault {
		t.Error("debug.HasDefault = false, but the recipe declared one")
	}
}

// TestVarsFileSpellsABoolTheWayTheCommandLineDoes is the issue's third face: a
// vars file used to refuse `debug: 1` while `--debug=1` was accepted.
func TestVarsFileSpellsABoolTheWayTheCommandLineDoes(t *testing.T) {
	t.Parallel()
	tests := map[string]struct {
		vars string
		want string
	}{
		"native number one":  {"debug: 1\n", "web-true"},
		"native number zero": {"debug: 0\n", "web-false"},
		"string cased":       {"debug: \"On\"\n", "web-true"},
		"string off":         {"debug: \"off\"\n", "web-false"},
		"native bool":        {"debug: true\n", "web-true"},
	}
	for name, tt := range tests {
		t.Run(name, func(t *testing.T) {
			path := writeTasksFile(t, boolSpellingRecipe("false"))
			vars := filepath.Join(filepath.Dir(path), "vars.yml")
			if err := os.WriteFile(vars, []byte(tt.vars), 0o644); err != nil {
				t.Fatalf("write vars.yml: %v", err)
			}

			stdout, stderr, exit := runApply(t, path, "--list-tasks", "--vars-file", vars)
			if exit != 0 {
				t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
			}
			if !strings.Contains(stdout, "dokku_app[app="+tt.want+"]") {
				t.Errorf("vars file %q did not resolve to %q; got:\n%s", tt.vars, tt.want, stdout)
			}
		})
	}
}

// TestVarsFileRejectsANumberThatIsNotABool: 1 and 0 are the spellings pflag
// takes, not an invitation to C-style truthiness.
func TestVarsFileRejectsANumberThatIsNotABool(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, boolSpellingRecipe("false"))
	vars := filepath.Join(filepath.Dir(path), "vars.yml")
	if err := os.WriteFile(vars, []byte("debug: 2\n"), 0o644); err != nil {
		t.Fatalf("write vars.yml: %v", err)
	}

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--vars-file", vars)
	if exit == 0 {
		t.Fatalf("exit = 0, want failure; stdout=%s stderr=%s", stdout, stderr)
	}
	human := stdout + stderr
	if !strings.Contains(human, `input "debug"`) || !strings.Contains(human, "got 2") {
		t.Errorf("error does not name the input and the value; got:\n%s", human)
	}
}

// TestPlayLocalBoolDefaultResolvesToItsType is the issue's own reproduction
// shape: one play carrying both the input and the tasks, which makes the input
// play-local. Its default used to layer into the render context as raw text, so
// a widened spelling would have rendered `web-On` where the flag path renders
// `web-true`.
func TestPlayLocalBoolDefaultResolvesToItsType(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- inputs:
    - { name: file_level, type: bool, default: "on" }
- name: api
  inputs:
    - { name: play_local, type: bool, default: "on" }
    - { name: replicas, type: int, default: "007" }
  tasks:
    - dokku_app: { app: "web-{{ .file_level }}-{{ .play_local }}-{{ .replicas }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=web-true-true-7]") {
		t.Errorf("a play-local default did not resolve to its type; got:\n%s", stdout)
	}
}

// TestBoolInputReadsTheSameInWhenFromEitherLayer pins the parity the play-local
// coercion buys: a predicate comparing a bool input reads the same value
// whether the input was declared file-level or play-local. Before, a play-local
// input compared the string "true" to a bool and was never equal.
func TestBoolInputReadsTheSameInWhenFromEitherLayer(t *testing.T) {
	t.Parallel()
	recipe := func(def string) string {
		return `---
- inputs:
    - { name: file_debug, type: bool, default: "` + def + `" }
- name: api
  inputs:
    - { name: play_debug, type: bool, default: "` + def + `" }
  tasks:
    - name: from-file-level
      when: 'file_debug == true'
      dokku_app: { app: a }
    - name: from-play-local
      when: 'play_debug == true'
      dokku_app: { app: b }
`
	}

	t.Run("true", func(t *testing.T) {
		path := writeTasksFile(t, recipe("yes"))
		stdout, stderr, exit := runApply(t, path, "--list-tasks")
		if exit != 0 {
			t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
		}
		if strings.Contains(stdout, "[skipped]") {
			t.Errorf("a task was skipped although both inputs are true; got:\n%s", stdout)
		}
	})

	t.Run("false", func(t *testing.T) {
		path := writeTasksFile(t, recipe("no"))
		stdout, stderr, exit := runApply(t, path, "--list-tasks")
		if exit != 0 {
			t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
		}
		if strings.Count(stdout, "[skipped]") != 2 {
			t.Errorf("want both tasks skipped; got:\n%s", stdout)
		}
	})
}
