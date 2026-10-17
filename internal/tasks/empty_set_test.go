package tasks

import (
	"context"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// TestEmptySetPlansAsClear checks, for every task offering both set and clear,
// that a set declaring an empty collection plans exactly what clear plans -
// the same commands, mutations, status and Observed state - and that applying
// it reports set, the state the recipe declared. Each task is checked against
// every resource its observed fixture exports, built from that resource's
// identity keys alone, so there is something for clear to remove.
func TestEmptySetPlansAsClear(t *testing.T) {
	t.Parallel()
	fixtures := observedFixtures()
	for _, typeKey := range TaskTypes() {
		prototype, _ := Lookup(typeKey)
		if !offersState(prototype, StateSet) || !offersState(prototype, StateClear) {
			continue
		}
		t.Run(typeKey, func(t *testing.T) {
			t.Parallel()
			fixture, ok := fixtures[typeKey]
			if !ok || fixture.export == nil {
				t.Fatal("no observed fixture exports a resource to clear")
			}
			ctx := subprocess.ContextWithRunner(testCtx(), observedRunner(t, fixture))
			bodies, err := fixture.export(ctx)
			if err != nil {
				t.Fatalf("export: %v", err)
			}
			drifted := false
			for _, body := range bodies {
				clearTask := keysOnly(t, typeKey, body.(Task), StateClear)
				setTask := keysOnly(t, typeKey, body.(Task), StateSet)
				address := IdentityAddress(typeKey, clearTask)
				// A resource keyed by a field the collection states do not
				// take, such as a single storage mount's container_dir, is
				// not one clear can address; set must refuse it the same way.
				if err := clearTask.(InputValidator).Validate(); err != nil {
					if setTask.(InputValidator).Validate() == nil {
						t.Errorf("%s: clear refuses the address (%v) but set accepts it", address, err)
					}
					continue
				}

				clearPlan := clearTask.Plan(ctx)
				setPlan := setTask.Plan(ctx)
				if clearPlan.Error != nil || setPlan.Error != nil {
					t.Errorf("%s: clear error = %v, set error = %v", address, clearPlan.Error, setPlan.Error)
					continue
				}
				if setPlan.InSync != clearPlan.InSync || setPlan.Status != clearPlan.Status {
					t.Errorf("%s: set plans {InSync %v, Status %q}, clear plans {InSync %v, Status %q}",
						address, setPlan.InSync, setPlan.Status, clearPlan.InSync, clearPlan.Status)
				}
				if !reflect.DeepEqual(setPlan.Commands, clearPlan.Commands) {
					t.Errorf("%s: set commands = %v, clear commands = %v", address, setPlan.Commands, clearPlan.Commands)
				}
				if !reflect.DeepEqual(setPlan.Mutations, clearPlan.Mutations) {
					t.Errorf("%s: set mutations = %v, clear mutations = %v", address, setPlan.Mutations, clearPlan.Mutations)
				}
				if !reflect.DeepEqual(normalizeObserved(setPlan.Observed), normalizeObserved(clearPlan.Observed)) {
					t.Errorf("%s: set observed %#v, clear observed %#v", address, setPlan.Observed, clearPlan.Observed)
				}
				if setPlan.DesiredState != StateSet {
					t.Errorf("%s: DesiredState = %q, want set", address, setPlan.DesiredState)
				}
				if setPlan.InSync {
					continue
				}
				drifted = true
				applyCtx := subprocess.ContextWithRunner(testCtx(), acceptingRunner(fixture))
				out := ExecutePlan(applyCtx, setPlan)
				if out.Error != nil {
					t.Errorf("%s: apply error: %v", address, out.Error)
					continue
				}
				if !out.Changed || out.State != StateSet || out.DesiredState != StateSet {
					t.Errorf("%s: apply = {Changed %v, State %q, DesiredState %q}, want a change ending in set",
						address, out.Changed, out.State, out.DesiredState)
				}
			}
			if !drifted {
				t.Error("no exported resource gave clear anything to remove")
			}
		})
	}
}

// offersState reports whether a task's State field lists state among its
// options.
func offersState(task Task, state State) bool {
	for _, option := range stateOptions(task) {
		if State(option) == state {
			return true
		}
	}
	return false
}

// keysOnly builds a task of typeKey carrying only body's identity keys and the
// given state, so its collection is empty.
func keysOnly(t *testing.T, typeKey string, body Task, state State) Task {
	t.Helper()
	task, err := NewTask(typeKey)
	if err != nil {
		t.Fatal(err)
	}
	dst := reflect.ValueOf(task).Elem()
	src, _ := structValue(body)
	for _, key := range TaskIdentity(task) {
		dst.FieldByName(key.Field).Set(src.FieldByName(key.Field))
	}
	dst.FieldByName("State").SetString(string(state))
	return task
}

// acceptingRunner answers a fixture's reads and accepts every other command,
// the writes an apply runs.
func acceptingRunner(f observedFixture) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Stdout: f.responses[strings.Join(in.Args, " ")]}, nil
	}
}
