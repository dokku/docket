package tasks

import (
	"errors"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestTaskOutputErrorFromExecPopulatesAllFields(t *testing.T) {
	state := TaskOutputState{State: StateAbsent}
	result := subprocess.ExecCommandResponse{
		Command:  "dokku --quiet apps:create boom",
		Stdout:   "trying to create boom\n",
		Stderr:   "app boom already exists\n",
		ExitCode: 1,
	}
	err := errors.New(result.Stderr)

	got := TaskOutputErrorFromExec(state, err, result)

	if got.Error == nil || got.Error.Error() != err.Error() {
		t.Errorf("Error = %v, want %v", got.Error, err)
	}
	if want := "app boom already exists"; got.Message != want {
		t.Errorf("Message = %q, want %q", got.Message, want)
	}
	if got.Stdout != result.Stdout {
		t.Errorf("Stdout = %q, want %q", got.Stdout, result.Stdout)
	}
	if got.Stderr != result.Stderr {
		t.Errorf("Stderr = %q, want %q", got.Stderr, result.Stderr)
	}
	if got.ExitCode != 1 {
		t.Errorf("ExitCode = %d, want 1", got.ExitCode)
	}
	if len(got.Commands) != 1 || got.Commands[0] != result.Command {
		t.Errorf("Commands = %v, want [%q]", got.Commands, result.Command)
	}
}

func TestTaskOutputErrorFromExecIdempotentCommandAppend(t *testing.T) {
	// runExecInputs appends the resolved command before checking err, so
	// the helper must not double-append the failing entry.
	cmd := "dokku --quiet apps:create boom"
	state := TaskOutputState{Commands: []string{cmd}}
	result := subprocess.ExecCommandResponse{
		Command:  cmd,
		Stderr:   "boom",
		ExitCode: 1,
	}

	got := TaskOutputErrorFromExec(state, errors.New("boom"), result)

	if len(got.Commands) != 1 {
		t.Errorf("Commands = %v, want one entry (no duplicate)", got.Commands)
	}
}

func TestTaskOutputErrorFromExecZeroExitCodePreserved(t *testing.T) {
	// A non-error path that nonetheless calls TaskOutputErrorFromExec
	// (e.g. wrapping a custom error) should still copy ExitCode = 0
	// rather than leaving the field at the receiver's prior value.
	state := TaskOutputState{ExitCode: 99}
	result := subprocess.ExecCommandResponse{Command: "dokku x", ExitCode: 0}

	got := TaskOutputErrorFromExec(state, errors.New("synthetic"), result)

	if got.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0 (overwritten)", got.ExitCode)
	}
}
