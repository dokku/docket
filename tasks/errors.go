package tasks

import "github.com/dokku/docket/subprocess"

// TaskOutputErrorFromExec sets Error and Message on the given state from an
// exec result, copies the captured Stdout/Stderr/ExitCode onto the state via
// WithExecResult, and ensures the failing invocation is recorded in
// state.Commands so it surfaces under `--verbose`. Idempotent against the
// per-task pre-error-check append pattern: when the failing command is
// already the most recent entry it is not appended again. Returns the
// modified state for convenient use with early returns.
func TaskOutputErrorFromExec(state TaskOutputState, err error, result subprocess.ExecCommandResponse) TaskOutputState {
	state.Error = err
	state.Message = result.StderrContents()
	if result.Command != "" {
		if n := len(state.Commands); n == 0 || state.Commands[n-1] != result.Command {
			state.Commands = append(state.Commands, result.Command)
		}
	}
	return state.WithExecResult(result)
}
