package commands

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// parseErrorRecipe interpolates a sensitive input into an envelope key, which
// renders to an unknown key and fails during GetPlaysWithFormat. The parse
// error quotes the rendered (secret) key, so it must be masked - which only
// works if the sensitive CLI value is registered before the recipe is parsed.
const parseErrorRecipe = `---
- inputs:
    - { name: secret_value, required: true, sensitive: true }
  tasks:
    - name: deploy
      "{{ .secret_value }}": true
      dokku_app: { app: x }
`

func TestApplyMasksSensitiveInputInParseError(t *testing.T) {
	path := writeTasksFile(t, parseErrorRecipe)
	_, stderr, exit := runApply(t, path, "--secret_value=envelopekey_secretzzz")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for a parse error; stderr=%s", stderr)
	}
	if strings.Contains(stderr, "envelopekey_secretzzz") {
		t.Errorf("parse error leaked the sensitive input: %s", stderr)
	}
	if !strings.Contains(stderr, "***") {
		t.Errorf("expected mask placeholder in parse error, got: %s", stderr)
	}
}

func TestPlanMasksSensitiveInputInParseError(t *testing.T) {
	path := writeTasksFile(t, parseErrorRecipe)
	_, stderr, exit := runPlan(t, path, "--secret_value=envelopekey_secretzzz")
	if exit == 0 {
		t.Fatalf("expected non-zero exit for a parse error; stderr=%s", stderr)
	}
	if strings.Contains(stderr, "envelopekey_secretzzz") {
		t.Errorf("parse error leaked the sensitive input: %s", stderr)
	}
	if !strings.Contains(stderr, "***") {
		t.Errorf("expected mask placeholder in parse error, got: %s", stderr)
	}
}

func TestValidateMasksSensitiveInJSONProblem(t *testing.T) {
	subprocess.SetGlobalSensitive([]string{"tok_secret"})
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	ui := cli.NewMockUi()
	c := &ValidateCommand{Meta: command.Meta{Ui: ui}}
	c.emitJSONProblem(tasks.Problem{
		Code:    "template_error",
		Message: `cannot render "tok_secret"`,
		Play:    "play tok_secret",
		Task:    "task tok_secret",
		Hint:    "check tok_secret",
	})
	out := ui.OutputWriter.String()
	if strings.Contains(out, "tok_secret") {
		t.Errorf("validate JSON problem leaked secret: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("expected mask placeholder, got: %s", out)
	}
}

func TestValidateMasksSensitiveInHumanProblem(t *testing.T) {
	subprocess.SetGlobalSensitive([]string{"tok_secret"})
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	ui := cli.NewMockUi()
	c := &ValidateCommand{Meta: command.Meta{Ui: ui}}
	c.renderHumanProblems([]tasks.Problem{{
		Play:    "play tok_secret",
		Task:    "task tok_secret",
		Message: `bad value "tok_secret"`,
	}})
	out := ui.OutputWriter.String()
	if strings.Contains(out, "tok_secret") {
		t.Errorf("validate human problem leaked secret: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("expected mask placeholder, got: %s", out)
	}
}
