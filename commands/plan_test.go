package commands

import (
	"context"
	"testing"

	"github.com/dokku/docket/tasks"
)

// TestPlanCommandMetadata is a smoke check that PlanCommand exposes a
// reasonable Name and Synopsis, satisfying the cli.Command interface.
func TestPlanCommandMetadata(t *testing.T) {
	c := &PlanCommand{}
	if c.Name() != "plan" {
		t.Errorf("Name = %q, want \"plan\"", c.Name())
	}
	if c.Synopsis() == "" {
		t.Error("Synopsis must not be empty")
	}
}

// TestPlanCommandExamples ensures every example string is non-empty. The
// cli-skeleton uses these in --help output.
func TestPlanCommandExamples(t *testing.T) {
	c := &PlanCommand{}
	examples := c.Examples()
	if len(examples) == 0 {
		t.Fatal("expected at least one example")
	}
	for label, example := range examples {
		if example == "" {
			t.Errorf("example %q is empty", label)
		}
	}
}

// TestPlanCommandHelpDoesNotPanic guards against a regression where
// FlagSet panics at help time when no tasks.yml exists on disk.
func TestPlanCommandHelpDoesNotPanic(t *testing.T) {
	c := &PlanCommand{}
	defer func() {
		if r := recover(); r != nil {
			t.Errorf("FlagSet panicked without tasks.yml on disk: %v", r)
		}
	}()
	_ = c.FlagSet()
}

// TestPlanCommandUsesPlanInterface guards the contract between the commands
// package and the tasks package: PlanCommand consumes tasks.Task, which must
// expose Plan and Execute taking the run context and returning the plan /
// output types the command layer switches on. The context parameter is the
// part worth pinning - it is how cancellation and the target reach a task,
// and dropping it would silently put both back on package globals.
func TestPlanCommandUsesPlanInterface(t *testing.T) {
	var _ interface {
		Plan(ctx context.Context) tasks.PlanResult
		Execute(ctx context.Context) tasks.TaskOutputState
	} = (tasks.Task)(nil)
}
