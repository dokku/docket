package commands

import (
	"strings"
	"testing"
)

// play_name_masking_test.go covers #477: the "available plays" hint for an
// unmatched --play was the one diagnostic on the apply and plan paths that
// never masked. Unlike #473 and #475 the value is not transformed on its way
// into the message - it is simply printed in the clear - so no registered
// spelling reaches it and the hint has to mask each play name itself.
//
// Two leaks are covered. A sensitive *input* interpolated into a play name is
// registered well before the filter runs, so masking the names is enough. A
// *task-declared* value is not: it joins the registry only after the filter
// narrows the play list, which is why the error path registers it from the
// unfiltered list before the message renders.

// playNameRecipe interpolates a sensitive input into the first play's name and
// keeps a second, unrelated play so the hint has more than one name to list.
const playNameRecipe = `---
- name: "play-{{ .secret_value }}"
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_app: { app: api }
- name: keepzzz
  tasks:
    - dokku_app: { app: worker }
`

// playNameSecret is the flag value the input cases pass. The `zzz` suffix
// keeps it from colliding with anything docket prints of its own.
const playNameSecret = "playleakzzz"

func TestApplyPlayHintMasksSensitiveInputInPlayName(t *testing.T) {
	path := writeTasksFile(t, playNameRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+playNameSecret, "--play", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stderr, playNameSecret) {
		t.Errorf("the --play hint leaked the sensitive input; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"play-***"`) {
		t.Errorf("expected a masked play name in the hint; got:\n%s", stderr)
	}
}

func TestPlanPlayHintMasksSensitiveInputInPlayName(t *testing.T) {
	path := writeTasksFile(t, playNameRecipe)

	stdout, stderr, exit := runPlan(t, path, "--secret_value="+playNameSecret, "--play", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stderr, playNameSecret) {
		t.Errorf("the plan --play hint leaked the sensitive input; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"play-***"`) {
		t.Errorf("expected a masked play name in the hint; got:\n%s", stderr)
	}
}

// TestPlayHintLeavesNonSensitivePlayNamesAlone is the boundary: the second
// play in the recipe carries no secret and must still print in full, so the
// fix cannot be passed by masking the whole message.
func TestPlayHintLeavesNonSensitivePlayNamesAlone(t *testing.T) {
	path := writeTasksFile(t, playNameRecipe)

	stdout, stderr, exit := runApply(t, path, "--secret_value="+playNameSecret, "--play", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, `"keepzzz"`) {
		t.Errorf("expected the non-sensitive play name to print in full; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `--play "nozzz"`) {
		t.Errorf("expected the unmatched target quoted back; got:\n%s", stderr)
	}
}

// TestPlayHintMasksQuoteBearingSensitivePlayName pins the quoting order. The
// hint renders each name through `%q`, so a secret carrying a double quote
// reaches the message escaped; masking each name before quoting it means the
// hint never depends on the escaped spelling being registered too.
func TestPlayHintMasksQuoteBearingSensitivePlayName(t *testing.T) {
	path := writeTasksFile(t, `---
- name: "play-{{ .secret_value | dq }}"
  inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - dokku_app: { app: api }
`)

	stdout, stderr, exit := runApply(t, path, `--secret_value=playquotedzzz"x`, "--play", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stderr, "playquotedzzz") {
		t.Errorf("the --play hint leaked the quote-bearing sensitive input; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"play-***"`) {
		t.Errorf("expected a masked play name in the hint; got:\n%s", stderr)
	}
}

// TestApplyPlayHintMasksTaskDeclaredSecretInPlayName is the ordering half of
// the fix. Nothing here is a sensitive input: dokku_config declares its whole
// `config:` map sensitive, so the value joins the registry through the task
// walk - which the filter used to run ahead of. The sibling of
// TestApplyStartAtTaskUnknownMasksTaskDeclaredSecret, which is the same leak
// on the --start-at-task hint (#455).
func TestApplyPlayHintMasksTaskDeclaredSecretInPlayName(t *testing.T) {
	path := writeTasksFile(t, `---
- name: play-taskdeclaredzzz
  tasks:
    - name: configure api
      dokku_config:
        app: api
        config:
          TOKEN: taskdeclaredzzz
`)

	stdout, stderr, exit := runApply(t, path, "--play", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stderr, "taskdeclaredzzz") {
		t.Errorf("the --play hint leaked a task-declared sensitive value; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"play-***"`) {
		t.Errorf("expected a masked play name in the hint; got:\n%s", stderr)
	}
}

func TestPlanPlayHintMasksTaskDeclaredSecretInPlayName(t *testing.T) {
	path := writeTasksFile(t, `---
- name: play-taskdeclaredzzz
  tasks:
    - name: configure api
      dokku_config:
        app: api
        config:
          TOKEN: taskdeclaredzzz
`)

	stdout, stderr, exit := runPlan(t, path, "--play", "nozzz")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stderr, "taskdeclaredzzz") {
		t.Errorf("the plan --play hint leaked a task-declared sensitive value; got:\n%s", stderr)
	}
	if !strings.Contains(stderr, `"play-***"`) {
		t.Errorf("expected a masked play name in the hint; got:\n%s", stderr)
	}
}
