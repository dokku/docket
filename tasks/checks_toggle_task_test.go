package tasks

import (
	"context"
	"errors"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestChecksToggleTaskInvalidState(t *testing.T) {
	task := ChecksToggleTask{App: "test-app", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

// TestChecksToggleTaskPlanSurfacesSSHError proves the checksEnabled probe
// forwards an SSH transport failure so planToggle reports it as a plan error
// rather than spurious drift (#357).
func TestChecksToggleTaskPlanSurfacesSSHError(t *testing.T) {
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 255}, &subprocess.SSHError{
			Host:   "dokku@unreachable",
			Stderr: "ssh: connect to host unreachable port 22: Connection refused",
		}
	})()

	plan := ChecksToggleTask{App: "web", State: StatePresent}.Plan(testCtx())
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

// TestGetTasksChecksToggleTaskParsedCorrectly decodes a toggle task from a
// recipe rather than building it in Go, which is the one thing the sibling
// tests here do not do. ChecksToggleTask is declared as `type ChecksToggleTask
// ToggleFields` (#467), so this is what proves the shared field set still yields
// the flat `app` / `state` recipe keys and that SetDefaults still fills `state`.
func TestGetTasksChecksToggleTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: enable checks
      dokku_checks_toggle:
        app: node-js-app
`)

	tasks, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("enable checks")
	if task == nil {
		t.Fatal("task 'enable checks' not found")
	}

	ctTask, ok := task.(*ChecksToggleTask)
	if !ok {
		t.Fatalf("task is not a ChecksToggleTask (type is %T)", task)
	}

	if ctTask.App != "node-js-app" {
		t.Errorf("App = %q, want %q", ctTask.App, "node-js-app")
	}
	if ctTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", ctTask.State)
	}
}
