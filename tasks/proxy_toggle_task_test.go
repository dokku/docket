package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestProxyToggleTaskInvalidState(t *testing.T) {
	task := ProxyToggleTask{App: "test-app", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

// TestProxyToggleTaskPlanSurfacesSSHError proves the proxyEnabled probe
// forwards an SSH transport failure so planToggle reports it as a plan error
// rather than spurious drift (#357).
func TestProxyToggleTaskPlanSurfacesSSHError(t *testing.T) {
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 255}, &subprocess.SSHError{
			Host:   "dokku@unreachable",
			Stderr: "ssh: connect to host unreachable port 22: Connection refused",
		}
	})()

	plan := ProxyToggleTask{App: "web", State: StateAbsent}.Plan()
	if plan.Status != PlanStatusError {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusError)
	}
	if plan.InSync {
		t.Error("expected InSync=false on transport failure")
	}
	var sshErr *subprocess.SSHError
	if !errors.As(plan.Error, &sshErr) {
		t.Errorf("Error = %v, want *subprocess.SSHError", plan.Error)
	}
}
