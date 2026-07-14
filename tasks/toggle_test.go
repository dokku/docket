package tasks

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// TestPlanToggleSurfacesSSHTransportError locks the #357 fix: an SSH transport
// failure from the probe must surface as a plan error (so an unreachable host
// is not mistaken for drift), for both the present and absent states.
func TestPlanToggleSurfacesSSHTransportError(t *testing.T) {
	sshErr := &subprocess.SSHError{
		Host:   "dokku@unreachable",
		Stderr: "ssh: connect to host unreachable port 22: Connection refused",
	}
	probe := func(ToggleContext) (bool, error) { return false, sshErr }

	for _, state := range []State{StatePresent, StateAbsent} {
		plan := planToggle(state, "web", "checks:enable", "checks:disable", probe)
		if plan.Status != PlanStatusError {
			t.Errorf("state %s: Status = %q, want %q", state, plan.Status, PlanStatusError)
		}
		if plan.InSync {
			t.Errorf("state %s: expected InSync=false on transport failure", state)
		}
		var got *subprocess.SSHError
		if !errors.As(plan.Error, &got) {
			t.Errorf("state %s: Error = %v, want *subprocess.SSHError", state, plan.Error)
		}
	}
}

// TestPlanToggleTreatsNonSSHProbeErrorAsDrift preserves the deliberate
// "nil/failed probe = drift, must mutate" behavior for non-transport errors
// (e.g. a plugin that does not support the report command).
func TestPlanToggleTreatsNonSSHProbeErrorAsDrift(t *testing.T) {
	probe := func(ToggleContext) (bool, error) { return false, fmt.Errorf("dokku: no such app") }

	for _, tc := range []struct {
		state State
		cmd   string
	}{
		{StatePresent, "checks:enable"},
		{StateAbsent, "checks:disable"},
	} {
		plan := planToggle(tc.state, "web", "checks:enable", "checks:disable", probe)
		if plan.Error != nil {
			t.Errorf("state %s: non-SSH probe error should not surface as a plan error, got %v", tc.state, plan.Error)
		}
		if plan.Status != PlanStatusModify {
			t.Errorf("state %s: Status = %q, want %q", tc.state, plan.Status, PlanStatusModify)
		}
		if plan.InSync {
			t.Errorf("state %s: expected drift (InSync=false)", tc.state)
		}
		if len(plan.Commands) == 0 || !strings.Contains(plan.Commands[0], tc.cmd+" web") {
			t.Errorf("state %s: Commands = %v, want one containing %q", tc.state, plan.Commands, tc.cmd+" web")
		}
	}
}

// TestPlanTogglePresentInSyncWhenEnabled covers the happy path: present desired
// and the probe reports enabled means no change.
func TestPlanTogglePresentInSyncWhenEnabled(t *testing.T) {
	probe := func(ToggleContext) (bool, error) { return true, nil }
	plan := planToggle(StatePresent, "web", "checks:enable", "checks:disable", probe)
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Errorf("present+enabled: InSync=%v Status=%q, want InSync=true Status=%q", plan.InSync, plan.Status, PlanStatusOK)
	}
}

// TestPlanToggleAbsentInSyncWhenDisabled covers the happy path: absent desired
// and the probe reports disabled means no change.
func TestPlanToggleAbsentInSyncWhenDisabled(t *testing.T) {
	probe := func(ToggleContext) (bool, error) { return false, nil }
	plan := planToggle(StateAbsent, "web", "checks:enable", "checks:disable", probe)
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Errorf("absent+disabled: InSync=%v Status=%q, want InSync=true Status=%q", plan.InSync, plan.Status, PlanStatusOK)
	}
}

// TestPlanTogglePresentDriftTargetsApp locks the #322 fix: with the global
// machinery removed, a drift always targets the app and never a --global scope.
func TestPlanTogglePresentDriftTargetsApp(t *testing.T) {
	probe := func(ToggleContext) (bool, error) { return false, nil }
	plan := planToggle(StatePresent, "web", "checks:enable", "checks:disable", probe)
	if plan.InSync {
		t.Fatal("expected drift when probe reports disabled and present is desired")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) == 0 || !strings.Contains(plan.Commands[0], "checks:enable web") {
		t.Errorf("Commands = %v, want one containing %q", plan.Commands, "checks:enable web")
	}
	for _, cmd := range plan.Commands {
		if strings.Contains(cmd, "--global") {
			t.Errorf("command must target the app, not --global, got %q", cmd)
		}
	}
}
