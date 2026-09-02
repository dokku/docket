package subprocess

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"sync"

	execute "github.com/alexellis/go-execute/v2"
	"github.com/fatih/color"
)

// ExecCommandInput is the input for the ExecCommand function
type ExecCommandInput struct {
	// Command is the command to execute
	Command string

	// Args are the arguments to pass to the command
	Args []string

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
// would carry if input were executed under ctx now. Used by tasks' Plan() methods
// so PlanResult.Commands renders byte-identical to the strings apply emits via
// state.Commands; sharing the rendering logic keeps the two views from drifting.
//
// The function mirrors the dispatch in defaultExecRunner, and reads the target
// from the same place, so plan and apply cannot disagree about where a command
// was going. When the target names a host and the command is `dokku`, the SSH
// transport's bare `cmd args` form is returned: remote sudo is wrapped
// server-side and never appears in the displayed command. Otherwise the local
// form is returned, sudo-wrapped when the target asks for it.
func ResolveCommandString(ctx context.Context, input ExecCommandInput) string {
	target := TargetFromContext(ctx)
	if target.Host != "" && input.Command == "dokku" {
		return resolveSshCommandString(MaskerFromContext(ctx), input.Command, input.Args)
	}
	cmd := input.Command
	args := input.Args
	if target.Sudo && input.Command == "dokku" {
		args = append([]string{"-n", "-u", "root", cmd}, args...)
		cmd = "sudo"
	}
	return resolveLocalCommandString(MaskerFromContext(ctx), cmd, args)
}

// resolveLocalCommandString joins a local command and args into the masked
// form CallExecCommand records on the response.
func resolveLocalCommandString(m *Masker, command string, args []string) string {
	if len(args) == 0 {
		return m.String(command)
	}
	return m.String(command + " " + strings.Join(args, " "))
}

// resolveSshCommandString renders the bare `cmd arg1 arg2 ...` form the SSH
// transport reports (sudo wrapping happens remotely and is not displayed).
func resolveSshCommandString(m *Masker, command string, args []string) string {
	if len(args) == 0 {
		return m.String(command)
	}
	return m.String(command + " " + strings.Join(args, " "))
}

// ExecRunner is the shape of the executor CallExecCommand delegates to. A test
// supplies one to return canned responses without spawning a process or
// contacting a server.
type ExecRunner func(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error)

// runnerKey is the context key a per-invocation ExecRunner is stored under.
type runnerKey struct{}

// ContextWithRunner returns a copy of ctx whose dokku commands go to fn
// instead of to a real process. It is the per-invocation form of
// SetExecRunner: because the executor travels on the context rather than in a
// package variable, two tests can install different fakes at the same time and
// so can call t.Parallel().
//
// Production code must not use it. The seam exists so a test can drive a task
// without a server; a caller that wants to change where commands actually run
// sets a Target.
func ContextWithRunner(ctx context.Context, fn ExecRunner) context.Context {
	return context.WithValue(ctx, runnerKey{}, fn)
}

// runnerFromContext returns the executor for this call: the context's, when it
// carries one, else the package-level fallback.
func runnerFromContext(ctx context.Context) ExecRunner {
	if ctx != nil {
		if fn, ok := ctx.Value(runnerKey{}).(ExecRunner); ok && fn != nil {
			return fn
		}
	}
	return globalExecRunner()
}

// execRunner is the package-level fallback for calls whose context carries no
// runner. It defaults to defaultExecRunner (the real implementation) and can be
// swapped in tests via SetExecRunner. Production code must never reassign it.
//
// The mutex is not for correctness under normal use - production never writes
// it - but so that a test which does write it cannot race a concurrent test
// reading it, which is otherwise a data race the moment anything runs in
// parallel. Prefer ContextWithRunner in new tests; this exists for the ones
// written before the seam moved onto the context.
var (
	execRunnerMu sync.RWMutex
	execRunner   ExecRunner = defaultExecRunner
)

func globalExecRunner() ExecRunner {
	execRunnerMu.RLock()
	defer execRunnerMu.RUnlock()
	return execRunner
}

// SetExecRunner swaps the package-level executor and returns a function that
// restores the previous one. Intended for tests:
//
//	defer subprocess.SetExecRunner(fake)()
//
// It replaces process-wide state, so a test using it still cannot call
// t.Parallel(); ContextWithRunner is the form that can.
func SetExecRunner(fn ExecRunner) func() {
	execRunnerMu.Lock()
	prev := execRunner
	execRunner = fn
	execRunnerMu.Unlock()
	return func() {
		execRunnerMu.Lock()
		execRunner = prev
		execRunnerMu.Unlock()
	}
}

// CallExecCommand executes a command under ctx, locally or on the remote the
// context's Target names.
//
// When that target names a host and the command is `dokku`, dispatch is routed
// through the SSH transport so the dokku invocation runs on the remote host.
// Non-dokku subprocesses (echo/git/etc.) always run locally even when a host is
// configured, since the remote side may not have those binaries (and tests
// expect local execution).
//
// The context is the caller's: cancelling it kills the child process, and a
// deadline on it bounds the call. Nothing in this package installs a signal
// handler any more - the command layer wires SIGINT to the run context via
// signal.NotifyContext, so one interrupt aborts the run rather than the
// current task.
//
// The call is routed through a swappable executor so tests can substitute a
// fake; defaultExecRunner is the production path.
func CallExecCommand(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
	return runnerFromContext(ctx)(ctx, input)
}

// defaultExecRunner is the production executor: it runs the command locally, or
// routes dokku commands through the SSH transport when the context's target
// names a host.
func defaultExecRunner(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
	target := TargetFromContext(ctx)
	if target.Host != "" && input.Command == "dokku" {
		return CallSshCommand(ctx, target, input)
	}
	masker := MaskerFromContext(ctx)

	// isatty reports whether our own stdout is a terminal, which is the
	// signal used below to decide whether the child may read the terminal.
	isatty := !color.NoColor

	command := input.Command
	commandArgs := input.Args
	// Elevation is scoped to dokku for the same reason routing is: a task's
	// local helper commands (docker, curl, tar) are docket's own plumbing, and
	// requiring passwordless root for them because the operator asked for a
	// sudo-wrapped dokku would be a surprise.
	if target.Sudo && input.Command == "dokku" {
		commandArgs = append([]string{"-n", "-u", "root", command}, commandArgs...)
		command = "sudo"
	}

	// Env and Cwd are deliberately left unset: go-execute only overrides the
	// child's environment and directory when they are non-empty, so the child
	// inherits docket's own. Nothing is layered on top, because anything that
	// must influence a dokku invocation has to be on the argv - the only form
	// that survives the SSH transport.
	cmd := execute.ExecTask{
		Command: command,
		Args:    commandArgs,
	}

	if os.Getenv("DOKKU_TRACE") == "1" {
		argsSt := ""
		if len(cmd.Args) > 0 {
			argsSt = strings.Join(cmd.Args, " ")
		}
		log.Printf("exec: %s %s", masker.String(cmd.Command), masker.String(argsSt))
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

	resolved := resolveLocalCommandString(masker, command, commandArgs)

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
