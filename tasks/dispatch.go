package tasks

import (
	"errors"
	"fmt"

	"github.com/dokku/docket/subprocess"
)

// resolveCommands renders the list of ExecCommandInputs to the masked,
// dispatch-aware command lines apply would echo on execution. Tasks call
// it from Plan() to populate PlanResult.Commands so plan output and
// apply --verbose render byte-identical strings for the same operation.
func resolveCommands(inputs []subprocess.ExecCommandInput) []string {
	out := make([]string, len(inputs))
	for i, in := range inputs {
		out[i] = subprocess.ResolveCommandString(in)
	}
	return out
}

// runExecInputs runs each input in order, appending the resolved command
// line to state.Commands and bailing on the first error. Used by
// Plan-built apply closures that just need to invoke a list of dokku
// commands sequentially. On success, the final input's
// stdout/stderr/exit-code is copied onto the returned state via
// WithExecResult so callers can inspect what the underlying subprocess
// produced. When inputs is empty (no-op apply), the new fields stay
// zero-valued.
func runExecInputs(initial TaskOutputState, finalState State, inputs []subprocess.ExecCommandInput) TaskOutputState {
	state := initial
	var last subprocess.ExecCommandResponse
	for _, in := range inputs {
		result, err := subprocess.CallExecCommand(in)
		state.Commands = append(state.Commands, result.Command)
		if err != nil {
			return TaskOutputErrorFromExec(state, err, result)
		}
		last = result
	}
	state = state.WithExecResult(last)
	state.Changed = true
	state.State = finalState
	return state
}

// DispatchState looks up the given state in the funcMap, calls the matching function,
// and returns an error TaskOutputState if the state is not found.
func DispatchState(state State, funcMap map[State]func() TaskOutputState) TaskOutputState {
	fn, ok := funcMap[state]
	if !ok {
		return TaskOutputState{
			Error: fmt.Errorf("invalid state: %s", state),
		}
	}

	result := fn()
	result.DesiredState = state
	return result
}

// DispatchPlan looks up the given state in the funcMap, calls the matching
// function, and returns an error PlanResult if the state is not found.
// Mirrors DispatchState but for the read-only Plan() path.
func DispatchPlan(state State, funcMap map[State]func() PlanResult) PlanResult {
	fn, ok := funcMap[state]
	if !ok {
		return PlanResult{
			Status: PlanStatusError,
			Error:  fmt.Errorf("invalid state: %s", state),
		}
	}

	result := fn()
	result.DesiredState = state
	return result
}

// ExecutePlan applies a PlanResult to the server. It is the canonical
// implementation of Task.Execute(): each task's Execute body is
// `return ExecutePlan(t.Plan())`. ExecutePlan ensures the existing
// `state.State == state.DesiredState` contract that commands/apply.go
// relies on.
//
// Three branches:
//
//  1. p.Error != nil  - probe failed; return TaskOutputState carrying the
//     error and the desired state. The apply closure is not invoked.
//  2. p.InSync        - no change needed; return State == DesiredState with
//     Changed=false. The apply closure is not invoked.
//  3. otherwise       - invoke p.apply (must be non-nil) and return its
//     TaskOutputState verbatim. apply is responsible for setting Changed
//     and a final State that matches DesiredState on success.
func ExecutePlan(p PlanResult) TaskOutputState {
	if p.Error != nil {
		stdout, stderr, exitCode := p.Stdout, p.Stderr, p.ExitCode
		// Recover the underlying ExecCommandResponse if a probe helper
		// surfaced its CallExecCommand failure as a *subprocess.ExecError.
		// This lets `failed_when: 'result.Stderr contains ...'` predicates
		// match against probe-side stderr without each probe helper having
		// to thread the response through its return signature.
		if stdout == "" && stderr == "" && exitCode == 0 {
			var execErr *subprocess.ExecError
			if errors.As(p.Error, &execErr) {
				stdout = execErr.Response.Stdout
				stderr = execErr.Response.Stderr
				exitCode = execErr.Response.ExitCode
			}
		}
		return TaskOutputState{
			Error:        p.Error,
			Message:      p.Error.Error(),
			DesiredState: p.DesiredState,
			State:        p.DesiredState,
			Stdout:       stdout,
			Stderr:       stderr,
			ExitCode:     exitCode,
		}
	}
	if p.InSync {
		return TaskOutputState{
			Changed:      false,
			State:        p.DesiredState,
			DesiredState: p.DesiredState,
		}
	}
	if p.apply == nil {
		return TaskOutputState{
			Error:        fmt.Errorf("plan reports drift but no apply function was provided"),
			DesiredState: p.DesiredState,
			State:        p.DesiredState,
		}
	}
	out := p.apply()
	if out.DesiredState == "" {
		out.DesiredState = p.DesiredState
	}
	return out
}

// PlanErrorFromExec wraps a probe-side CallExecCommand failure into a
// PlanResult that also carries the response's Stdout/Stderr/ExitCode.
// Probe helpers use it from their `if err != nil` branch so a later
// failed_when predicate can match `result.Stderr` in plan mode the
// same way it can in apply mode. desiredState is the state value the
// caller would have stored in PlanResult.DesiredState; pass the State
// argument the probe is processing.
func PlanErrorFromExec(desiredState State, err error, r subprocess.ExecCommandResponse) PlanResult {
	return PlanResult{
		Status:       PlanStatusError,
		Error:        err,
		DesiredState: desiredState,
		Stdout:       r.Stdout,
		Stderr:       r.Stderr,
		ExitCode:     r.ExitCode,
	}
}
