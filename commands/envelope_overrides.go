package commands

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/tasks"
)

// applyEnvelopeOverrides applies the post-execute predicate phases on
// state in the order documented in #210: failed_when first, then
// changed_when, then register snapshot. The post-override state is
// returned. If either predicate raises an expr runtime error, state is
// returned unchanged with the wrapped error so the caller surfaces it
// the same way it surfaces a `when:` evaluation error.
//
// `failed_when` truthy installs a synthetic error
// (`fmt.Errorf("failed_when matched: %s", src)`) when the task's own
// state.Error is nil; truthy with an existing error keeps the original
// error. `failed_when` falsy clears state.Error and normalizes
// state.State to state.DesiredState so the apply classifier's
// state-mismatch branch does not re-flag the task. This matches
// Ansible's "failed_when fully overrides the failure verdict"
// semantics.
//
// `changed_when` overrides state.Changed based on truthiness alone; the
// underlying task's self-reported flag is replaced regardless of its
// previous value.
func applyEnvelopeOverrides(
	env *tasks.TaskEnvelope,
	state tasks.TaskOutputState,
	playExprCtx map[string]interface{},
	registered map[string]tasks.RegisteredValue,
) (tasks.TaskOutputState, error) {
	if env == nil {
		return state, nil
	}

	if env.HasFailedWhen() {
		ctx := envelopeExprContext(playExprCtx, env, state, registered)
		ok, err := tasks.EvalBool(env.FailedWhenProgram(), ctx)
		if err != nil {
			return state, fmt.Errorf("failed_when expression error: %w", err)
		}
		if ok {
			if state.Error == nil {
				state.Error = errors.New("failed_when matched: " + env.FailedWhen)
			}
		} else {
			state.Error = nil
			if state.DesiredState != "" {
				state.State = state.DesiredState
			}
		}
	}

	if env.HasChangedWhen() {
		ctx := envelopeExprContext(playExprCtx, env, state, registered)
		ok, err := tasks.EvalBool(env.ChangedWhenProgram(), ctx)
		if err != nil {
			return state, fmt.Errorf("changed_when expression error: %w", err)
		}
		state.Changed = ok
	}

	return state, nil
}

// loopRegisterAccumulator buffers per-iteration TaskOutputStates for a
// loop+register envelope. The apply / plan loop accumulates into it
// each iteration and recomputes the run-wide registered map after every
// append so predicates between iterations see the running aggregate.
type loopRegisterAccumulator map[string][]tasks.TaskOutputState

// remember stores state under name. For the first occurrence it
// initializes the slice; subsequent calls append in source order.
func (a loopRegisterAccumulator) remember(name string, state tasks.TaskOutputState) {
	a[name] = append(a[name], state)
}

// finalize returns the RegisteredValue for name from the accumulated
// states. For a single-iteration accumulator this is identical to the
// non-loop register shape (Results: nil). For multi-iteration accumulators
// the embedded TaskOutputState aggregates per the documented Ansible
// semantics and Results carries every iteration's post-override state.
func (a loopRegisterAccumulator) finalize(name string) tasks.RegisteredValue {
	return tasks.AggregateRegistered(a[name])
}

// recordRegister updates the run-wide registered map with the latest
// state for env.Register, supporting both non-loop and loop expansions.
// Non-loop envelopes overwrite directly; loop expansions accumulate
// into the per-name buffer and republish the running aggregate so
// predicates running between iterations see the in-progress Results
// list rather than nothing.
func recordRegister(
	env *tasks.TaskEnvelope,
	state tasks.TaskOutputState,
	accum loopRegisterAccumulator,
	registered map[string]tasks.RegisteredValue,
) {
	if env == nil || env.Register == "" {
		return
	}
	if !env.IsLoopExpansion {
		registered[env.Register] = tasks.RegisteredValue{TaskOutputState: state}
		return
	}
	accum.remember(env.Register, state)
	registered[env.Register] = accum.finalize(env.Register)
}
