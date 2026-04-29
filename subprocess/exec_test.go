package subprocess

import (
	"bytes"
	"context"
	"log"
	"strings"
	"testing"
	"time"
)

func TestResolveCommandString(t *testing.T) {
	t.Cleanup(func() { SetGlobalSensitive(nil) })
	t.Cleanup(func() { SetDefaultHost("") })

	tests := []struct {
		name      string
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
			name:  "sudo wraps the command and args",
			input: ExecCommandInput{Command: "dokku", Args: []string{"apps:create", "api"}, Sudo: true},
			want:  "sudo -n -u root dokku apps:create api",
		},
		{
			name:      "sensitive values are masked",
			input:     ExecCommandInput{Command: "dokku", Args: []string{"config:set", "api", "KEY=topsecret"}},
			sensitive: []string{"topsecret"},
			want:      "dokku config:set api KEY=***",
		},
		{
			name:  "ssh transport returns the bare form even with Sudo set",
			input: ExecCommandInput{Command: "dokku", Args: []string{"apps:create", "api"}, Host: "alice@host", Sudo: true},
			want:  "dokku apps:create api",
		},
		{
			name:  "non-dokku command runs locally even with Host set",
			input: ExecCommandInput{Command: "echo", Args: []string{"hi"}, Host: "alice@host"},
			want:  "echo hi",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			SetGlobalSensitive(tc.sensitive)
			t.Cleanup(func() { SetGlobalSensitive(nil) })
			if got := ResolveCommandString(tc.input); got != tc.want {
				t.Errorf("ResolveCommandString = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestResolveCommandStringHonorsDefaultHost(t *testing.T) {
	t.Cleanup(func() { SetDefaultHost("") })
	SetDefaultHost("alice@host")
	got := ResolveCommandString(ExecCommandInput{Command: "dokku", Args: []string{"apps:create", "api"}, Sudo: true})
	want := "dokku apps:create api"
	if got != want {
		t.Errorf("ResolveCommandString with default host = %q, want %q", got, want)
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
	resp, err := CallExecCommand(ExecCommandInput{
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
	resp, err := CallExecCommand(ExecCommandInput{
		Command: "false",
	})
	if err == nil {
		t.Fatal("expected error for failing command")
	}
	if resp.ExitCode == 0 {
		t.Error("expected non-zero exit code")
	}
}

func TestCallExecCommandNotFound(t *testing.T) {
	_, err := CallExecCommand(ExecCommandInput{
		Command: "nonexistent-binary-docket-test-12345",
	})
	if err == nil {
		t.Fatal("expected error for nonexistent command")
	}
}

func TestCallExecCommandWithEnv(t *testing.T) {
	resp, err := CallExecCommand(ExecCommandInput{
		Command: "env",
		Env:     map[string]string{"DOCKET_TEST_VAR": "test123"},
	})
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

	_, err := CallExecCommandWithContext(ctx, ExecCommandInput{
		Command: "sleep",
		Args:    []string{"10"},
	})
	if err == nil {
		t.Fatal("expected error from cancelled context")
	}
}

func TestCallExecCommandResponseCommandIsMasked(t *testing.T) {
	SetGlobalSensitive([]string{"topsecret123"})
	defer SetGlobalSensitive(nil)

	resp, err := CallExecCommand(ExecCommandInput{
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
	SetGlobalSensitive([]string{"topsecret123"})
	defer SetGlobalSensitive(nil)

	var buf bytes.Buffer
	prev := log.Writer()
	log.SetOutput(&buf)
	defer log.SetOutput(prev)

	if _, err := CallExecCommand(ExecCommandInput{
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
	SetGlobalSensitive(nil)

	resp, err := CallExecCommand(ExecCommandInput{
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
