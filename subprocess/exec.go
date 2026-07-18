package subprocess

import (
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"context"

	execute "github.com/alexellis/go-execute/v2"
	"github.com/fatih/color"
)

// ExecCommandInput is the input for the ExecCommand function
type ExecCommandInput struct {
	// Command is the command to execute
	Command string

	// Args are the arguments to pass to the command
	Args []string

	// DisableStdioBuffer disables the stdio buffer
	DisableStdioBuffer bool

	// Env is the environment variables to pass to the command
	Env map[string]string

	// Stdin is the stdin of the command
	Stdin io.Reader

	// StreamStdio prints stdout and stderr directly to os.Stdout/err as
	// the command runs
	StreamStdio bool

	// StreamStdout prints stdout directly to os.Stdout as the command runs.
	StreamStdout bool

	// StreamStderr prints stderr directly to os.Stderr as the command runs.
	StreamStderr bool

	// StdoutWriter is the writer to write stdout to
	StdoutWriter io.Writer

	// StderrWriter is the writer to write stderr to
	StderrWriter io.Writer

	// Sudo runs the command with sudo -n -u root
	Sudo bool

	// WorkingDirectory is the working directory to run the command in
	WorkingDirectory string

	// Host, when non-empty, routes the command through an `ssh` subprocess
	// against [user@]host[:port] instead of executing locally. Only used
	// when Command is "dokku"; non-dokku commands always run locally
	// (the remote side may not have those binaries). When empty, the
	// dispatcher consults the package default set by SetDefaultHost.
	Host string
}

// ExecError wraps a CallExecCommand failure with the underlying
// response so callers that propagate the error up the stack can later
// recover Stdout / Stderr / ExitCode without threading the response
// through every helper signature. The Error() method returns the
// inner error's text so existing callers that print err.Error() see
// the same string as before.
//
// Callers recover the response with errors.As:
//
//	var execErr *subprocess.ExecError
//	if errors.As(err, &execErr) {
//	    // execErr.Response.Stderr is available
//	}
type ExecError struct {
	Response ExecCommandResponse
	Err      error

	// Ran is true only when the command executed to completion and
	// Response.ExitCode is its real exit status. It is false when the
	// command could not be started (binary not found, permission denied)
	// or was cancelled, in which cases Response.ExitCode is not
	// meaningful. Probe() uses this to tell a dokku-level "state absent"
	// (Ran, non-zero exit) apart from a real execution failure that must
	// be propagated.
	Ran bool
}

// Error returns the wrapped error's message so existing string-based
// comparisons (e.g. fmt.Errorf wrapping) continue to work.
func (e *ExecError) Error() string {
	if e == nil || e.Err == nil {
		return ""
	}
	return e.Err.Error()
}

// Unwrap exposes the wrapped error so errors.Is / errors.As traverse
// chains of wrapped errors correctly.
func (e *ExecError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// ExecCommandResponse is the response for the ExecCommand function
type ExecCommandResponse struct {
	// Command is the resolved command line that was executed, including
	// any sudo wrapping and joined arguments. Used by callers that surface
	// the command in user-facing output (e.g. `docket apply --verbose`).
	Command string

	// Stdout is the stdout of the command
	Stdout string

	// Stderr is the stderr of the command
	Stderr string

	// ExitCode is the exit code of the command
	ExitCode int

	// Cancelled is whether the command was cancelled
	Cancelled bool
}

// StdoutContents returns the trimmed stdout of the command
func (ecr ExecCommandResponse) StdoutContents() string {
	return strings.TrimSpace(ecr.Stdout)
}

// StderrContents returns the trimmed stderr of the command
func (ecr ExecCommandResponse) StderrContents() string {
	return strings.TrimSpace(ecr.Stderr)
}

// StderrBytes returns the trimmed stderr of the command as bytes
func (ecr ExecCommandResponse) StderrBytes() []byte {
	return []byte(ecr.StderrContents())
}

// StdoutBytes returns the trimmed stdout of the command as bytes
func (ecr ExecCommandResponse) StdoutBytes() []byte {
	return []byte(ecr.StdoutContents())
}

// ResolveCommandString returns the masked command line ExecCommandResponse.Command
// would carry if input were executed now. Used by tasks' Plan() methods so
// PlanResult.Commands renders byte-identical to the strings apply emits via
// state.Commands; sharing the rendering logic keeps the two views from drifting.
//
// The function mirrors the dispatch in CallExecCommandWithContext and
// CallSshCommandWithContext: when the (possibly defaulted) Host is set and the
// command is `dokku`, the SSH transport's bare `cmd args` form is returned
// because remote sudo is wrapped server-side via DOKKU_SUDO and never appears
// in the displayed command. Otherwise the local path's sudo-wrap-when-true
// form is returned.
func ResolveCommandString(input ExecCommandInput) string {
	if input.Host == "" {
		input.Host = GetDefaultHost()
	}
	if input.Host != "" && input.Command == "dokku" {
		return resolveSshCommandString(input.Command, input.Args)
	}
	cmd := input.Command
	args := input.Args
	if input.Sudo {
		args = append([]string{"-n", "-u", "root", cmd}, args...)
		cmd = "sudo"
	}
	return resolveLocalCommandString(cmd, args)
}

// resolveLocalCommandString joins a local command and args into the masked
// form CallExecCommandWithContext records on the response.
func resolveLocalCommandString(command string, args []string) string {
	if len(args) == 0 {
		return MaskString(command)
	}
	return MaskString(command + " " + strings.Join(args, " "))
}

// resolveSshCommandString renders the bare `cmd arg1 arg2 ...` form the SSH
// transport reports (sudo wrapping happens remotely and is not displayed).
func resolveSshCommandString(command string, args []string) string {
	if len(args) == 0 {
		return MaskString(command)
	}
	return MaskString(command + " " + strings.Join(args, " "))
}

// CallExecCommand executes a command on the local host
func CallExecCommand(input ExecCommandInput) (ExecCommandResponse, error) {
	ctx := context.Background()
	return CallExecCommandWithContext(ctx, input)
}

// execRunner is the executor CallExecCommand and CallExecCommandWithContext
// delegate to. It defaults to defaultExecRunner (the real implementation) and
// can be swapped in tests via SetExecRunner so unit tests return canned
// responses without spawning a process or contacting a server. Production code
// must never reassign it.
//
// The swap mutates package state and is therefore test-only and not safe under
// t.Parallel().
var execRunner = defaultExecRunner

// SetExecRunner swaps the executor and returns a function that restores the
// previous one. Intended for tests:
//
//	defer subprocess.SetExecRunner(fake)()
func SetExecRunner(fn func(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error)) func() {
	prev := execRunner
	execRunner = fn
	return func() { execRunner = prev }
}

// CallExecCommandWithContext executes a command on the local host with the given context.
//
// When `input.Host` is set (or a default has been configured via
// SetDefaultHost) and the command is `dokku`, dispatch is routed
// through the SSH transport so the dokku invocation runs on the remote
// host. Non-dokku subprocesses (echo/git/etc.) always run locally even
// when a host is configured, since the remote side may not have those
// binaries (and tests expect local execution).
//
// The call is routed through the swappable execRunner so tests can substitute a
// fake executor; defaultExecRunner is the production path.
func CallExecCommandWithContext(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
	return execRunner(ctx, input)
}

// defaultExecRunner is the production executor: it runs the command locally, or
// routes dokku commands through the SSH transport when a host is configured.
func defaultExecRunner(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
	if input.Host == "" {
		input.Host = GetDefaultHost()
	}
	if input.Host != "" && input.Command == "dokku" {
		return CallSshCommandWithContext(ctx, input.Host, input)
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, os.Interrupt, syscall.SIGHUP,
		syscall.SIGINT,
		syscall.SIGQUIT,
		syscall.SIGTERM)
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		<-signals
		cancel()
	}()

	// hack: colors do not work natively with io.MultiWriter
	// as it isn't detected as a tty. If the output isn't
	// being captured, then color output can be forced.
	isatty := !color.NoColor
	env := os.Environ()
	if isatty && input.DisableStdioBuffer {
		env = append(env, "FORCE_TTY=1")
	}
	if input.Env != nil {
		for k, v := range input.Env {
			env = append(env, fmt.Sprintf("%s=%s", k, v))
		}
	}

	command := input.Command
	commandArgs := input.Args
	if input.Sudo {
		commandArgs = append([]string{"-n", "-u", "root", command}, commandArgs...)
		command = "sudo"
	}

	cmd := execute.ExecTask{
		Command:            command,
		Args:               commandArgs,
		Env:                env,
		DisableStdioBuffer: input.DisableStdioBuffer,
	}

	if input.WorkingDirectory != "" {
		cmd.Cwd = input.WorkingDirectory
	}

	if os.Getenv("DOKKU_TRACE") == "1" {
		argsSt := ""
		if len(cmd.Args) > 0 {
			argsSt = strings.Join(cmd.Args, " ")
		}
		log.Printf("exec: %s %s", MaskString(cmd.Command), MaskString(argsSt))
	}

	if input.Stdin != nil {
		cmd.Stdin = input.Stdin
	} else if isatty {
		cmd.Stdin = os.Stdin
	}

	if input.StreamStdio {
		cmd.StreamStdio = true
	}
	if input.StreamStdout {
		cmd.StdOutWriter = os.Stdout
	}
	if input.StreamStderr {
		cmd.StdErrWriter = os.Stderr
	}
	if input.StdoutWriter != nil {
		cmd.StdOutWriter = input.StdoutWriter
	}
	if input.StderrWriter != nil {
		cmd.StdErrWriter = input.StderrWriter
	}

	resolved := resolveLocalCommandString(command, commandArgs)

	res, err := cmd.Execute(ctx)
	if err != nil {
		// The command could not be run to completion: the binary was not
		// found, was not executable, or the context was cancelled. The
		// exit code is not meaningful, so Ran stays false and callers such
		// as Probe surface the failure instead of reading it as absence.
		response := ExecCommandResponse{
			Command:   resolved,
			Stdout:    res.Stdout,
			Stderr:    res.Stderr,
			ExitCode:  res.ExitCode,
			Cancelled: res.Cancelled,
		}
		return response, &ExecError{Response: response, Err: err}
	}

	if res.ExitCode != 0 {
		response := ExecCommandResponse{
			Command:   resolved,
			Stdout:    res.Stdout,
			Stderr:    res.Stderr,
			ExitCode:  res.ExitCode,
			Cancelled: res.Cancelled,
		}
		return response, &ExecError{Response: response, Err: errors.New(res.Stderr), Ran: true}
	}

	return ExecCommandResponse{
		Command:   resolved,
		Stdout:    res.Stdout,
		Stderr:    res.Stderr,
		ExitCode:  res.ExitCode,
		Cancelled: res.Cancelled,
	}, nil
}
