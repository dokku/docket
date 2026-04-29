package tasks

import (
	"errors"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestWithExecResultCopiesFields(t *testing.T) {
	state := TaskOutputState{State: StateAbsent}
	result := subprocess.ExecCommandResponse{
		Stdout:   "hello\n",
		Stderr:   "warn: deprecated\n",
		ExitCode: 0,
	}

	got := state.WithExecResult(result)

	if got.Stdout != result.Stdout {
		t.Errorf("Stdout = %q, want %q", got.Stdout, result.Stdout)
	}
	if got.Stderr != result.Stderr {
		t.Errorf("Stderr = %q, want %q", got.Stderr, result.Stderr)
	}
	if got.ExitCode != result.ExitCode {
		t.Errorf("ExitCode = %d, want %d", got.ExitCode, result.ExitCode)
	}
}

func TestWithExecResultDoesNotMutateReceiver(t *testing.T) {
	original := TaskOutputState{Stdout: "untouched", Stderr: "untouched", ExitCode: 7}
	_ = original.WithExecResult(subprocess.ExecCommandResponse{
		Stdout:   "new",
		Stderr:   "new",
		ExitCode: 1,
	})

	if original.Stdout != "untouched" {
		t.Errorf("receiver Stdout mutated: %q", original.Stdout)
	}
	if original.Stderr != "untouched" {
		t.Errorf("receiver Stderr mutated: %q", original.Stderr)
	}
	if original.ExitCode != 7 {
		t.Errorf("receiver ExitCode mutated: %d", original.ExitCode)
	}
}

// TestExecutePlanProbeErrorPopulatesStderrFromExecError pins the
// behavior #210 added: when a probe helper bubbles a CallExecCommand
// failure, the wrapped *subprocess.ExecError carries the response
// fields, and ExecutePlan recovers them onto the returned
// TaskOutputState so failed_when predicates can match against
// `result.Stderr` even on probe-error paths.
func TestExecutePlanProbeErrorPopulatesStderrFromExecError(t *testing.T) {
	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stdout:   "stdout text",
			Stderr:   "App nonexistent does not exist",
			ExitCode: 20,
		},
		Err: errors.New("App nonexistent does not exist"),
	}
	got := ExecutePlan(PlanResult{
		Status:       PlanStatusError,
		Error:        execErr,
		DesiredState: StatePresent,
	})
	if got.Error == nil {
		t.Fatalf("expected error to propagate")
	}
	if got.Stderr != "App nonexistent does not exist" {
		t.Errorf("stderr should recover from ExecError; got %q", got.Stderr)
	}
	if got.Stdout != "stdout text" {
		t.Errorf("stdout should recover from ExecError; got %q", got.Stdout)
	}
	if got.ExitCode != 20 {
		t.Errorf("exit code should recover from ExecError; got %d", got.ExitCode)
	}
}

// TestExecutePlanExplicitProbeFieldsTakePrecedence pins that PlanResult
// fields populated by a probe helper that called PlanErrorFromExec
// directly are not overwritten by the ExecError fallback.
func TestExecutePlanExplicitProbeFieldsTakePrecedence(t *testing.T) {
	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr:   "from-execerror",
			ExitCode: 1,
		},
		Err: errors.New("boom"),
	}
	got := ExecutePlan(PlanResult{
		Status:       PlanStatusError,
		Error:        execErr,
		DesiredState: StatePresent,
		Stderr:       "explicit",
		ExitCode:     42,
	})
	if got.Stderr != "explicit" {
		t.Errorf("explicit Stderr should win; got %q", got.Stderr)
	}
	if got.ExitCode != 42 {
		t.Errorf("explicit ExitCode should win; got %d", got.ExitCode)
	}
}

func TestWithExecResultZeroValueClears(t *testing.T) {
	// A no-op apply (no inputs) hands runExecInputs the zero
	// ExecCommandResponse; the contract is that the new fields then
	// stay zero-valued, not that they retain whatever the caller had.
	state := TaskOutputState{Stdout: "stale", Stderr: "stale", ExitCode: 9}

	got := state.WithExecResult(subprocess.ExecCommandResponse{})

	if got.Stdout != "" || got.Stderr != "" || got.ExitCode != 0 {
		t.Errorf("zero ExecCommandResponse should clear fields, got Stdout=%q Stderr=%q ExitCode=%d", got.Stdout, got.Stderr, got.ExitCode)
	}
}
