package commands

import (
	"strings"
	"testing"
)

// identity_address_quoting_masking_test.go covers #475: a generated resource
// address wraps a key value in strconv.Quote when a bare form would not parse
// back out of the address, and that escapes the value, so the literal
// registered in the mask registry no longer matches it there. Every stream
// that prints a task name leaked it - the apply and plan run streams as well
// as --list-tasks - so there is a case per stream, on both the human and the
// --json path.
//
// None of the recipes here pins a `name:`, because the generated address is
// exactly what is under test.

// quotedIdentityRecipe interpolates a sensitive input into the stub's single
// identity key. `dq` escapes the value for the double-quoted scalar it sits
// in, which is what keeps the recipe parseable with a quote in the value.
const quotedIdentityRecipe = `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_stub: { key: "{{ .secret_value | dq }}" }
`

// quoteBearingSecret is the flag value every case below passes. The embedded
// double quote is what forces the address to quote and escape the value; the
// `zzz` suffix keeps it from colliding with anything docket prints of its own.
const quoteBearingSecret = `quo"tedzzz`

// maskedStubAddress is the address every case expects once the secret masks:
// the key value collapses to `***` inside the quotes the address still needs.
const maskedStubAddress = `dokku_stub[key="***"]`

func TestListTasksMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+quoteBearingSecret, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("listing leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, maskedStubAddress) {
		t.Errorf("expected a masked identity value in the address; got:\n%s", stdout)
	}
}

func TestListTasksJSONMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+quoteBearingSecret, "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("--json listing leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"dokku_stub[key=\"***\"]"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

func TestApplyMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	stubSet(quoteBearingSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+quoteBearingSecret)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("the apply run stream leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, maskedStubAddress) {
		t.Errorf("expected a masked identity value in the address; got:\n%s", stdout)
	}
}

func TestApplyJSONMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	stubSet(quoteBearingSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+quoteBearingSecret, "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("the apply --json run stream leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"dokku_stub[key=\"***\"]"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

func TestPlanMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	stubSet(quoteBearingSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runPlan(t, path, "--secret_value="+quoteBearingSecret)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("the plan run stream leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, maskedStubAddress) {
		t.Errorf("expected a masked identity value in the address; got:\n%s", stdout)
	}
}

func TestPlanJSONMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	stubSet(quoteBearingSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runPlan(t, path, "--secret_value="+quoteBearingSecret, "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("the plan --json run stream leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"dokku_stub[key=\"***\"]"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

// TestStartAtTaskHintMasksQuotedIdentityValueInName covers the one stream the
// registry alone cannot reach. The "available names" hint renders every name
// through `%q`, a second layer of Go quoting on top of the escaping a
// generated address already carries, so no registered spelling can match the
// finished message. The hint masks each name before quoting it instead.
func TestStartAtTaskHintMasksQuotedIdentityValueInName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, quotedIdentityRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+quoteBearingSecret, "--start-at-task", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stderr, "tedzzz") {
		t.Errorf("the --start-at-task hint leaked the quote-bearing sensitive input; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"dokku_stub[key=\"***\"]"`) {
		t.Errorf("expected a masked name in the hint; got:\n%s", stderr)
	}
}

// TestListTasksMasksQuotedTaskDeclaredIdentityValue is why the escaped
// spelling is registered in the mask registry rather than where the address is
// built. Nothing here is a sensitive *input*: dokku_config declares its whole
// `config:` map sensitive, so the value joins the registry through the task
// walk - after the recipe has parsed and already generated every name. The
// address cannot escape-and-mask at generation time, because the registry is
// not populated yet.
func TestListTasksMasksQuotedTaskDeclaredIdentityValue(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - dokku_config:
        app: api
        config:
          LABEL: '--label quotedeclaredzzz="x"'
    - dokku_docker_options:
        app: api
        phase: deploy
        option: '--label quotedeclaredzzz="x"'
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "quotedeclaredzzz") {
		t.Errorf("listing leaked a quote-bearing task-declared sensitive value; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"dokku_docker_options[app=api,phase=deploy,option=\"***\"]"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

// TestListTasksLeavesUnquotedIdentityValuesAlone pins the boundary:
// registering the escaped spelling must not widen masking to values that
// merely resemble a secret. `keepzzz` is not registered, so it prints in full
// alongside the masked task.
func TestListTasksLeavesUnquotedIdentityValuesAlone(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_stub: { key: "{{ .secret_value | dq }}" }
    - dokku_stub: { key: keepzzz }
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+quoteBearingSecret, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "tedzzz") {
		t.Errorf("listing leaked the quote-bearing sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "dokku_stub[key=keepzzz]") {
		t.Errorf("expected the non-sensitive identity value to print in full; got:\n%s", stdout)
	}
}
