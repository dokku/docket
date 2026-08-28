package commands

import (
	"strings"
	"testing"
)

// loop_item_whitespace_masking_test.go covers #473: a sensitive value carrying
// leading or trailing whitespace reaches the `(item=<value>)` task-name suffix
// through strings.TrimSpace, so the literal registered in the mask registry no
// longer matches it there. Every stream that prints a task name leaked it -
// the apply and plan run streams as well as --list-tasks - so there is a case
// per stream, on both the human and the --json path.
//
// The recipes all pad the value with spaces on both sides and pin the task
// name, so the only place the item value can surface is the name suffix.

// whitespaceLoopItemRecipe loops over a sensitive input whose single element
// keeps the input's surrounding whitespace.
const whitespaceLoopItemRecipe = `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      loop: 'split(secret_value, ",")'
      dokku_stub: { key: "{{ .item }}" }
`

// whitespacePaddedSecret is the flag value every case below passes. The
// padding is what defeats the literal match; the `zzz` suffix keeps the
// value from colliding with anything docket prints of its own.
const whitespacePaddedSecret = " whitespacezzz "

func TestListTasksMasksWhitespacePaddedLoopItemInName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, whitespaceLoopItemRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+whitespacePaddedSecret, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("listing leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy (item=***)") {
		t.Errorf("expected a masked loop item in the name; got:\n%s", stdout)
	}
}

func TestListTasksJSONMasksWhitespacePaddedLoopItemInName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, whitespaceLoopItemRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+whitespacePaddedSecret, "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("--json listing leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"deploy (item=***)"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

func TestApplyMasksWhitespacePaddedLoopItemInName(t *testing.T) {
	defer stubReset()
	stubSet(whitespacePaddedSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, whitespaceLoopItemRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+whitespacePaddedSecret)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("the apply run stream leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy (item=***)") {
		t.Errorf("expected a masked loop item in the name; got:\n%s", stdout)
	}
}

func TestApplyJSONMasksWhitespacePaddedLoopItemInName(t *testing.T) {
	defer stubReset()
	stubSet(whitespacePaddedSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, whitespaceLoopItemRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+whitespacePaddedSecret, "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("the apply --json run stream leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"deploy (item=***)"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

func TestPlanMasksWhitespacePaddedLoopItemInName(t *testing.T) {
	defer stubReset()
	stubSet(whitespacePaddedSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, whitespaceLoopItemRecipe)

	stdout, stderr, exit := runPlan(t, path, "--secret_value="+whitespacePaddedSecret)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("the plan run stream leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy (item=***)") {
		t.Errorf("expected a masked loop item in the name; got:\n%s", stdout)
	}
}

func TestPlanJSONMasksWhitespacePaddedLoopItemInName(t *testing.T) {
	defer stubReset()
	stubSet(whitespacePaddedSecret, StubFixture{Changed: true})
	path := writeTasksFile(t, whitespaceLoopItemRecipe)

	stdout, stderr, exit := runPlan(t, path, "--secret_value="+whitespacePaddedSecret, "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("the plan --json run stream leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"deploy (item=***)"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

// TestListTasksMasksWhitespacePaddedTaskDeclaredLoopItem is why the trimmed
// spelling is registered in the mask registry rather than at the loop's own
// expansion site. Nothing here is a sensitive *input*: dokku_config declares
// its whole `config:` map sensitive, so the value joins the registry through
// CollectPlaySensitiveValues - after the recipe has parsed and already named
// its expansions. A fix that trimmed at expansion time would still leak here.
func TestListTasksMasksWhitespacePaddedTaskDeclaredLoopItem(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: configure
      loop: [" taskdeclaredzzz "]
      dokku_config:
        app: api
        config:
          TOKEN: "{{ .item }}"
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "taskdeclaredzzz") {
		t.Errorf("listing leaked a whitespace-padded task-declared sensitive value; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"configure (item=***)"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

// TestListTasksLeavesUnpaddedLoopItemsAlone pins the boundary: registering the
// trimmed spelling must not widen masking to values that merely resemble a
// secret. `keepzzz` is not registered, so it prints in full even though the
// registered secret trims to a different value entirely.
func TestListTasksLeavesUnpaddedLoopItemsAlone(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      loop: ["{{ .secret_value }}", keepzzz]
      dokku_stub: { key: "{{ .item }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+whitespacePaddedSecret, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "whitespacezzz") {
		t.Errorf("listing leaked the whitespace-padded sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy (item=keepzzz)") {
		t.Errorf("expected the non-sensitive loop item to print in full; got:\n%s", stdout)
	}
}
