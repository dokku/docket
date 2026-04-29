package tasks

import (
	"testing"

	"github.com/dokku/docket/subprocess"
)

// TestIntegrationRunExecInputsPopulatesExitCodeAndStdout drives the
// shared dispatch helper end-to-end against a real dokku and verifies
// that the new ExitCode/Stdout/Stderr fields land on the returned
// TaskOutputState. `dokku version` is the cheapest read-only call that
// is guaranteed to succeed on every dokku install.
func TestIntegrationRunExecInputsPopulatesExitCodeAndStdout(t *testing.T) {
	skipIfNoDokkuT(t)

	state := runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, []subprocess.ExecCommandInput{{
		Command: "dokku",
		Args:    []string{"version"},
	}})

	if state.Error != nil {
		t.Fatalf("runExecInputs returned error: %v", state.Error)
	}
	if state.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", state.ExitCode)
	}
	if state.Stdout == "" {
		t.Errorf("Stdout should be non-empty, got %q", state.Stdout)
	}
	if !state.Changed {
		t.Error("Changed should be true on a successful apply path")
	}
	if state.State != StatePresent {
		t.Errorf("State = %v, want StatePresent", state.State)
	}
	if len(state.Commands) != 1 {
		t.Errorf("Commands = %v, want one entry", state.Commands)
	}
}

// TestIntegrationRunExecInputsCapturesFailureOutput drives a guaranteed
// failure and verifies the error path also populates the new fields.
// `dokku apps:create ""` rejects the empty app name with a non-zero
// exit and a stderr message.
func TestIntegrationRunExecInputsCapturesFailureOutput(t *testing.T) {
	skipIfNoDokkuT(t)

	state := runExecInputs(TaskOutputState{State: StateAbsent}, StatePresent, []subprocess.ExecCommandInput{{
		Command: "dokku",
		Args:    []string{"--quiet", "apps:create", ""},
	}})

	if state.Error == nil {
		t.Fatal("expected error from apps:create with empty name")
	}
	if state.ExitCode == 0 {
		t.Errorf("ExitCode = %d, want non-zero on failure", state.ExitCode)
	}
	if state.Stderr == "" {
		t.Errorf("Stderr should be populated on failure, got empty")
	}
}
