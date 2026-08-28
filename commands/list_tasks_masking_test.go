package commands

import (
	"strings"
	"testing"
)

// list_tasks_masking_test.go covers #455: --list-tasks masks registered
// sensitive values on both the human and the --json path. The listing renders
// resolved values, so before #455 a `sensitive: true` input came back verbatim
// in every field it reached, and a task-declared secret was not even in the
// registry - commands/apply.go returned from the listing branch before
// CollectPlaySensitiveValues ran.
//
// One test per field a secret can travel through, plus the two that pin what
// must *not* be masked.

// listMasksIdentityRecipe interpolates a sensitive input into a dokku_stub identity
// field. Since #427 an unnamed task is named after the resource it addresses,
// so the secret lands in the generated name.
const listMasksIdentityRecipe = `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_stub: { key: "{{ .secret_value }}" }
`

func TestListTasksMasksSensitiveInputInGeneratedName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, listMasksIdentityRecipe)

	stdout, _, exit := runApply(t, path, "--secret_value=identityzzz", "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "identityzzz") {
		t.Errorf("listing leaked the sensitive input in the generated name; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "dokku_stub[key=***]") {
		t.Errorf("expected the address to render with a masked key; got:\n%s", stdout)
	}
}

func TestListTasksJSONMasksSensitiveInputInGeneratedName(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, listMasksIdentityRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value=identityzzz", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "identityzzz") {
		t.Errorf("--json listing leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"dokku_stub[key=***]"`) {
		t.Errorf("expected a masked name field; got:\n%s", stdout)
	}
}

// TestPlanListTasksMasksSensitiveInput pins that plan's listing branch got the
// same treatment as apply's - they are separate call sites for the same
// renderer, and only apply's was covered above.
func TestPlanListTasksMasksSensitiveInput(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, listMasksIdentityRecipe)

	stdout, _, exit := runPlan(t, path, "--secret_value=identityzzz", "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "identityzzz") {
		t.Errorf("plan listing leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "***") {
		t.Errorf("expected the mask placeholder; got:\n%s", stdout)
	}
}

// TestListTasksMasksSensitiveInputInWhen covers a task predicate that
// sigil-interpolates a secret: the rendered source is what the [skipped] line
// and the `when` field carry.
func TestListTasksMasksSensitiveInputInWhen(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: gated
      when: '"{{ .secret_value }}" == "will-not-match"'
      dokku_stub: { key: a }
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value=taskwhenzzz", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "taskwhenzzz") {
		t.Errorf("task when: leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"skipped":true`) || !strings.Contains(stdout, "***") {
		t.Errorf("expected a masked when on a skipped task; got:\n%s", stdout)
	}
}

// TestListTasksMasksSensitivePlayWhen covers the play_skipped event, whose
// play / when / reason fields are emitted by a different branch than the
// per-task ones.
func TestListTasksMasksSensitivePlayWhen(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- name: play {{ .secret_value }}
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  when: '"{{ .secret_value }}" == "will-not-match"'
  tasks:
    - dokku_stub: { key: a }
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value=playwhenzzz", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "playwhenzzz") {
		t.Errorf("play_skipped leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"play":"play ***"`) {
		t.Errorf("expected a masked play name; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"reason":"when: `) || !strings.Contains(stdout, "***") {
		t.Errorf("expected a masked reason; got:\n%s", stdout)
	}

	stdout, _, exit = runApply(t, path, "--secret_value=playwhenzzz", "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "playwhenzzz") {
		t.Errorf("human play skip line leaked the sensitive input; got:\n%s", stdout)
	}
}

// TestListTasksMasksSensitiveLoopItem covers both fields a loop expansion
// contributes: the `(item=<value>)` suffix on the name, and loop_item itself,
// which is the one listing field that is not a string.
func TestListTasksMasksSensitiveLoopItem(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      loop: 'split(secret_value, ",")'
      dokku_stub: { key: "{{ .item }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value=loopitemzzz", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "loopitemzzz") {
		t.Errorf("loop expansion leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"loop_item":"***"`) {
		t.Errorf("expected a masked loop_item; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"deploy (item=***)"`) {
		t.Errorf("expected a masked loop item in the name; got:\n%s", stdout)
	}
}

// TestListTasksMasksSensitiveTags covers the tags array, which is rendered
// from the same whole-file template pass as the rest of the recipe.
func TestListTasksMasksSensitiveTags(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: tagged
      tags: ["{{ .secret_value }}"]
      dokku_stub: { key: a }
`)

	stdout, _, exit := runApply(t, path, "--secret_value=tagsecretzzz", "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "tagsecretzzz") {
		t.Errorf("human listing leaked the sensitive tag; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[tags=***]") {
		t.Errorf("expected a masked tag suffix; got:\n%s", stdout)
	}

	stdout, _, exit = runApply(t, path, "--secret_value=tagsecretzzz", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "tagsecretzzz") {
		t.Errorf("--json listing leaked the sensitive tag; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"tags":["***"]`) {
		t.Errorf("expected a masked tags array; got:\n%s", stdout)
	}
}

// TestListTasksMasksTaskDeclaredSensitiveValue pins the other half of #455:
// the registry move in commands/apply.go. dokku_config declares its whole
// `config:` map sensitive through SensitiveValues(), and nothing here is a
// sensitive *input* - so the value reaches the mask registry only via
// CollectPlaySensitiveValues. Fails if that call moves back below the
// --list-tasks branch.
func TestListTasksMasksTaskDeclaredSensitiveValue(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: configure
      loop: [taskdeclaredzzz]
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
		t.Errorf("listing leaked a task-declared sensitive value; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"loop_item":"***"`) {
		t.Errorf("expected a masked loop_item; got:\n%s", stdout)
	}
}

// TestApplyStartAtTaskUnknownMasksTaskDeclaredSecret covers the second leak the
// registry move closes: the --start-at-task hint lists every available name
// and already calls MaskString, but before #455 the task-declared values were
// not registered yet when it ran.
func TestApplyStartAtTaskUnknownMasksTaskDeclaredSecret(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: configure taskdeclaredzzz
      dokku_config:
        app: api
        config:
          TOKEN: taskdeclaredzzz
`)

	_, stderr, exit := runApply(t, path, "--start-at-task", "nope")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stderr=%s", exit, stderr)
	}
	if strings.Contains(stderr, "taskdeclaredzzz") {
		t.Errorf("--start-at-task hint leaked a task-declared sensitive value; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, "***") {
		t.Errorf("expected the mask placeholder in the hint; got:\n%s", stderr)
	}
}

// TestListTasksMasksSensitiveInputInPlayWhenError covers the expr snippet: a
// runtime error from a predicate quotes the predicate's own source back in
// its `| <source>` frame, so the formatted error echoes the recipe line - the
// play name alone is not enough to mask.
func TestListTasksMasksSensitiveInputInPlayWhenError(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- name: broken
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  when: '[][0] == "{{ .secret_value }}"'
  tasks:
    - dokku_stub: { key: a }

- name: listed
  tasks:
    - name: still listed
      dokku_stub: { key: b }
`)

	stdout, _, exit := runApply(t, path, "--secret_value=whenerrzzz", "--list-tasks")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "whenerrzzz") {
		t.Errorf("the when-error snippet leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "(when error:") {
		t.Errorf("expected the when error form to survive masking; got:\n%s", stdout)
	}

	stdout, _, exit = runApply(t, path, "--secret_value=whenerrzzz", "--list-tasks", "--json")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s", exit, stdout)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "whenerrzzz") {
		t.Errorf("--json when-error reason leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"reason":"when error:`) {
		t.Errorf("expected the when error reason form; got:\n%s", stdout)
	}
}

// TestListTasksJSONMasksLoopItemStructure drives the recursive walker end to
// end: loop_item is the one listing field that is not a string, and a loop
// over mappings puts a secret one level down from where MaskString reaches.
func TestListTasksJSONMasksLoopItemStructure(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      loop:
        - { user: alice, token: "{{ .secret_value }}" }
      dokku_stub: { key: "{{ .item.user }}" }
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value=nestedzzz", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	if strings.Contains(stdout, "nestedzzz") {
		t.Errorf("a nested loop_item value leaked the sensitive input; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"token":"***"`) {
		t.Errorf("expected the nested token to be masked; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"user":"alice"`) {
		t.Errorf("masking must leave the rest of the mapping intact; got:\n%s", stdout)
	}
}

// TestListTasksDoesNotMaskStructuralDecorations pins the other half of the
// rule: docket's own vocabulary is never masked. `probe` and `phase` are
// schema-pinned enums, so masking one would emit a stream that fails
// docs/schemas/list-tasks-v1.schema.json - while `probe_caveat`, which is
// prose, is masked like everything else.
//
// The secret is chosen to be a substring of every decoration under test.
func TestListTasksDoesNotMaskStructuralDecorations(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: gate
      block:
        - name: unprobeable
          dokku_git_auth:
            host: github.com
            username: u
            password: p
`)

	stdout, stderr, exit := runApply(t, path, "--secret_value=netrc", "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)
	for _, want := range []string{`"group":true`, `"phase":"block"`, `"probe":"unsupported"`} {
		if !strings.Contains(stdout, want) {
			t.Errorf("masking must not touch %s; got:\n%s", want, stdout)
		}
	}
	if !strings.Contains(stdout, `"probe_caveat":"*** state has no read command`) {
		t.Errorf("expected the prose caveat to be masked; got:\n%s", stdout)
	}

	stdout, _, exit = runApply(t, path, "--secret_value=group", "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	for _, want := range []string{"(group)", "[block] ", "(never converges)"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("masking must not touch the %q decoration; got:\n%s", want, stdout)
		}
	}
}
