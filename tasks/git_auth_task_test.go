package tasks

import (
	"context"
	"errors"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestGitAuthTaskInvalidState(t *testing.T) {
	task := GitAuthTask{Host: "github.com", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGitAuthTaskMissingHost(t *testing.T) {
	for _, st := range []State{StatePresent, StateAbsent} {
		task := GitAuthTask{Username: "u", Password: "p", State: st}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("Execute without host (state=%s) should return an error", st)
		}
		if !strings.Contains(result.Error.Error(), "'host' is required") {
			t.Errorf("state=%s: unexpected error: %v", st, result.Error)
		}
	}
}

func TestGitAuthTaskPresentMissingUsername(t *testing.T) {
	task := GitAuthTask{Host: "github.com", Password: "p", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without username should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGitAuthTaskPresentMissingPassword(t *testing.T) {
	task := GitAuthTask{Host: "github.com", Username: "u", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without password should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'username' and 'password' are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksGitAuthTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: configure git auth
      dokku_git_auth:
        host: github.com
        username: deploy-bot
        password: ghp_examplepat
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("configure git auth")
	if task == nil {
		t.Fatal("task 'configure git auth' not found")
	}

	authTask, ok := task.(*GitAuthTask)
	if !ok {
		t.Fatalf("task is not a GitAuthTask (type is %T)", task)
	}
	if authTask.Host != "github.com" {
		t.Errorf("Host = %q, want %q", authTask.Host, "github.com")
	}
	if authTask.Username != "deploy-bot" {
		t.Errorf("Username = %q, want %q", authTask.Username, "deploy-bot")
	}
	if authTask.Password != "ghp_examplepat" {
		t.Errorf("Password = %q, want %q", authTask.Password, "ghp_examplepat")
	}
	if authTask.State != StatePresent {
		t.Errorf("State = %q, want %q", authTask.State, StatePresent)
	}
}

func TestGitAuthTaskPasswordWithNewline(t *testing.T) {
	task := GitAuthTask{Host: "github.com", Username: "u", Password: "line1\nline2", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with a multi-line password should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'password' must not contain a newline") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// TestGitAuthTaskRejectsNewlineBeforeProbing pins the point of the newline
// rule: dokku's `read -r` would stop at the first line, so the probe would
// compare a truncated password and the task would never converge. Validate()
// runs first, so the recipe is rejected before a secret leaves the machine.
func TestGitAuthTaskRejectsNewlineBeforeProbing(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(nil, &calls))

	plan := GitAuthTask{Host: "github.com", Username: "u", Password: "a\nb", State: StatePresent}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a plan error for a multi-line password")
	}
	if len(calls) != 0 {
		t.Errorf("expected no dokku calls, got %v", calls)
	}
}

// gitAuthDrifted stubs every dokku call as a clean non-zero exit, which is how
// subprocess.Probe reports git:auth-status saying "the stored entry is not what
// you handed me". fakeDokku always exits 0, so it can only express the in-sync
// case.
func gitAuthDrifted() func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 1}, nil
	}
}

func TestGitAuthTaskPresentInSync(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(nil))

	plan := GitAuthTask{
		Host:     "github.com",
		Username: "deploy-bot",
		Password: "ghp_examplepat",
		State:    StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("an in-sync plan must issue no commands, got %v", plan.Commands)
	}
}

func TestGitAuthTaskPresentDrifts(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), gitAuthDrifted())

	plan := GitAuthTask{
		Host:     "github.com",
		Username: "deploy-bot",
		Password: "ghp_examplepat",
		State:    StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want a modify", plan.InSync, plan.Status)
	}
	if plan.Reason != "netrc entry does not match" {
		t.Errorf("Reason = %q, want %q", plan.Reason, "netrc entry does not match")
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"git:auth github.com as deploy-bot"}) {
		t.Errorf("Mutations = %v, want [git:auth github.com as deploy-bot]", plan.Mutations)
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "git:auth github.com deploy-bot") {
		t.Fatalf("expected a single git:auth command, got %v", plan.Commands)
	}
	// The password rides on stdin, so neither the rendered command nor the
	// itemized mutations may carry it.
	for _, s := range append(append([]string{}, plan.Commands...), plan.Mutations...) {
		if strings.Contains(s, "ghp_examplepat") {
			t.Errorf("password leaked into plan output: %q", s)
		}
	}
}

func TestGitAuthTaskAbsentInSync(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(nil))

	plan := GitAuthTask{Host: "github.com", State: StateAbsent}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("an in-sync plan must issue no commands, got %v", plan.Commands)
	}
}

func TestGitAuthTaskAbsentDrifts(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), gitAuthDrifted())

	plan := GitAuthTask{Host: "github.com", State: StateAbsent}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusDestroy {
		t.Fatalf("plan = {InSync:%v Status:%q}, want a destroy", plan.InSync, plan.Status)
	}
	if plan.Reason != "netrc entry present" {
		t.Errorf("Reason = %q, want %q", plan.Reason, "netrc entry present")
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "git:auth github.com") {
		t.Fatalf("expected a single git:auth command, got %v", plan.Commands)
	}
}

// TestGitAuthTaskProbeArgs pins the argv of both probe shapes. The present-state
// probe has to name the username for git:auth-status to compare anything, and
// it must stop there: a third positional would put the password in the dokku
// host's process table, which is the whole reason it goes over stdin.
func TestGitAuthTaskProbeArgs(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name string
		task GitAuthTask
		want string
	}{
		{
			name: "present compares the credentials",
			task: GitAuthTask{Host: "github.com", Username: "deploy-bot", Password: "ghp_examplepat", State: StatePresent},
			want: "--quiet git:auth-status github.com deploy-bot",
		},
		{
			name: "absent asks whether any entry exists",
			task: GitAuthTask{Host: "github.com", State: StateAbsent},
			want: "--quiet git:auth-status github.com",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(nil, &calls))

			tc.task.Plan(ctx)
			if len(calls) != 1 {
				t.Fatalf("expected exactly one probe call, got %v", calls)
			}
			if calls[0] != tc.want {
				t.Errorf("probe args = %q, want %q", calls[0], tc.want)
			}
		})
	}
}

// TestGitAuthTaskStreamsPasswordToProbeAndApply asserts the payload that never
// appears in plan output: resolveCommands ignores Stdin, so the only way to see
// what git:auth-status and git:auth receive is to read the reader off each exec
// input. Both readers are collected because they must be distinct - an
// io.Reader is single-use, so a probe sharing the apply's reader would leave
// the apply with an empty stream and dokku would fail on a missing password.
func TestGitAuthTaskStreamsPasswordToProbeAndApply(t *testing.T) {
	t.Parallel()
	var payloads []string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if in.Stdin != nil {
			b, err := io.ReadAll(in.Stdin)
			if err != nil {
				t.Fatalf("read stdin: %v", err)
			}
			payloads = append(payloads, string(b))
		}
		// A non-zero exit from the probe is what drives the plan on to the
		// apply, so both stdin payloads are observed in one Execute.
		return subprocess.ExecCommandResponse{ExitCode: 1}, nil
	})

	GitAuthTask{
		Host:     "github.com",
		Username: "deploy-bot",
		Password: "ghp_examplepat",
		State:    StatePresent,
	}.Execute(ctx)

	want := []string{"ghp_examplepat", "ghp_examplepat"}
	if !reflect.DeepEqual(payloads, want) {
		t.Errorf("stdin payloads = %q, want %q", payloads, want)
	}
}

// TestGitAuthTaskAbsentSendsNoStdin keeps the absent probe from handing dokku a
// password it has no username to compare it against, which would make
// git:auth-status fail on "Missing password" instead of answering.
func TestGitAuthTaskAbsentSendsNoStdin(t *testing.T) {
	t.Parallel()
	sawStdin := false
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if in.Stdin != nil {
			sawStdin = true
		}
		return subprocess.ExecCommandResponse{}, nil
	})

	GitAuthTask{Host: "github.com", Username: "deploy-bot", Password: "ghp_examplepat", State: StateAbsent}.Plan(ctx)
	if sawStdin {
		t.Error("the absent-state probe must not stream a password")
	}
}

// TestGitAuthTaskProbeTransportFailure keeps an unreachable host from reading
// as drift. subprocess.Probe only folds a command that ran and exited non-zero
// into a false verdict; an *SSHError has to reach the plan so it renders [!]
// rather than optimistically predicting a change.
func TestGitAuthTaskProbeTransportFailure(t *testing.T) {
	t.Parallel()
	for _, st := range []State{StatePresent, StateAbsent} {
		ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
			return subprocess.ExecCommandResponse{}, &subprocess.SSHError{Host: "dokku@example.com", Err: errors.New("connection refused")}
		})

		plan := GitAuthTask{
			Host:     "github.com",
			Username: "deploy-bot",
			Password: "ghp_examplepat",
			State:    st,
		}.Plan(ctx)
		if plan.Error == nil {
			t.Fatalf("state=%s: expected the transport failure to surface as a plan error", st)
		}
		if plan.Status != PlanStatusError {
			t.Errorf("state=%s: Status = %q, want %q", st, plan.Status, PlanStatusError)
		}
	}
}
