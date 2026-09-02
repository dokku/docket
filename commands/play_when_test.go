package commands

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// These tests pin the spec's "play `when:` sees the file-level merged
// context only" rule. The play's own `inputs:` are intentionally NOT
// visible to its own `when:` predicate (the issue body calls this
// circular). The tests drive the plan path because plan does not
// require a Dokku server, so the assertions stay deterministic in CI.
//
// Predicate negative tests use equality (`==`) rather than negation
// (`!=`) because expr is configured with AllowUndefinedVariables, which
// renders unknown identifiers as nil at runtime. With `==`, an unknown
// identifier produces `nil == "x"` (false → skipped), which is the
// outcome we want to assert. With `!=`, `nil != "x"` is true and the
// play would run regardless of whether the variable was visible.

func newTestPlanCommand(t *testing.T, argv []string) *PlanCommand {
	c := &PlanCommand{Argv: argv, Ctx: withStubFixtures(context.Background(), stubsFor(t))}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	return c
}

// playOutput drives plan with the given args against the tasks file at
// path and returns the captured stdout / stderr / exit code. It hands the
// command its own argv because PlanCommand.FlagSet() reads --tasks from argv
// (not from the args slice) so it can pre-register input flags before
// flag.Parse runs against the per-call args.
func playOutput(t *testing.T, path string, args ...string) (string, string, int) {
	t.Helper()
	argv := []string{"docket-test", "plan", "--tasks", path}

	c := newTestPlanCommand(t, argv)
	all := append([]string{"--tasks", path}, args...)
	exit := c.Run(all)
	ui := c.Ui.(*cli.MockUi)
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit
}

func writePlayTasks(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	return path
}

// TestPlayWhenSeesFileLevelInputDefault: the play's `when:` evaluates a
// file-level input default. The truthy default runs the play.
func TestPlayWhenSeesFileLevelInputDefault(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- inputs:
    - name: env
      default: prod
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	stdout, _, _ := playOutput(t, path)
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("expected api play header (truthy default); got:\n%s", stdout)
	}
	if strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf("api play should NOT be skipped when default makes when truthy; got:\n%s", stdout)
	}
}

// TestPlayWhenSeesFileLevelInputDefaultFalsy: file-level default does
// not satisfy the predicate; the play is skipped.
func TestPlayWhenSeesFileLevelInputDefaultFalsy(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- inputs:
    - name: env
      default: staging
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	stdout, _, _ := playOutput(t, path)
	if !strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf("expected api play to be skipped; got:\n%s", stdout)
	}
}

// TestPlayWhenCLIOverridesFileDefault: --env=prod beats the file-level
// staging default and the play runs.
func TestPlayWhenCLIOverridesFileDefault(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- inputs:
    - name: env
      default: staging
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	stdout, _, _ := playOutput(t, path, "--env", "prod")
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("expected api play header after --env=prod override; got:\n%s", stdout)
	}
	if strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf("play should NOT be skipped when CLI override makes when truthy; got:\n%s", stdout)
	}
}

// TestPlayWhenVarsFileOverridesFileDefault: --vars-file value beats the
// file-level default.
func TestPlayWhenVarsFileOverridesFileDefault(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- inputs:
    - name: env
      default: staging
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	dir := filepath.Dir(path)
	varsPath := filepath.Join(dir, "vars.yml")
	if err := os.WriteFile(varsPath, []byte("env: prod\n"), 0o644); err != nil {
		t.Fatalf("write vars.yml: %v", err)
	}
	stdout, _, _ := playOutput(t, path, "--vars-file", varsPath)
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("expected api play header after vars-file override; got:\n%s", stdout)
	}
	if strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf("play should NOT be skipped after vars-file override; got:\n%s", stdout)
	}
}

// TestPlayWhenCLIBeatsVarsFile: vars-file says staging, CLI says prod;
// the play runs (CLI wins).
func TestPlayWhenCLIBeatsVarsFile(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- inputs:
    - name: env
      default: dev
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	dir := filepath.Dir(path)
	varsPath := filepath.Join(dir, "vars.yml")
	if err := os.WriteFile(varsPath, []byte("env: staging\n"), 0o644); err != nil {
		t.Fatalf("write vars.yml: %v", err)
	}
	stdout, _, _ := playOutput(t, path, "--vars-file", varsPath, "--env", "prod")
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("expected api play header (CLI must beat vars-file); got:\n%s", stdout)
	}
	if strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf("play should NOT be skipped when CLI beats vars-file; got:\n%s", stdout)
	}
}

// TestPlayWhenCannotSeeOwnInputs: a play declares `enabled` and asks
// `when: 'enabled == "true"'`. The play's own inputs are NOT in the
// when context, so `enabled` resolves to nil; `nil == "true"` is false
// and the play is skipped. The play would run only if its own input
// leaked into its own when context.
func TestPlayWhenCannotSeeOwnInputs(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- name: api
  inputs:
    - name: enabled
      default: "true"
  when: 'enabled == "true"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	stdout, _, _ := playOutput(t, path)
	if !strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf("api play should be skipped (own input must not be visible to own when); got:\n%s", stdout)
	}
}

// TestPlayWhenCannotSeeSiblingPlayInputs: play `worker` references the
// sibling play `api`'s input. Sibling-play inputs must not be visible,
// so the comparison resolves to nil == "api" → false and worker is
// skipped.
func TestPlayWhenCannotSeeSiblingPlayInputs(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- name: api
  inputs:
    - name: app
      default: api
  tasks:
    - name: api-noop
      dokku_app:
        app: docket-test-noop
- name: worker
  when: 'app == "api"'
  tasks:
    - name: worker-noop
      dokku_app:
        app: docket-test-noop
`)
	stdout, _, _ := playOutput(t, path)
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("api play header missing; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `Play: worker  (skipped`) {
		t.Errorf("worker play should be skipped (sibling input must not leak); got:\n%s", stdout)
	}
}

// playLines returns the `==> Play:` lines of out, in order. Task lines
// are dropped because the two renderers describe tasks differently by
// design (`[0] <name>` under --list-tasks, `[ok]` / `[changed]` under
// plan); only play selection is being compared.
func playLines(out string) []string {
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if strings.HasPrefix(line, "==> Play:") {
			lines = append(lines, line)
		}
	}
	return lines
}

// TestPlayWhenListTasksMatchesPlan pins that `plan` and
// `plan --list-tasks` reach the same play-selection verdict. The two
// resolve play `when:` in separate code paths - the plan loop in
// plan.go against Formatter.PlaySkipped, and renderListTasks against
// its own format strings in list_tasks.go - which agree today only by
// convention. tests/bats/plays.bats asserts play scoping through
// --list-tasks (it is the offline surface, #412), so a divergence there
// would silently stop covering what plan actually does.
//
// Task bodies are dokku_stub, whose Plan() reads a fixture instead of
// shelling out, so the plan side runs a play to completion without a
// server. The recipe carries no sensitive values: the plan side masks
// play names and --list-tasks does not, so a sensitive name would make
// the two differ by design rather than by drift.
func TestPlayWhenListTasksMatchesPlan(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- inputs:
    - name: env
      default: prod
- name: api
  inputs:
    - name: app
      default: api
  when: 'env == "prod"'
  tasks:
    - dokku_stub: { key: a }
- name: worker
  when: 'env == "staging"'
  tasks:
    - dokku_stub: { key: b }
- name: sibling
  when: 'app == "api"'
  tasks:
    - dokku_stub: { key: c }
`)

	planOut, planErr, planExit := playOutput(t, path)
	if planExit != 0 {
		t.Fatalf("plan exit = %d, want 0; stdout=%s stderr=%s", planExit, planOut, planErr)
	}
	listOut, listErr, listExit := playOutput(t, path, "--list-tasks")
	if listExit != 0 {
		t.Fatalf("plan --list-tasks exit = %d, want 0; stdout=%s stderr=%s", listExit, listOut, listErr)
	}

	got := playLines(listOut)
	want := playLines(planOut)
	if len(want) != 3 {
		t.Fatalf("expected 3 play lines from plan, got %d:\n%s", len(want), planOut)
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Errorf("--list-tasks and plan disagree on play selection\nplan:\n%s\n--list-tasks:\n%s",
			strings.Join(want, "\n"), strings.Join(got, "\n"))
	}

	// Guard the comparison itself: two empty line sets would compare
	// equal, so assert the three verdicts the recipe was built to
	// produce actually appear.
	if !strings.Contains(listOut, "==> Play: api") || strings.Contains(listOut, "==> Play: api  (skipped") {
		t.Errorf("api should run (file-level default satisfies when:); got:\n%s", listOut)
	}
	if !strings.Contains(listOut, `==> Play: worker  (skipped: when "env == \"staging\"")`) {
		t.Errorf("worker should be skipped by its false predicate; got:\n%s", listOut)
	}
	if !strings.Contains(listOut, `==> Play: sibling  (skipped: when "app == \"api\"")`) {
		t.Errorf("sibling should be skipped (api's play-local input must not leak); got:\n%s", listOut)
	}
}

// TestPlayWhenCannotSeeLoopVars: `.item` is a loop-iteration value;
// it must not be available to a play-level `when:`. With `==` the
// undefined identifier resolves to nil → comparison false → skipped.
func TestPlayWhenCannotSeeLoopVars(t *testing.T) {
	t.Parallel()
	path := writePlayTasks(t, `---
- name: api
  when: 'item == "expected"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop
`)
	stdout, _, _ := playOutput(t, path)
	if !strings.Contains(stdout, `Play: api  (skipped`) {
		t.Errorf(".item should not be visible to play when (expected skip); got:\n%s", stdout)
	}
}
