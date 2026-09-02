package subprocess

import (
	"bytes"
	"context"
	"errors"
	"log"
	"strings"
	"testing"
	"time"
)

func TestResolveCommandString(t *testing.T) {

	tests := []struct {
		name      string
		target    Target
		input     ExecCommandInput
		sensitive []string
		want      string
	}{
		{
			name:  "bare command, no args",
			input: ExecCommandInput{Command: "dokku"},
			want:  "dokku",
		},
		{
			name:  "command with args",
			input: ExecCommandInput{Command: "dokku", Args: []string{"--quiet", "apps:create", "api"}},
			want:  "dokku --quiet apps:create api",
		},
		{
			name:   "a sudo target wraps the local command and args",
			target: Target{Sudo: true},
			input:  ExecCommandInput{Command: "dokku", Args: []string{"apps:create", "api"}},
			want:   "sudo -n -u root dokku apps:create api",
		},
		{
			name:   "a sudo target leaves a non-dokku helper alone",
			target: Target{Sudo: true},
			input:  ExecCommandInput{Command: "docker", Args: []string{"image", "inspect", "api"}},
			want:   "docker image inspect api",
		},
		{
			name:      "sensitive values are masked",
			input:     ExecCommandInput{Command: "dokku", Args: []string{"config:set", "api", "KEY=topsecret"}},
			sensitive: []string{"topsecret"},
			want:      "dokku config:set api KEY=***",
		},
		{
			name:   "ssh transport returns the bare form even with sudo set",
			target: Target{Host: "alice@host", Sudo: true},
			input:  ExecCommandInput{Command: "dokku", Args: []string{"apps:create", "api"}},
			want:   "dokku apps:create api",
		},
		{
			name:   "non-dokku command runs locally even with a host set",
			target: Target{Host: "alice@host"},
			input:  ExecCommandInput{Command: "echo", Args: []string{"hi"}},
			want:   "echo hi",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctx := ContextWithTarget(context.Background(), tc.target)
			ctx = ContextWithMasker(ctx, NewMasker(tc.sensitive...))
			if got := ResolveCommandString(ctx, tc.input); got != tc.want {
				t.Errorf("ResolveCommandString = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestResolveCommandStringWithoutATargetRunsLocally pins the zero value: a
// context carrying no target renders the local form, which is what docket does
// when neither --host nor DOKKU_HOST is set.
func TestResolveCommandStringWithoutATargetRunsLocally(t *testing.T) {
	t.Parallel()
	got := ResolveCommandString(context.Background(), ExecCommandInput{
		Command: "dokku",
		Args:    []string{"apps:create", "api"},
	})
	if want := "dokku apps:create api"; got != want {
		t.Errorf("ResolveCommandString = %q, want %q", got, want)
	}
}

func TestExecCommandResponseStdoutContents(t *testing.T) {
	tests := []struct {
		name   string
		stdout string
		want   string
	}{
		{"trims whitespace", "  hello world  \n", "hello world"},
		{"empty string", "", ""},
		{"only whitespace", "   \n\t  ", ""},
		{"no trimming needed", "hello", "hello"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := ExecCommandResponse{Stdout: tt.stdout}
			if got := resp.StdoutContents(); got != tt.want {
				t.Errorf("StdoutContents() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecCommandResponseStderrContents(t *testing.T) {
	tests := []struct {
		name   string
		stderr string
		want   string
	}{
		{"trims whitespace", "  error message  \n", "error message"},
		{"empty string", "", ""},
		{"only whitespace", "   \n\t  ", ""},
		{"no trimming needed", "error", "error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := ExecCommandResponse{Stderr: tt.stderr}
			if got := resp.StderrContents(); got != tt.want {
				t.Errorf("StderrContents() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestExecCommandResponseStdoutBytes(t *testing.T) {
	resp := ExecCommandResponse{Stdout: "  hello world  \n"}
	got := resp.StdoutBytes()
	want := []byte("hello world")
	if !bytes.Equal(got, want) {
		t.Errorf("StdoutBytes() = %v, want %v", got, want)
	}

	empty := ExecCommandResponse{Stdout: ""}
	if got := empty.StdoutBytes(); len(got) != 0 {
		t.Errorf("StdoutBytes() for empty = %v, want empty", got)
	}
}

func TestExecCommandResponseStderrBytes(t *testing.T) {
	resp := ExecCommandResponse{Stderr: "  error msg  \n"}
	got := resp.StderrBytes()
	want := []byte("error msg")
	if !bytes.Equal(got, want) {
		t.Errorf("StderrBytes() = %v, want %v", got, want)
	}

	empty := ExecCommandResponse{Stderr: ""}
	if got := empty.StderrBytes(); len(got) != 0 {
		t.Errorf("StderrBytes() for empty = %v, want empty", got)
	}
}

func TestCallExecCommandSuccess(t *testing.T) {
	resp, err := CallExecCommand(context.Background(), ExecCommandInput{
		Command: "echo",
		Args:    []string{"hello"},
	})
	if err != nil {
		t.Fatalf("CallExecCommand failed: %v", err)
	}
	if resp.ExitCode != 0 {
		t.Errorf("ExitCode = %d, want 0", resp.ExitCode)
	}
	if !strings.Contains(resp.StdoutContents(), "hello") {
		t.Errorf("stdout = %q, want it to contain 'hello'", resp.StdoutContents())
	}
}

func TestCallExecCommandFailure(t *testing.T) {
	resp, err := CallExecCommand(context.Background(), ExecCommandInput{
		Command: "false",
	})
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if resp.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
	// The command ran and exited non-zero, so the exit code is real:
	// ExecError.Ran must be true so Probe reads it as "state absent".
	var execErr *ExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *ExecError, got %T", err)
	}
	if !execErr.Ran {
		t.Error("ExecError.Ran should be true when the command ran and exited non-zero")
	}
}

func TestCallExecCommandNotFound(t *testing.T) {
	_, err := CallExecCommand(context.Background(), ExecCommandInput{
		Command: "nonexistent-binary-docket-test-12345",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
	// The command could not be started, so there is no real exit code:
	// ExecError.Ran must be false so Probe propagates the failure instead
	// of reporting the probed state as absent.
	var execErr *ExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("expected *ExecError, got %T", err)
	}
	if execErr.Ran {
		t.Error("ExecError.Ran should be false when the binary is not found")
	}
}

// TestCallExecCommandInheritsProcessEnv locks the environment contract that
// remains now that ExecCommandInput has no Env field: the child gets docket's
// own environment, and nothing is layered on top. The inheritance itself is
// implicit - go-execute leaves cmd.Env nil when ExecTask.Env is empty - so it
// is worth asserting rather than assuming.
func TestCallExecCommandInheritsProcessEnv(t *testing.T) {
	t.Setenv("DOCKET_TEST_VAR", "test123")

	resp, err := CallExecCommand(context.Background(), ExecCommandInput{Command: "env"})
	if err != nil {
		t.Fatalf("CallExecCommand failed: %v", err)
	}
	if !strings.Contains(resp.StdoutContents(), "DOCKET_TEST_VAR=test123") {
		t.Errorf("stdout = %q, want it to contain 'DOCKET_TEST_VAR=test123'", resp.StdoutContents())
	}
}

func TestCallExecCommandWithContext(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := CallExecCommand(ctx, ExecCommandInput{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

// TestContextRunnerReceivesInputAndFallsBackToTheReal covers both halves of
// runnerFromContext: a context carrying a runner routes to it with the input
// intact, and one carrying none reaches the real executor. It replaces
// TestSetExecRunnerSwapsAndRestores, which pinned the same two halves against
// the package-level setter #502 removed.
func TestContextRunnerReceivesInputAndFallsBackToTheReal(t *testing.T) {
	t.Parallel()
	var gotInput ExecCommandInput
	ctx := ContextWithRunner(context.Background(), func(_ context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
		gotInput = input
		return ExecCommandResponse{Stdout: "canned"}, nil
	})

	// The fake answers without spawning a process (the command below does
	// not exist on PATH).
	resp, err := CallExecCommand(ctx, ExecCommandInput{Command: "dokku", Args: []string{"apps:list"}})
	if err != nil {
		t.Fatalf("CallExecCommand with fake runner failed: %v", err)
	}
	if resp.Stdout != "canned" {
		t.Errorf("expected canned stdout, got %q", resp.Stdout)
	}
	if gotInput.Command != "dokku" || len(gotInput.Args) != 1 || gotInput.Args[0] != "apps:list" {
		t.Errorf("fake runner did not receive the input: %+v", gotInput)
	}

	// A bare context reaches the real executor: echo succeeds locally.
	resp, err = CallExecCommand(context.Background(), ExecCommandInput{Command: "echo", Args: []string{"hi"}})
	if err != nil {
		t.Fatalf("CallExecCommand without a context runner failed: %v", err)
	}
	if resp.StdoutContents() != "hi" {
		t.Errorf("expected real executor output %q, got %q", "hi", resp.StdoutContents())
	}
}

func TestCallExecCommandResponseCommandIsMasked(t *testing.T) {
	masker := NewMasker("topsecret123")

	resp, err := CallExecCommand(ContextWithMasker(context.Background(), masker), ExecCommandInput{
		Command: "echo",
		Args:    []string{"login", "topsecret123"},
	})
	if err != nil {
		t.Fatalf("CallExecCommand failed: %v", err)
	}
	if strings.Contains(resp.Command, "topsecret123") {
		t.Errorf("response.Command leaked secret: %q", resp.Command)
	}
	if !strings.Contains(resp.Command, "***") {
		t.Errorf("response.Command did not mask: %q", resp.Command)
	}
	// Stdout still contains the actual value because the subprocess
	// receives the unmasked args - masking only applies to display.
	if !strings.Contains(resp.StdoutContents(), "topsecret123") {
		t.Errorf("subprocess should have received unmasked args; stdout = %q", resp.StdoutContents())
	}
}

func TestCallExecCommandTraceLogIsMasked(t *testing.T) {
	t.Setenv("DOKKU_TRACE", "1")
	masker := NewMasker("topsecret123")

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	if _, err := CallExecCommand(ContextWithMasker(context.Background(), masker), ExecCommandInput{
		Command: "echo",
		Args:    []string{"login", "topsecret123"},
	}); err != nil {
		t.Fatalf("CallExecCommand failed: %v", err)
	}
	got := buf.String()
	if strings.Contains(got, "topsecret123") {
		t.Errorf("DOKKU_TRACE log leaked secret: %q", got)
	}
	if !strings.Contains(got, "***") {
		t.Errorf("DOKKU_TRACE log did not mask: %q", got)
	}
}

func TestCallExecCommandResponseCommandUnmaskedWhenNoSecrets(t *testing.T) {

	resp, err := CallExecCommand(context.Background(), ExecCommandInput{
		Command: "echo",
		Args:    []string{"hello", "world"},
	})
	if err != nil {
		t.Fatalf("CallExecCommand failed: %v", err)
	}
	if !strings.Contains(resp.Command, "hello") || !strings.Contains(resp.Command, "world") {
		t.Errorf("response.Command unexpectedly altered: %q", resp.Command)
	}
}
