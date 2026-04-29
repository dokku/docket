package tasks

import (
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
