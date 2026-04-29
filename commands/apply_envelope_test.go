package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"
	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// runApply drives an ApplyCommand against the tasks file at path,
// returning stdout / stderr / exit code. It mirrors playOutput
// (commands/play_when_test.go) but for the apply path: the apply
// command's FlagSet reads --tasks from os.Args, so we stage that here
// before invoking Run.
func runApply(t *testing.T, path string, args ...string) (string, string, int) {
	t.Helper()
	origArgs := os.Args
	os.Args = []string{"docket-test", "apply", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	c := &ApplyCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	all := append([]string{"--tasks", path}, args...)
	exit := c.Run(all)
	ui := c.Ui.(*cli.MockUi)
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit
}

func writeTasksFile(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	return path
}

// TestApplyRegisterMakesPriorResultAvailable: a task that registers as
// `first` is followed by a task whose `when:` references
// `registered.first.Changed`. The follow-up should run when first
// changed and skip otherwise.
func TestApplyRegisterMakesPriorResultAvailable(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: false})

	path := writeTasksFile(t, `---
- tasks:
    - name: first
      register: first
      dokku_stub: { key: a }
    - name: follow-up
      when: 'registered.first.Changed'
      dokku_stub: { key: b }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "follow-up") {
		t.Errorf("follow-up should run when first.Changed is true; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[skipped] follow-up") {
		t.Errorf("follow-up should not be skipped; got:\n%s", stdout)
	}
}

// TestApplyRegisterFalseSkipsFollowUp: when the registered task did
// not change, the follow-up's when: predicate is falsy and the task
// renders as [skipped].
func TestApplyRegisterFalseSkipsFollowUp(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})
	stubSet("b", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: first
      register: first
      dokku_stub: { key: a }
    - name: follow-up
      when: 'registered.first.Changed'
      dokku_stub: { key: b }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[skipped]") {
		t.Errorf("follow-up should be skipped; got:\n%s", stdout)
	}
}

// TestApplyChangedWhenFalseFlipsToOK: changed_when: false flips a
// self-reported-changed task to [ok].
func TestApplyChangedWhenFalseFlipsToOK(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: stamp
      changed_when: 'false'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[ok]") {
		t.Errorf("expected [ok] marker; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[changed] stamp") {
		t.Errorf("changed_when:false should suppress [changed]; got:\n%s", stdout)
	}
}

// TestApplyChangedWhenTrueFlipsToChanged: changed_when: true converts
// an in-sync task to [changed].
func TestApplyChangedWhenTrueFlipsToChanged(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})

	path := writeTasksFile(t, `---
- tasks:
    - name: stamp
      changed_when: 'true'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[changed]") {
		t.Errorf("expected [changed] marker; got:\n%s", stdout)
	}
}

// TestApplyFailedWhenSuppressesExpectedError: failed_when matching the
// stderr pattern clears the error, exit is 0.
func TestApplyFailedWhenSuppressesExpectedError(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{
		ExecuteError: stubExecError("App nonexistent does not exist"),
		Stderr:       "App nonexistent does not exist",
	})

	path := writeTasksFile(t, `---
- tasks:
    - name: tolerant
      failed_when: 'result.Error != nil and not (result.Stderr contains "does not exist")'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (failed_when should clear the error); stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "[error]") {
		t.Errorf("error should be suppressed; got:\n%s", stdout)
	}
}

// TestApplyFailedWhenTrueMarksSuccessAsError: failed_when: true on a
// successful task flips it to [error] and the run aborts (exit 1).
func TestApplyFailedWhenTrueMarksSuccessAsError(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})

	path := writeTasksFile(t, `---
- tasks:
    - name: pessimist
      failed_when: 'true'
      dokku_stub: { key: a }
`)
	_, stderr, exit := runApply(t, path)
	if exit == 0 {
		t.Fatalf("exit = 0, want non-zero (failed_when:true should fail the run)")
	}
	if !strings.Contains(stderr, "[error]") {
		t.Errorf("expected [error] marker in stderr; got:\n%s", stderr)
	}
}

// TestApplyFailedWhenFalseClearsStateMismatch: a falsy failed_when
// also normalizes State to DesiredState so the state-mismatch branch
// does not re-flag the task.
func TestApplyFailedWhenFalseClearsStateMismatch(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{MismatchState: true, Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: forgiving
      failed_when: 'false'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "[error]") {
		t.Errorf("state mismatch should be cleared by falsy failed_when; got:\n%s", stdout)
	}
}

// TestApplyIgnoreErrorsContinuesPastFailure: ignore_errors:true keeps
// the run going past an erroring task. The error event is still
// emitted but the second task runs and the exit code is 0.
func TestApplyIgnoreErrorsContinuesPastFailure(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{ExecuteError: errors.New("boom")})
	stubSet("b", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: this errors
      ignore_errors: true
      dokku_stub: { key: a }
    - name: this still runs
      dokku_stub: { key: b }
`)
	stdout, stderr, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (ignore_errors should suppress fatal exit); stderr=%s", exit, stderr)
	}
	if !strings.Contains(stdout, "this still runs") {
		t.Errorf("second task should run; stdout=\n%s", stdout)
	}
	if !strings.Contains(stderr+stdout, "[error]") {
		t.Errorf("error event should still emit; stdout=%s stderr=%s", stdout, stderr)
	}
}

// TestApplyIgnoreErrorsNoOpOnSuccess: ignore_errors:true on a
// successful task is a no-op; the task still renders as [ok] /
// [changed] and the run exits 0.
func TestApplyIgnoreErrorsNoOpOnSuccess(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: harmless
      ignore_errors: true
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[changed]") {
		t.Errorf("expected [changed]; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[error]") {
		t.Errorf("expected no [error]; got:\n%s", stdout)
	}
}

// TestApplyLoopRegisterAccumulatesResults: a loop+register task
// accumulates per-iteration states into Results, and the aggregate
// Changed reflects "any iteration changed."
func TestApplyLoopRegisterAccumulatesResults(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})
	stubSet("b", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: each
      loop: [a, b]
      register: each
      dokku_stub:
        key: "{{ .item }}"
    - name: any-changed
      when: 'registered.each.Changed'
      dokku_stub: { key: a }
    - name: first-iter
      when: 'registered.each.Results[0].Changed'
      dokku_stub: { key: a }
    - name: second-iter
      when: 'registered.each.Results[1].Changed'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	// Aggregate Changed should be true (b changed) so any-changed runs.
	if !strings.Contains(stdout, "any-changed") || strings.Contains(stdout, "[skipped] any-changed") {
		t.Errorf("any-changed should run because aggregate Changed is true; got:\n%s", stdout)
	}
	// First iteration did NOT change, so first-iter is skipped.
	if !strings.Contains(stdout, "[skipped]") {
		t.Errorf("first-iter should be skipped; got:\n%s", stdout)
	}
	// Second iteration changed, so second-iter runs.
	if !strings.Contains(stdout, "second-iter") {
		t.Errorf("second-iter should run; got:\n%s", stdout)
	}
}

// TestApplyEnvelopeOverridesUnitFailedWhen pins applyEnvelopeOverrides
// behavior in isolation so the predicate phase ordering and Ansible
// failure-verdict semantics are anchored without spinning up Run().
func TestApplyEnvelopeOverridesUnitFailedWhen(t *testing.T) {
	env := &tasks.TaskEnvelope{
		Name:       "x",
		FailedWhen: "false",
	}
	if _, err := compileEnvelope(env); err != nil {
		t.Fatalf("compile: %v", err)
	}
	state := tasks.TaskOutputState{
		Error:        errors.New("boom"),
		State:        tasks.StateAbsent,
		DesiredState: tasks.StatePresent,
	}
	got, err := applyEnvelopeOverrides(env, state, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("applyEnvelopeOverrides: %v", err)
	}
	if got.Error != nil {
		t.Errorf("falsy failed_when should clear Error; got %v", got.Error)
	}
	if got.State != got.DesiredState {
		t.Errorf("falsy failed_when should normalize State to DesiredState; got %v vs %v", got.State, got.DesiredState)
	}
}

func TestApplyEnvelopeOverridesUnitChangedWhen(t *testing.T) {
	env := &tasks.TaskEnvelope{
		Name:        "x",
		ChangedWhen: "false",
	}
	if _, err := compileEnvelope(env); err != nil {
		t.Fatalf("compile: %v", err)
	}
	state := tasks.TaskOutputState{Changed: true}
	got, err := applyEnvelopeOverrides(env, state, map[string]interface{}{}, nil)
	if err != nil {
		t.Fatalf("applyEnvelopeOverrides: %v", err)
	}
	if got.Changed {
		t.Errorf("falsy changed_when should clear Changed; got true")
	}
}

// compileEnvelope is a tiny test helper that pre-compiles the
// envelope's predicate sources via the public CompilePredicate API and
// stores the programs back through reflection-free package-private
// fields. The override evaluator only reads from the program getters,
// so we need the compiled program in place before calling it.
func compileEnvelope(env *tasks.TaskEnvelope) (*tasks.TaskEnvelope, error) {
	// CompilePredicate caches by source, and TaskEnvelope.{Changed,Failed}WhenProgram
	// look up those programs through the unexported fields. Reach them
	// indirectly by re-parsing through GetTasks: write a minimal recipe
	// that carries the predicates and let the loader populate them.
	data := []byte("---\n- tasks:\n    - name: " + env.Name + "\n      dokku_stub: { key: ignored }\n")
	if env.FailedWhen != "" {
		data = []byte("---\n- tasks:\n    - name: " + env.Name + "\n      failed_when: " + quoted(env.FailedWhen) + "\n      dokku_stub: { key: ignored }\n")
	}
	if env.ChangedWhen != "" {
		data = []byte("---\n- tasks:\n    - name: " + env.Name + "\n      changed_when: " + quoted(env.ChangedWhen) + "\n      dokku_stub: { key: ignored }\n")
	}
	envelopes, err := tasks.GetTasks(data, nil)
	if err != nil {
		return nil, err
	}
	loaded := envelopes.GetEnvelope(env.Name)
	if loaded == nil {
		return nil, errors.New("loaded envelope not found")
	}
	*env = *loaded
	return env, nil
}

func quoted(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "''") + "'"
}
