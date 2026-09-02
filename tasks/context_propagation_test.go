package tasks

import (
	"context"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// ctxKey is the sentinel type used to prove which context reached a subprocess
// call. A distinct unexported type keeps the value from colliding with anything
// else riding on the context.
type ctxKey struct{}

// TestExecutePlanPassesItsContextToApply pins the one real design question
// #424 raises: the apply closure is built during Plan and invoked later by
// ExecutePlan, so it either captures a context or takes one. It takes one -
// which means applying a plan uses the caller's current cancellation and
// target rather than whatever was in scope when the plan was computed.
func TestExecutePlanPassesItsContextToApply(t *testing.T) {
	t.Parallel()
	planCtx := context.WithValue(context.Background(), ctxKey{}, "plan")
	applyCtx := context.WithValue(context.Background(), ctxKey{}, "apply")

	var seen string
	// buildPlan stands in for a task's Plan(): it runs under planCtx, so a
	// closure that captured rather than took a context would capture that one.
	buildPlan := func(planCtx context.Context) PlanResult {
		return PlanResult{
			Status:       PlanStatusCreate,
			DesiredState: StatePresent,
			Commands:     []string{"dokku apps:create demo"},
			apply: func(ctx context.Context) TaskOutputState {
				seen, _ = ctx.Value(ctxKey{}).(string)
				return TaskOutputState{Changed: true, State: StatePresent}
			},
		}
	}

	state := ExecutePlan(applyCtx, buildPlan(planCtx))
	if state.Error != nil {
		t.Fatalf("ExecutePlan returned %v", state.Error)
	}
	if seen != "apply" {
		t.Errorf("apply ran under context %q, want %q", seen, "apply")
	}
}

// TestPlanUsesTheContextItWasGiven pins that the context reaches all the way
// down to the subprocess call a task's probe makes. Before this, every task
// bottomed out in a context.Background() manufactured inside subprocess, so a
// caller's cancellation and deadline could not reach a running task at all.
func TestPlanUsesTheContextItWasGiven(t *testing.T) {
	t.Parallel()
	var seen string
	// The runner goes on the caller's own context, not a fresh one, so what the
	// probe observes is the value the caller put there.
	ctx := subprocess.ContextWithRunner(
		context.WithValue(context.Background(), ctxKey{}, "caller"),
		func(ctx context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			seen, _ = ctx.Value(ctxKey{}).(string)
			return subprocess.ExecCommandResponse{}, nil
		})

	AppTask{App: "demo", State: StatePresent}.Plan(ctx)

	if seen != "caller" {
		t.Errorf("probe ran under context %q, want %q", seen, "caller")
	}
}

// TestPlanReportsCancellationAsAnError pins that a probe cut short by a
// cancelled context produces a plan error rather than reported drift. Reading
// "the probe did not answer" as "the resource is missing" would have apply
// mutate a server whose state was never established.
func TestPlanReportsCancellationAsAnError(t *testing.T) {
	t.Parallel()
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	ctx := subprocess.ContextWithRunner(cancelled, func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Cancelled: true}, &subprocess.ExecError{Err: context.Canceled}
	})

	plan := AppTask{App: "demo", State: StatePresent}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a cancelled probe to surface as a plan error")
	}
	if plan.Status != PlanStatusError {
		t.Errorf("status = %q, want %q", plan.Status, PlanStatusError)
	}
	if plan.InSync {
		t.Error("a plan that could not probe must not report in sync")
	}
}
