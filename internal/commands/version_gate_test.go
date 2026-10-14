package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

func TestPlanRefusesOldDokku(t *testing.T) {
	t.Parallel()
	stdout, stderr, exit, seen := runGatedCommand(t, "plan", appRecipe, versionAnswers("0.38.8\n", ""))
	if exit == 0 {
		t.Fatalf("exit = 0, want an error; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "dokku_app requires Dokku >= 0.38.31, server has 0.38.8 (pass --ignore-version to run it anyway)") {
		t.Fatalf("output missing the refusal:\n%s\n%s", stdout, stderr)
	}
	if containsArg(seen, "apps:exists") {
		t.Fatalf("plan ran the task after refusing it: %v", seen)
	}
}

func TestPlanIgnoreVersionRunsTheTask(t *testing.T) {
	t.Parallel()
	_, _, exit, seen := runGatedCommand(t, "plan", appRecipe, versionAnswers("0.38.8\n", ""), "--ignore-version")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !containsArg(seen, "apps:exists") {
		t.Fatalf("--ignore-version should plan the task; saw %v", seen)
	}
}

func TestApplyRefusesOldDokku(t *testing.T) {
	t.Parallel()
	stdout, stderr, exit, seen := runGatedCommand(t, "apply", appRecipe, versionAnswers("0.38.8\n", ""))
	if exit == 0 {
		t.Fatalf("exit = 0, want an error; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "requires Dokku >= 0.38.31") {
		t.Fatalf("output missing the refusal:\n%s\n%s", stdout, stderr)
	}
	if containsArg(seen, "apps:") {
		t.Fatalf("apply ran the task after refusing it: %v", seen)
	}
}

func TestApplyIgnoreVersionRunsTheTask(t *testing.T) {
	t.Parallel()
	_, _, exit, seen := runGatedCommand(t, "apply", appRecipe, versionAnswers("0.38.8\n", ""), "--ignore-version")
	if exit != 0 {
		t.Fatalf("exit = %d", exit)
	}
	if !containsArg(seen, "apps:exists") {
		t.Fatalf("--ignore-version should run the task; saw %v", seen)
	}
}

func TestApplyFailedWhenFalseClearsVersionErrorWithoutRunning(t *testing.T) {
	t.Parallel()
	recipe := `---
- tasks:
    - name: app
      failed_when: 'false'
      dokku_app: { app: a }
`
	_, _, exit, seen := runGatedCommand(t, "apply", recipe, versionAnswers("0.38.8\n", ""))
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if containsArg(seen, "apps:") {
		t.Fatalf("failed_when: false should clear the error without running the task; saw %v", seen)
	}
}

func TestApplyIgnoreErrorsSwallowsVersionErrorWithoutRunning(t *testing.T) {
	t.Parallel()
	recipe := `---
- tasks:
    - name: app
      ignore_errors: true
      dokku_app: { app: a }
`
	_, _, exit, seen := runGatedCommand(t, "apply", recipe, versionAnswers("0.38.8\n", ""))
	if exit != 0 {
		t.Fatalf("exit = %d, want 0", exit)
	}
	if containsArg(seen, "apps:") {
		t.Fatalf("ignore_errors should swallow the refusal without running the task; saw %v", seen)
	}
}

func TestPlanRefusesOldPluginAndAllowsAMissingOne(t *testing.T) {
	t.Parallel()
	recipe := `---
- tasks:
    - name: cert
      dokku_letsencrypt: { app: a }
`
	stdout, stderr, exit, seen := runGatedCommand(t, "plan", recipe, versionAnswers("0.38.31\n", `[{"name":"letsencrypt","version":"0.1.0"}]`))
	if exit == 0 {
		t.Fatalf("exit = 0; stdout:\n%s\nstderr:\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout+stderr, "dokku_letsencrypt requires letsencrypt >= 0.25.2, server has 0.1.0") {
		t.Fatalf("output missing the plugin refusal:\n%s\n%s", stdout, stderr)
	}
	if containsArg(seen, "letsencrypt:active") {
		t.Fatalf("plan ran the task against an old plugin: %v", seen)
	}

	_, _, _, seen = runGatedCommand(t, "plan", recipe, versionAnswers("0.38.31\n", `[]`))
	if !containsArg(seen, "letsencrypt:active") {
		t.Fatalf("a missing plugin should reach the task's own probe; saw %v", seen)
	}
}

func TestExportSkipsOldDokkuUnlessIgnored(t *testing.T) {
	t.Parallel()
	old := exportCommandFixture()
	old["version"] = "0.38.0\n"
	dir := t.TempDir()
	recipe := filepath.Join(dir, "tasks.yml")
	c, ui := exportWithRunner(fakeExecRunner(old), dir)
	if code := c.Run([]string{"--output", recipe}); code == 0 {
		t.Fatalf("exit = 0, want the empty-export error; stderr:\n%s", ui.ErrorWriter.String())
	}
	stderr := ui.ErrorWriter.String()
	if !strings.Contains(stderr, "web: dokku_config: dokku_config requires Dokku >= 0.38.31") {
		t.Fatalf("stderr = %s", stderr)
	}

	dir = t.TempDir()
	recipe = filepath.Join(dir, "tasks.yml")
	c, ui = exportWithRunner(fakeExecRunner(exportCommandFixture()), dir)
	if code := c.Run([]string{"--ignore-version", "--output", recipe}); code != 0 {
		t.Fatalf("Run exit = %d, want 0: %s", code, ui.ErrorWriter.String())
	}
	if strings.Contains(ui.ErrorWriter.String(), "requires Dokku") {
		t.Fatalf("--ignore-version should not refuse: %s", ui.ErrorWriter.String())
	}
	body, err := os.ReadFile(recipe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), "dokku_config") {
		t.Fatalf("recipe:\n%s", body)
	}
}

const appRecipe = `---
- tasks:
    - name: app
      dokku_app: { app: a }
`

func runGatedCommand(t *testing.T, kind, recipe string, runner subprocess.ExecRunner, args ...string) (string, string, int, []string) {
	t.Helper()
	path := writeTasksFile(t, recipe)
	var seen []string
	ctx := subprocess.ContextWithRunner(context.Background(), func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		seen = append(seen, strings.Join(in.Args, " "))
		return runner(ctx, in)
	})
	ui := cli.NewMockUi()
	argv := append([]string{"docket-test", kind, "--tasks", path}, args...)
	runArgs := append([]string{"--tasks", path}, args...)
	var exit int
	switch kind {
	case "plan":
		c := &PlanCommand{Meta: command.Meta{Ui: ui}, Argv: argv, Ctx: ctx}
		exit = c.Run(runArgs)
	case "apply":
		c := &ApplyCommand{Meta: command.Meta{Ui: ui}, Argv: argv, Ctx: ctx}
		exit = c.Run(runArgs)
	default:
		t.Fatalf("unknown command %q", kind)
	}
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit, seen
}

func versionAnswers(dokku, plugins string) subprocess.ExecRunner {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "version":
			return subprocess.ExecCommandResponse{Stdout: dokku}, nil
		case "--quiet plugin:list --format json":
			return subprocess.ExecCommandResponse{Stdout: plugins}, nil
		default:
			return subprocess.ExecCommandResponse{}, nil
		}
	}
}

func containsArg(seen []string, fragment string) bool {
	for _, args := range seen {
		if strings.Contains(args, fragment) {
			return true
		}
	}
	return false
}
