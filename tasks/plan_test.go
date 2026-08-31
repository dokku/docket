package tasks

import (
	"context"
	"errors"
	"testing"
)

// TestAllTasksImplementPlan asserts every registered task type satisfies the
// Task interface (which now requires Plan()). The interface assignment is a
// compile-time guarantee, but this test fails loudly if a future refactor
// loosens the contract or registers a task that does not implement Plan.
func TestAllTasksImplementPlan(t *testing.T) {
	if len(RegisteredTasks) == 0 {
		t.Fatal("no tasks registered; init() side effects didn't run")
	}
	for name, task := range RegisteredTasks {
		var _ Task = task
		if name == "" {
			t.Errorf("task with empty name: %T", task)
		}
	}
}

// TestDispatchPlanInvalidState verifies that DispatchPlan returns an error
// PlanResult when the requested state has no handler, mirroring DispatchState.
func TestDispatchPlanInvalidState(t *testing.T) {
	result := DispatchPlan(State("nonsense"), map[State]func() PlanResult{
		StatePresent: func() PlanResult { return PlanResult{InSync: true} },
	})
	if result.Error == nil {
		t.Fatal("DispatchPlan with unknown state should set Error")
	}
	if result.Status != PlanStatusError {
		t.Errorf("expected PlanStatusError, got %q", result.Status)
	}
}

// TestDispatchPlanSetsDesiredState verifies DispatchPlan propagates the
// requested state onto the returned PlanResult.
func TestDispatchPlanSetsDesiredState(t *testing.T) {
	result := DispatchPlan(StatePresent, map[State]func() PlanResult{
		StatePresent: func() PlanResult { return PlanResult{InSync: true, Status: PlanStatusOK} },
	})
	if result.DesiredState != StatePresent {
		t.Errorf("expected DesiredState=%q, got %q", StatePresent, result.DesiredState)
	}
	if !result.InSync {
		t.Error("expected handler's InSync to propagate")
	}
}

// TestExecutePlanInSyncSkipsApply verifies that ExecutePlan does not invoke
// the apply closure when the plan reports InSync. This is the critical
// invariant that makes apply idempotent for the post-#198 design.
func TestExecutePlanInSyncSkipsApply(t *testing.T) {
	called := false
	result := ExecutePlan(testCtx(), PlanResult{
		InSync:       true,
		Status:       PlanStatusOK,
		DesiredState: StatePresent,
		apply: func(ctx context.Context) TaskOutputState {
			called = true
			return TaskOutputState{}
		},
	})
	if called {
		t.Fatal("ExecutePlan invoked apply on InSync result")
	}
	if result.Changed {
		t.Error("InSync result should report Changed=false")
	}
	if result.State != StatePresent {
		t.Errorf("expected State=%q, got %q", StatePresent, result.State)
	}
	if result.DesiredState != StatePresent {
		t.Errorf("expected DesiredState=%q, got %q", StatePresent, result.DesiredState)
	}
}

// TestExecutePlanErrorSkipsApply verifies that ExecutePlan does not invoke
// the apply closure when the plan reports an error.
func TestExecutePlanErrorSkipsApply(t *testing.T) {
	called := false
	probeErr := errors.New("probe failed")
	result := ExecutePlan(testCtx(), PlanResult{
		Status:       PlanStatusError,
		Error:        probeErr,
		DesiredState: StatePresent,
		apply: func(ctx context.Context) TaskOutputState {
			called = true
			return TaskOutputState{}
		},
	})
	if called {
		t.Fatal("ExecutePlan invoked apply on error result")
	}
	if !errors.Is(result.Error, probeErr) {
		t.Errorf("expected Error to be probeErr, got %v", result.Error)
	}
}

// TestExecutePlanInvokesApplyOnDrift verifies that ExecutePlan does invoke
// the apply closure when the plan reports drift (the canonical path).
func TestExecutePlanInvokesApplyOnDrift(t *testing.T) {
	called := false
	result := ExecutePlan(testCtx(), PlanResult{
		InSync:       false,
		Status:       PlanStatusModify,
		DesiredState: StatePresent,
		apply: func(ctx context.Context) TaskOutputState {
			called = true
			return TaskOutputState{Changed: true, State: StatePresent}
		},
	})
	if !called {
		t.Fatal("ExecutePlan did not invoke apply on drift result")
	}
	if !result.Changed {
		t.Error("apply returned Changed=true; ExecutePlan must propagate it")
	}
	if result.DesiredState != StatePresent {
		t.Errorf("ExecutePlan should fill in DesiredState; got %q", result.DesiredState)
	}
}

// TestExecutePlanMissingApplyIsError verifies that a drift PlanResult
// without an apply closure surfaces a clear error rather than silently
// no-opping. Catches refactor mistakes where Plan() forgets to set apply.
func TestExecutePlanMissingApplyIsError(t *testing.T) {
	result := ExecutePlan(testCtx(), PlanResult{
		InSync:       false,
		Status:       PlanStatusModify,
		DesiredState: StatePresent,
		// apply intentionally nil
	})
	if result.Error == nil {
		t.Fatal("ExecutePlan with drift but no apply should error")
	}
}

// TestPlanResultStatusConstants documents the canonical status values used
// across the codebase. If a refactor renames or removes one, this test
// fails so the plan output formatter and JSON consumers can be updated.
func TestPlanResultStatusConstants(t *testing.T) {
	cases := map[PlanStatus]string{
		PlanStatusOK:      "ok",
		PlanStatusModify:  "~",
		PlanStatusCreate:  "+",
		PlanStatusDestroy: "-",
		PlanStatusError:   "!",
	}
	for got, want := range cases {
		if string(got) != want {
			t.Errorf("status %q has unexpected string %q", want, string(got))
		}
	}
}

// TestPlanConfigSetItemizesKeys exercises the per-key diff helper used by
// config_task.Plan to ensure the right keys are flagged. Plan-level
// integration with subprocess is covered by integration tests; this is a
// pure-data test that does not touch dokku.
func TestPlanConfigSetItemizesKeys(t *testing.T) {
	current := map[string]string{
		"EXISTING": "old",
		"KEEP":     "same",
	}
	desired := map[string]string{
		"EXISTING": "new",
		"KEEP":     "same",
		"NEW":      "value",
	}

	keys := configKeysToSet(current, desired)
	if len(keys) != 2 {
		t.Fatalf("expected 2 keys to set, got %d: %v", len(keys), keys)
	}
	got := map[string]bool{}
	for _, k := range keys {
		got[k] = true
	}
	if !got["EXISTING"] || !got["NEW"] {
		t.Errorf("expected EXISTING and NEW; got %v", keys)
	}
	if got["KEEP"] {
		t.Error("KEEP should not be in keys to set (value matches)")
	}
}

// TestPlanConfigUnsetItemizesKeys checks that the unset path itemizes only
// keys that currently exist on the server.
func TestPlanConfigUnsetItemizesKeys(t *testing.T) {
	current := map[string]string{"PRESENT": "v"}
	desired := map[string]string{"PRESENT": "", "MISSING": ""}
	keys := configKeysToUnset(current, desired)
	if len(keys) != 1 || keys[0] != "PRESENT" {
		t.Errorf("expected only PRESENT; got %v", keys)
	}
}

// TestPluginFromSubcommand exercises the plugin extraction used by the
// generic property probe.
func TestPluginFromSubcommand(t *testing.T) {
	cases := map[string]string{
		"nginx:set":                  "nginx",
		"app-json:set":               "app-json",
		"buildpacks:set-property":    "buildpacks",
		"scheduler-docker-local:set": "scheduler-docker-local",
	}
	for input, want := range cases {
		if got := pluginFromSubcommand(input); got != want {
			t.Errorf("pluginFromSubcommand(%q) = %q, want %q", input, got, want)
		}
	}
}
