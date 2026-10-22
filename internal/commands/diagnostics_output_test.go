package commands

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
	"github.com/dokku/docket/internal/tasks"
)

// The run stream renders a task's errors and warnings through
// tasks.Diagnostics (#647). These pin that it still prints exactly what it
// printed before, for each kind of error the helper classifies.
func TestRunStreamErrorRenderingIsUnchanged(t *testing.T) {
	t.Parallel()
	execRes := subprocess.ExecCommandResponse{Stdout: "step one\n", Stderr: "name is reserved\n", ExitCode: 1}
	cases := []struct {
		name     string
		err      error
		stdout   string
		stderr   string
		exitCode int
		text     string
	}{
		{
			name:     "failed command",
			err:      &subprocess.ExecError{Response: execRes, Err: errors.New(execRes.Stderr), Ran: true},
			stdout:   execRes.Stdout,
			stderr:   execRes.Stderr,
			exitCode: 1,
			text:     "dokku: name is reserved\n",
		},
		{
			name: "ssh failure",
			err:  &subprocess.SSHError{Host: "alice@host", Err: errors.New("ssh exited 255"), Stderr: "Permission denied (publickey).\n"},
			text: "ssh: ssh alice@host: Permission denied (publickey).",
		},
		{
			name: "version",
			err:  ignoreVersionHint(&tasks.VersionError{TaskType: "dokku_app", Minimum: "0.38.31", Found: "0.30.0"}),
			text: "dokku: dokku_app requires Dokku >= 0.38.31, server has 0.30.0 (pass --ignore-version to run it anyway)",
		},
		{
			name: "object missing",
			err:  &tasks.ObjectMissingError{Object: tasks.TaskObject{Kind: tasks.ObjectApp, Values: []string{"web"}}},
			text: "dokku: app web does not exist",
		},
		{
			name: "state unreadable",
			err:  fmt.Errorf("%w: no probe", tasks.ErrStateUnreadable),
			text: "dokku: the task's current state cannot be read: no probe",
		},
		{
			name: "generic",
			err:  errors.New("boom"),
			text: "dokku: boom",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			state := tasks.TaskOutputState{Error: tc.err, Stdout: tc.stdout, Stderr: tc.stderr, ExitCode: tc.exitCode, DesiredState: tasks.StatePresent}
			plan := tasks.PlanResult{Status: tasks.PlanStatusError, Error: tc.err, Stdout: tc.stdout, Stderr: tc.stderr, ExitCode: tc.exitCode, DesiredState: tasks.StatePresent}

			f, ui := newTestFormatter(false)
			f.PlanTask(PlanTaskEvent{Name: "web", Result: plan})
			if got, want := ui.ErrorWriter.String(), "[!]       web  ("+tc.text+")\n"; got != want {
				t.Errorf("plan line\ngot:  %q\nwant: %q", got, want)
			}

			f, ui = newTestFormatter(false)
			f.ApplyTask(ApplyTaskEvent{Name: "web", State: state})
			if got, want := ui.ErrorWriter.String(), "[error]   web\n"; got != want {
				t.Errorf("apply line\ngot:  %q\nwant: %q", got, want)
			}
			wantOut := ""
			for _, line := range strings.Split(strings.TrimRight(tc.text, "\n"), "\n") {
				wantOut += "          ! " + line + "\n"
			}
			for _, line := range strings.Split(strings.TrimRight(tc.stdout, "\n"), "\n") {
				if line != "" {
					wantOut += "          ! " + line + "\n"
				}
			}
			if got := ui.OutputWriter.String(); got != wantOut {
				t.Errorf("apply continuation\ngot:  %q\nwant: %q", got, wantOut)
			}

			e, jui := emitterTestUI()
			e.PlanTask(PlanTaskEvent{Play: "tasks", Name: "web", Result: plan})
			if ev := decodeOnly(t, jui.OutputWriter.String()); ev["error"] != tc.text {
				t.Errorf("plan JSON error = %q, want %q", ev["error"], tc.text)
			}

			e, jui = emitterTestUI()
			e.ApplyTask(ApplyTaskEvent{Play: "tasks", Name: "web", State: state})
			ev := decodeOnly(t, jui.OutputWriter.String())
			if ev["error"] != tc.text {
				t.Errorf("apply JSON error = %q, want %q", ev["error"], tc.text)
			}
			if got, _ := ev["exit_code"].(float64); int(got) != tc.exitCode {
				t.Errorf("apply JSON exit_code = %v, want %d", ev["exit_code"], tc.exitCode)
			}
			for key, want := range map[string]string{"stdout": tc.stdout, "stderr": tc.stderr} {
				got, present := ev[key]
				if want == "" {
					if present {
						t.Errorf("apply JSON %s = %q, want it absent", key, got)
					}
					continue
				}
				if got != want {
					t.Errorf("apply JSON %s = %q, want %q", key, got, want)
				}
			}
		})
	}
}

const diagnosticsMaskingRecipe = `---
- inputs:
    - { name: secret, required: true, sensitive: true }
  tasks:
    - name: "warn {{ .secret }}"
      dokku_stub: { key: warn }
    - name: "fail {{ .secret }}"
      dokku_stub: { key: fail }
`

// TestRunStreamMasksDiagnostics puts a declared secret (a sensitive input) and
// a secret the server returned (registered by the task while it runs) into
// every field of a failing task's result and of a warning, and checks that
// neither reaches plan or apply output, human or JSON.
func TestRunStreamMasksDiagnostics(t *testing.T) {
	t.Parallel()
	const declared = "declared-diag-s3cr3t"
	const served = "served-diag-t0ken"
	both := declared + " " + served

	res := subprocess.ExecCommandResponse{Stdout: "out " + both + "\n", Stderr: "err " + both + "\n", ExitCode: 1}
	failure := &subprocess.ExecError{Response: res, Err: errors.New("failed " + both), Ran: true}
	warnings := []tasks.PlanWarning{{Reason: tasks.WarnReasonProbeRejected, Message: "rejected " + both}}

	for _, mode := range []struct {
		name string
		run  func(*testing.T, string, ...string) (string, string, int)
		args []string
	}{
		{"plan", runPlan, nil},
		{"plan json", runPlan, []string{"--json"}},
		{"apply", runApply, nil},
		{"apply verbose", runApply, []string{"--verbose"}},
		{"apply json", runApply, []string{"--json"}},
	} {
		t.Run(mode.name, func(t *testing.T) {
			t.Parallel()
			stubSet(t, "warn", StubFixture{Changed: true, Warnings: warnings, ServerSecret: served})
			stubSet(t, "fail", StubFixture{
				PlanError:    failure,
				ExecuteError: failure,
				Stdout:       res.Stdout,
				Stderr:       res.Stderr,
				ExitCode:     1,
				ServerSecret: served,
			})
			path := writeTasksFile(t, diagnosticsMaskingRecipe)
			stdout, stderr, _ := mode.run(t, path, append([]string{"--secret=" + declared}, mode.args...)...)
			all := stdout + stderr
			for _, secret := range []string{declared, served} {
				if strings.Contains(all, secret) {
					t.Errorf("output leaks %q:\n%s", secret, all)
				}
			}
			for _, want := range []string{"rejected *** ***", "failed *** ***"} {
				if !strings.Contains(all, want) {
					t.Errorf("output should carry %q:\n%s", want, all)
				}
			}
		})
	}
}
