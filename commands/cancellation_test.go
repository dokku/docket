package commands

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// runWithCtx drives apply or plan under a caller-supplied context, the way
// main.go does with the process signal context. The plain runApply / runPlan
// helpers leave Ctx nil, which is the "constructed directly" path.
func runWithCtx(t *testing.T, ctx context.Context, sub, path string, args ...string) (string, string, int) {
	t.Helper()
	origArgs := os.Args
	os.Args = []string{"docket-test", sub, "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	ui := cli.NewMockUi()
	all := append([]string{"--tasks", path}, args...)
	var exit int
	switch sub {
	case "apply":
		c := &ApplyCommand{Meta: command.Meta{Ui: ui}, Ctx: ctx}
		exit = c.Run(all)
	case "plan":
		c := &PlanCommand{Meta: command.Meta{Ui: ui}, Ctx: ctx}
		exit = c.Run(all)
	default:
		t.Fatalf("unknown subcommand %q", sub)
	}
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit
}

const twoTaskRecipe = `---
- tasks:
    - name: first
      dokku_stub: { key: first }
    - name: second
      dokku_stub: { key: second }
`

// TestApplyStopsAtCancelledContext pins that cancelling the run context stops
// the run at the next task rather than letting every remaining task fail one
// at a time on a dead context. Before the context reached a task at all, an
// interrupt could only kill the child process of whichever task was in flight
// and the loop marched on to the next one.
func TestApplyStopsAtCancelledContext(t *testing.T) {
	defer stubReset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondRan := false
	stubSet("first", StubFixture{Changed: true, Hook: cancel})
	stubSet("second", StubFixture{Changed: true, Hook: func() { secondRan = true }})

	path := writeTasksFile(t, twoTaskRecipe)
	stdout, stderr, exit := runWithCtx(t, ctx, "apply", path)

	if secondRan {
		t.Error("the task after the cancellation must not run")
	}
	if exit != 1 {
		t.Errorf("exit = %d, want 1 for a cancelled run", exit)
	}
	if !strings.Contains(stderr, "run cancelled") {
		t.Errorf("stderr should say the run was cancelled; got %q", stderr)
	}
	if !strings.Contains(stdout, "first") {
		t.Errorf("the task that did run should still be reported; got %q", stdout)
	}
}

// TestPlanStopsAtCancelledContext is the plan-side mirror. plan probes the
// server for every task, so it needs the same escape hatch as apply.
func TestPlanStopsAtCancelledContext(t *testing.T) {
	defer stubReset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondRan := false
	stubSet("first", StubFixture{Changed: true, Hook: cancel})
	stubSet("second", StubFixture{Changed: true, Hook: func() { secondRan = true }})

	path := writeTasksFile(t, twoTaskRecipe)
	_, stderr, exit := runWithCtx(t, ctx, "plan", path)

	if secondRan {
		t.Error("the task after the cancellation must not be planned")
	}
	if exit != 1 {
		t.Errorf("exit = %d, want 1 for a cancelled run", exit)
	}
	if !strings.Contains(stderr, "run cancelled") {
		t.Errorf("stderr should say the run was cancelled; got %q", stderr)
	}
}

// TestApplyCancelledRunNeverReportsChanged pins that a cancelled run does not
// exit 2 under --detailed-exitcode even though a task changed before the
// interrupt. A wrapper reads 2 as "the run completed and changed something";
// an interrupted run completed nothing.
func TestApplyCancelledRunNeverReportsChanged(t *testing.T) {
	defer stubReset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	stubSet("first", StubFixture{Changed: true, Hook: cancel})
	stubSet("second", StubFixture{Changed: true})

	path := writeTasksFile(t, twoTaskRecipe)
	if _, _, exit := runWithCtx(t, ctx, "apply", path, "--detailed-exitcode"); exit != 1 {
		t.Errorf("exit = %d, want 1; a cancelled run must not report 2 (changed)", exit)
	}
}

// TestApplyReportsCancellationWhenTheInterruptedTaskAlsoFails is the case the
// obvious implementation gets wrong. An interrupt that lands while a task is
// talking to the server also makes that task fail, so the run loop leaves
// through the error path and never reaches its own cancellation check. Asking
// about the context once at the exit is what keeps the report honest.
func TestApplyReportsCancellationWhenTheInterruptedTaskAlsoFails(t *testing.T) {
	defer stubReset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondRan := false
	stubSet("first", StubFixture{
		ExecuteError: errors.New("interrupted"),
		Hook:         cancel,
	})
	stubSet("second", StubFixture{Changed: true, Hook: func() { secondRan = true }})

	path := writeTasksFile(t, twoTaskRecipe)
	_, stderr, exit := runWithCtx(t, ctx, "apply", path)

	if secondRan {
		t.Error("the task after the cancellation must not run")
	}
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "run cancelled") {
		t.Errorf("an interrupt that also failed the task must still be reported as a cancellation; stderr: %q", stderr)
	}
}

// TestApplyCancellationStopsLaterPlays pins the run-level abort. A task error
// already ends the play it happened in, but the next play still runs - so a
// recipe whose first play is interrupted would go on to start the second and
// need interrupting again. This is the behaviour the bats interrupt test
// exercises end to end against a host that never answers.
func TestApplyCancellationStopsLaterPlays(t *testing.T) {
	defer stubReset()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	secondPlayRan := false
	stubSet("first", StubFixture{ExecuteError: errors.New("interrupted"), Hook: cancel})
	stubSet("second", StubFixture{Changed: true, Hook: func() { secondPlayRan = true }})

	path := writeTasksFile(t, `---
- name: one
  tasks:
    - name: first
      dokku_stub: { key: first }
- name: two
  tasks:
    - name: second
      dokku_stub: { key: second }
`)
	stdout, stderr, exit := runWithCtx(t, ctx, "apply", path)

	if secondPlayRan {
		t.Error("the play after the cancellation must not run")
	}
	if strings.Contains(stdout, "Play: two") {
		t.Errorf("a play whose tasks will never run should not print a header; got %q", stdout)
	}
	if exit != 1 {
		t.Errorf("exit = %d, want 1", exit)
	}
	if !strings.Contains(stderr, "run cancelled") {
		t.Errorf("stderr should say the run was cancelled; got %q", stderr)
	}
}

// TestApplyWithoutCtxUsesBackground pins the fallback: a command built as a
// bare struct literal, as most of these tests and any embedding caller that
// does not set Ctx do, still runs instead of panicking on a nil context.
func TestApplyWithoutCtxUsesBackground(t *testing.T) {
	defer stubReset()
	stubSet("first", StubFixture{Changed: true})
	stubSet("second", StubFixture{Changed: true})

	path := writeTasksFile(t, twoTaskRecipe)
	if _, stderr, exit := runApply(t, path); exit != 0 {
		t.Errorf("exit = %d, want 0; stderr: %s", exit, stderr)
	}
}
