package commands

import (
	"strings"
	"testing"
)

// required_input_test.go covers the `required: true` half of #493. The check
// used to be `argument.Required && !argument.HasValue()`, and HasValue() is
// always true for a bool / int / float, because pflag's pointer for one is
// never nil - so `required:` was only ever enforced for string inputs. Fixing
// the flag registration made `type: bool, required: true` with no default
// reachable for the first time, where it would otherwise have resolved silently
// to false.
//
// The rule is now "the recipe declares a default, or the user supplied a
// value", which is what validateStrictInputs already assumed offline.

const requiredBoolRecipe = `---
- inputs:
    - { name: debug, type: bool, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`

func TestRequiredInputIsEnforcedForEveryType(t *testing.T) {
	recipes := map[string]string{
		"bool": requiredBoolRecipe,
		"int": `---
- inputs:
    - { name: debug, type: int, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`,
		"float": `---
- inputs:
    - { name: debug, type: float, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`,
		"string": `---
- inputs:
    - { name: debug, type: string, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`,
	}

	for name, recipe := range recipes {
		t.Run(name, func(t *testing.T) {
			path := writeTasksFile(t, recipe)
			for _, cmd := range []struct {
				name string
				run  func(t *testing.T, path string, args ...string) (string, string, int)
			}{
				{"apply", runApply},
				{"plan", runPlan},
			} {
				stdout, stderr, exit := cmd.run(t, path, "--list-tasks")
				if exit == 0 {
					t.Errorf("%s: exit = 0, want non-zero for a required input with no value; stdout=%s", cmd.name, stdout)
				}
				if !strings.Contains(stderr, "Missing flag '--debug'") {
					t.Errorf("%s: stderr does not name the missing input; got:\n%s", cmd.name, stderr)
				}
			}
		})
	}
}

func TestRequiredInputSatisfiedBySuppliedValue(t *testing.T) {
	path := writeTasksFile(t, requiredBoolRecipe)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--debug=false")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=web-false]") {
		t.Errorf("expected the supplied value in the listing; got:\n%s", stdout)
	}
}

func TestRequiredInputSatisfiedByDeclaredDefault(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: debug, type: bool, required: true, default: "true" }
  tasks:
    - dokku_app: { app: "web-{{ .debug }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "dokku_app[app=web-true]") {
		t.Errorf("expected the declared default in the listing; got:\n%s", stdout)
	}
}

// TestMissingRequiredInputIsDeterministic: the argument map is walked in sorted
// order, so a recipe missing two required inputs names the same one every run.
func TestMissingRequiredInputIsDeterministic(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: zebra, required: true }
    - { name: alpha, required: true }
  tasks:
    - dokku_app: { app: web }
`)

	for i := 0; i < 5; i++ {
		_, stderr, exit := runApply(t, path, "--list-tasks")
		if exit == 0 {
			t.Fatalf("exit = 0, want non-zero")
		}
		if !strings.Contains(stderr, "Missing flag '--alpha'") {
			t.Fatalf("run %d named a different input; got:\n%s", i, stderr)
		}
	}
}

// TestRequiredStringInputIsNotSatisfiedByAnEmptyValue: `--app=` types the flag
// but supplies nothing. Counting that as an answer would let the input resolve
// to "" and render an app named `web-`, which is why IsSatisfied checks the
// resolved value as well as where it came from.
func TestRequiredStringInputIsNotSatisfiedByAnEmptyValue(t *testing.T) {
	path := writeTasksFile(t, `---
- inputs:
    - { name: app, required: true }
  tasks:
    - dokku_app: { app: "web-{{ .app }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--app=")
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero; stdout=%s", stdout)
	}
	if !strings.Contains(stderr, "Missing flag '--app'") {
		t.Errorf("stderr does not name the missing input; got:\n%s", stderr)
	}
	if strings.Contains(stdout, "<no value>") {
		t.Errorf("an empty value rendered into the plan; got:\n%s", stdout)
	}

	stdout, stderr, exit = runValidate(t, path, "--strict", "--app=")
	if exit != 1 {
		t.Fatalf("--strict exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, `input "app" is required`) {
		t.Errorf("--strict accepted an empty value; got:\n%s", stdout+stderr)
	}
}

// TestValidateStrictFlagsRequiredNonStringInput: --strict built its override map
// from HasValue(), so every non-string input counted as supplied and
// input_missing never fired for one.
func TestValidateStrictFlagsRequiredNonStringInput(t *testing.T) {
	path := writeTasksFile(t, requiredBoolRecipe)

	stdout, stderr, exit := runValidate(t, path, "--strict")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, `input "debug" is required`) {
		t.Errorf("--strict did not flag the required bool; got:\n%s", stdout+stderr)
	}

	stdout, stderr, exit = runValidate(t, path, "--strict", "--debug=false")
	if exit != 0 {
		t.Fatalf("supplying the value left exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
}
