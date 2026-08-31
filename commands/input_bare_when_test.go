package commands

import (
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"

	flag "github.com/spf13/pflag"
)

// input_bare_when_test.go covers #497. An input value reached the render and
// the predicates as the pointer pflag allocated for its flag, and both
// evaluators read a pointer as "true" whatever it points at: expr emits an
// OpDeref for the operands of an operator but not for a program that is
// nothing but a top-level identifier, and text/template's isTrue counts any
// non-nil pointer as true. So `when: debug` ran the task for --debug=false,
// `when: replicas` ran it at 0, and `{{ if .debug }}` took the then-branch
// whatever the input held.
//
// The context holds concrete values now, so a bare predicate is a truthiness
// test on the value the operator actually supplied.

// bareWhenRecipe declares one input of the given type and guards one task with
// a predicate that is only that input's name, so --list-tasks reports [skipped]
// exactly when the value is falsy.
func bareWhenRecipe(name, typ string) string {
	decl := "{ name: " + name
	if typ != "" {
		decl += ", type: " + typ
	}
	decl += " }"
	return `---
- inputs:
    - ` + decl + `
  tasks:
    - name: guarded
      when: ` + name + `
      dokku_app: { app: web }
`
}

// assertBareWhen runs apply --list-tasks with args and asserts whether the
// single guarded task was skipped.
func assertBareWhen(t *testing.T, path string, wantSkipped bool, args ...string) {
	t.Helper()
	stdout, stderr, exit := runApply(t, path, append([]string{"--list-tasks"}, args...)...)
	if exit != 0 {
		t.Fatalf("apply %v exit = %d, want 0; stdout=%s stderr=%s", args, exit, stdout, stderr)
	}
	if got := strings.Contains(stdout, "[skipped]"); got != wantSkipped {
		t.Errorf("apply %v: skipped = %v, want %v; got:\n%s", args, got, wantSkipped, stdout)
	}
}

// TestBareWhenOnABoolInputFollowsTheValue is the issue's own reproduction. Both
// spellings pflag accepts for false have to skip, and both for true have to
// run; the zero value an omitted default resolves to is false and skips too.
func TestBareWhenOnABoolInputFollowsTheValue(t *testing.T) {
	path := writeTasksFile(t, bareWhenRecipe("debug", "bool"))

	assertBareWhen(t, path, true)
	assertBareWhen(t, path, true, "--debug=false")
	assertBareWhen(t, path, true, "--debug=0")
	assertBareWhen(t, path, false, "--debug=true")
	assertBareWhen(t, path, false, "--debug=1")
}

// TestBareWhenOnAnIntInputFollowsTheValue: an int has the same hole - the
// pointer is never nil, so `when: replicas` was true at 0.
func TestBareWhenOnAnIntInputFollowsTheValue(t *testing.T) {
	path := writeTasksFile(t, bareWhenRecipe("replicas", "int"))

	assertBareWhen(t, path, true)
	assertBareWhen(t, path, true, "--replicas=0")
	assertBareWhen(t, path, false, "--replicas=2")
}

// TestBareWhenOnAStringInputFollowsTheValue pins the third type. A string
// already behaved - an empty one reached the context as nil, which is falsy -
// so this is the parity check that says all three types now read alike.
func TestBareWhenOnAStringInputFollowsTheValue(t *testing.T) {
	path := writeTasksFile(t, bareWhenRecipe("env", ""))

	assertBareWhen(t, path, true)
	assertBareWhen(t, path, true, "--env=")
	assertBareWhen(t, path, false, "--env=prod")
}

// TestTemplateConditionalFollowsABoolInput is the other half of the issue: a
// `{{ if }}` in a recipe body could not be fixed in truthy, because it is
// text/template's own isTrue that counts a non-nil pointer as true.
//
// The conditional sits in a task field rather than the task name so the
// assertion reads off the resource address the other input tests use, and it
// stays inside a quoted scalar so validate and fmt still parse the raw file.
func TestTemplateConditionalFollowsABoolInput(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: debug, type: bool }
  tasks:
    - dokku_app: { app: "cond-{{ if .debug }}on{{ else }}off{{ end }}" }
`)

	stdout, stderr, exit := runValidate(t, path)
	if exit != 0 {
		t.Fatalf("validate exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}

	stdout, stderr, exit = runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=cond-off]") {
		t.Errorf("a false bool took the then-branch; got:\n%s", stdout)
	}

	stdout, stderr, exit = runApply(t, path, "--list-tasks", "--debug=true")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=cond-on]") {
		t.Errorf("a true bool took the else-branch; got:\n%s", stdout)
	}
}

// TestBareWhenReadsTheSameFromEitherInputLayer is the #495 parity test written
// with the bare predicate: a file-level input and a play-local one reach a
// predicate as the same shape, so both halves skip or run together.
func TestBareWhenReadsTheSameFromEitherInputLayer(t *testing.T) {
	recipe := func(def string) string {
		return `---
- inputs:
    - { name: file_debug, type: bool, default: "` + def + `" }
- name: api
  inputs:
    - { name: play_debug, type: bool, default: "` + def + `" }
  tasks:
    - name: from-file-level
      when: file_debug
      dokku_app: { app: a }
    - name: from-play-local
      when: play_debug
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

// TestUnsetStringInputRendersEmpty: a string input with no value reached the
// context as an untyped nil, which text/template renders as `<no value>` - the
// literal text landed in the task body. It is the empty string now, the zero
// value the inputs table has always documented.
//
// The interpolation is a bare suffix rather than `web-{{ .suffix }}` so the
// assertion does not depend on how the address renderer quotes a value ending
// in a hyphen.
func TestUnsetStringInputRendersEmpty(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: suffix }
  tasks:
    - dokku_app: { app: "web{{ .suffix }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "<no value>") {
		t.Errorf("an unset input rendered the template placeholder; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "dokku_app[app=web]") {
		t.Errorf("an unset input did not render as empty; got:\n%s", stdout)
	}

	stdout, stderr, exit = runApply(t, path, "--list-tasks", "--suffix=-api")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=web-api]") {
		t.Errorf("a supplied suffix did not render; got:\n%s", stdout)
	}
}

// TestBuildInputContextHoldsConcreteValues is the unit-level statement of the
// fix: the map handed to the render and to every predicate holds values, not
// the flag pointers registerInputFlags allocated.
func TestBuildInputContextHoldsConcreteValues(t *testing.T) {
	recipe := `---
- inputs:
    - { name: debug, type: bool }
    - { name: replicas, type: int, default: 3 }
    - { name: ratio, type: float, default: 1.5 }
    - { name: app, default: web }
    - { name: suffix }
  tasks:
    - dokku_app: { app: "{{ .app }}" }
`
	f := flag.NewFlagSet("apply", flag.ContinueOnError)
	arguments, err := registerInputFlags(f, []byte(recipe), tasks.FormatYAML)
	if err != nil {
		t.Fatalf("registerInputFlags returned error: %v", err)
	}

	context, _, err := buildInputContext(arguments, nil)
	if err != nil {
		t.Fatalf("buildInputContext returned error: %v", err)
	}

	want := map[string]interface{}{
		"debug":    false,
		"replicas": 3,
		"ratio":    1.5,
		"app":      "web",
		"suffix":   "",
	}
	for name, wantValue := range want {
		got, ok := context[name]
		if !ok {
			t.Errorf("context is missing %q", name)
			continue
		}
		if got != wantValue {
			t.Errorf("context[%q] = %#v, want %#v", name, got, wantValue)
		}
	}
}
