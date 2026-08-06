package commands

import (
	"errors"
	"testing"
)

// These tests pin the `docket apply --detailed-exitcode` contract that
// docs/ansible-dokku.md tells wrapper authors to rely on: a wrapper that
// runs one docket invocation per Ansible task derives `changed` from the
// exit code alone, without parsing the --json event stream. They mirror
// the plan-side guard in play_executor_test.go
// (TestPlanProbeErrorRendersMarkerAndExits).

// TestApplyDetailedExitCodeChanged: a task that changes state exits 2
// with the flag and 0 without it.
func TestApplyDetailedExitCodeChanged(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: changes
      dokku_stub: { key: a }
`)

	if _, _, exit := runApply(t, path, "--detailed-exitcode"); exit != 2 {
		t.Errorf("detailed-exitcode exit = %d, want 2 (task changed)", exit)
	}
	if _, _, exit := runApply(t, path); exit != 0 {
		t.Errorf("plain exit = %d, want 0 (apply ignores change without the flag)", exit)
	}
}

// TestApplyDetailedExitCodeUnchanged: an in-sync task exits 0 either way.
func TestApplyDetailedExitCodeUnchanged(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false})

	path := writeTasksFile(t, `---
- tasks:
    - name: in sync
      dokku_stub: { key: a }
`)

	if _, _, exit := runApply(t, path, "--detailed-exitcode"); exit != 0 {
		t.Errorf("detailed-exitcode exit = %d, want 0 (nothing changed)", exit)
	}
	if _, _, exit := runApply(t, path); exit != 0 {
		t.Errorf("plain exit = %d, want 0", exit)
	}
}

// TestApplyDetailedExitCodeErrorsWinOverChanges: when a task changed and
// a later task errored, the exit code is 1, not 2. A wrapper must never
// read a failure as "changed".
func TestApplyDetailedExitCodeErrorsWinOverChanges(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{ExecuteError: errors.New("boom")})

	path := writeTasksFile(t, `---
- tasks:
    - name: changes
      dokku_stub: { key: a }
    - name: errors
      dokku_stub: { key: b }
`)

	if _, _, exit := runApply(t, path, "--detailed-exitcode"); exit != 1 {
		t.Errorf("detailed-exitcode exit = %d, want 1 (errors win over changes)", exit)
	}
	if _, _, exit := runApply(t, path); exit != 1 {
		t.Errorf("plain exit = %d, want 1", exit)
	}
}

// TestApplyDetailedExitCodeIgnoredErrorStillCountsChanges: an error
// swallowed by ignore_errors is not an error for exit-code purposes, so
// a change elsewhere in the run still surfaces as 2.
func TestApplyDetailedExitCodeIgnoredErrorStillCountsChanges(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{ExecuteError: errors.New("boom")})

	path := writeTasksFile(t, `---
- tasks:
    - name: changes
      dokku_stub: { key: a }
    - name: errors but ignored
      ignore_errors: true
      dokku_stub: { key: b }
`)

	if _, _, exit := runApply(t, path, "--detailed-exitcode"); exit != 2 {
		t.Errorf("detailed-exitcode exit = %d, want 2 (ignored error is not an error)", exit)
	}
}

// TestApplyDetailedExitCodeListTasksUnaffected: --list-tasks returns
// before any task runs, so it cannot report a change.
func TestApplyDetailedExitCodeListTasksUnaffected(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: changes
      dokku_stub: { key: a }
`)

	if _, _, exit := runApply(t, path, "--detailed-exitcode", "--list-tasks"); exit != 0 {
		t.Errorf("--list-tasks exit = %d, want 0", exit)
	}
}
