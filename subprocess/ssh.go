// Package subprocess SSH transport.
//
// When DOKKU_HOST is set, every dokku subprocess invocation is routed
// through an `ssh` subprocess wrapper instead of executing locally. We
// shell out to the user's `ssh` binary rather than using a Go SSH
// library so we inherit the user's `~/.ssh/config`, `ProxyJump`, agent,
// and known_hosts handling for free.
//
// All invocations in a single docket run share one TCP+SSH handshake
// via OpenSSH ControlMaster multiplexing. The first `ssh` invocation
// negotiates the master connection and writes a unix-domain socket at
// `<tmpdir>/docket-<hash>.sock`; subsequent invocations reuse it. The
// socket name hashes the resolved host plus the docket PID so two
// docket processes targeting the same host do not collide on the
// socket path.
//
// The ControlPersist option keeps the master alive 60 seconds past the
// last command exit; the command package additionally invokes
// CloseSshControlMaster as a defer to tear the master down cleanly when
// the run exits normally.
//
// Error attribution. OpenSSH exits with code 255 when the transport
// itself fails (connect refused, auth, host-key mismatch) and forwards
// the remote command's exit code otherwise. We use exit 255 to classify
// failures: a 255 exit is wrapped as `*SSHError` so the formatter can
// render it with an `ssh:` prefix; any other non-zero exit is returned
// as the underlying error so the formatter renders it as a `dokku:`
// failure.
//
// `input.Sudo` in `ExecCommandInput` means "wrap with local sudo" and
// is meaningless when DOKKU_HOST is set. SSH dispatch ignores
// `input.Sudo` and consults `DOKKU_SUDO=1` to decide whether to wrap
// the *remote* dokku invocation in `sudo -n`.
package subprocess

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"net/url"
	"os"
	"os/exec"
	"os/signal"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"

	execute "github.com/alexellis/go-execute/v2"
	"github.com/fatih/color"
)

// SSHError wraps a transport-level failure of the ssh subprocess
// (connect, auth, host-key) as opposed to a non-zero exit from the
// remote dokku command. The output formatter renders SSHError values
// with an `ssh:` prefix; all other errors render with a `dokku:`
// prefix.
type SSHError struct {
	Host    string
	Command []string
	Err     error
	Stderr  string
}

func (e *SSHError) Error() string {
	if e == nil {
		return ""
	}
	stderr := strings.TrimSpace(e.Stderr)
	if stderr != "" {
		return fmt.Sprintf("ssh %s: %s", e.Host, stderr)
	}
	if e.Err != nil {
		return fmt.Sprintf("ssh %s: %s", e.Host, e.Err)
	}
	return fmt.Sprintf("ssh %s: transport failure", e.Host)
}

func (e *SSHError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// sshTarget is the parsed form of DOKKU_HOST.
type sshTarget struct {
	User string
	Host string
	Port string
}

// UserHost returns the [user@]host portion suitable for passing to ssh.
func (t sshTarget) UserHost() string {
	if t.User == "" {
		return t.Host
	}
	return t.User + "@" + t.Host
}

// parseDokkuHost parses a DOKKU_HOST value of the form `[user@]host[:port]`.
// We prepend `ssh://` and use net/url so port and IPv6 hosts get parsed
// correctly. An empty user defaults to $USER (then $LOGNAME, then
// user.Current); an empty port defaults to "22".
func parseDokkuHost(raw string) (sshTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sshTarget{}, errors.New("DOKKU_HOST is empty")
	}
	u, err := url.Parse("ssh://" + raw)
	if err != nil {
		return sshTarget{}, fmt.Errorf("invalid DOKKU_HOST %q: %w", raw, err)
	}
	host := u.Hostname()
	if host == "" {
		return sshTarget{}, fmt.Errorf("invalid DOKKU_HOST %q: no host", raw)
	}
	target := sshTarget{
		Host: host,
		Port: u.Port(),
	}
	if u.User != nil {
		target.User = u.User.Username()
	}
	if target.User == "" {
		target.User = defaultSshUser()
	}
	if target.Port == "" {
		target.Port = "22"
	}
	return target, nil
}

func defaultSshUser() string {
	if v := os.Getenv("USER"); v != "" {
		return v
	}
	if v := os.Getenv("LOGNAME"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// controlPath returns the unix-domain socket path used by ControlMaster
// for the given host and PID. Hashing the host + PID gives concurrent
// docket runs against the same host distinct sockets so they cannot
// collide.
func controlPath(host string, pid int) string {
	sum := sha256.Sum256([]byte(host + ":" + strconv.Itoa(pid)))
	return filepath.Join(os.TempDir(), "docket-"+hex.EncodeToString(sum[:])[:16]+".sock")
}

// buildSshArgv assembles the full argv for the `ssh` subprocess. The
// remote command is passed as separate argv elements; OpenSSH handles
// quoting so callers must not pre-quote.
//
// Reads two env vars at build time: DOKKU_SSH_ACCEPT_NEW_HOST_KEYS=1
// adds `-o StrictHostKeyChecking=accept-new`; DOKKU_SUDO=1 wraps the
// remote command in `sudo -n`.
func buildSshArgv(target sshTarget, remote []string) []string {
	argv := []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath(target.UserHost(), os.Getpid()),
		"-o", "ControlPersist=60",
		"-o", "BatchMode=yes",
	}
	if os.Getenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS") == "1" {
		argv = append(argv, "-o", "StrictHostKeyChecking=accept-new")
	}
	if target.Port != "" && target.Port != "22" {
		argv = append(argv, "-p", target.Port)
	}
	argv = append(argv, target.UserHost(), "--")
	if os.Getenv("DOKKU_SUDO") == "1" {
		argv = append(argv, "sudo", "-n")
	}
	argv = append(argv, remote...)
	return argv
}

// sshLookPathOnce caches the result of looking up the `ssh` binary so
// we don't pay LookPath on every dispatch.
var (
	sshLookPathOnce sync.Once
	sshLookPathErr  error
)

// defaultHost is the package-level fallback used by CallExecCommand
// when the per-call ExecCommandInput.Host is empty. The commands layer
// (commands/apply.go and commands/plan.go) sets this once at start-of-
// run from the resolved CLI flag / env var so tasks can keep building
// transport-agnostic ExecCommandInput values.
var (
	defaultHostMu sync.RWMutex
	defaultHost   string
)

// Probe runs input as a state probe and reports whether it matched
// (exit 0). A dokku-level non-zero exit is reported as `(false, nil)`,
// i.e. "the probed state is absent," so callers can write idempotent
// probes without unwrapping errors themselves. A transport-level
// failure (`*SSHError`) is propagated as `(false, err)` so the caller
// can short-circuit `Plan()` with `PlanResult{Error: err}` and let the
// formatter render `! ssh: ...`.
//
// Use this for any plan-time probe that today reads exit code only
// (`apps:exists`, `network:exists`, `<service>:linked`, etc.). Probes
// that need stdout should call CallExecCommand directly and use
// `errors.As(err, &*SSHError)` to discriminate.
func Probe(input ExecCommandInput) (bool, error) {
	result, err := CallExecCommand(input)
	if err != nil {
		var sshErr *SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		return false, nil
	}
	return result.ExitCode == 0, nil
}

// SetDefaultHost registers the host that CallExecCommandWithContext
// should use when ExecCommandInput.Host is empty. Pass an empty string
// to clear the default. Mirrors the SetGlobalSensitive pattern.
func SetDefaultHost(host string) {
	defaultHostMu.Lock()
	defer defaultHostMu.Unlock()
	defaultHost = host
}

// GetDefaultHost returns the currently registered default host (or
// empty string when none).
func GetDefaultHost() string {
	defaultHostMu.RLock()
	defer defaultHostMu.RUnlock()
	return defaultHost
}

func ensureSshAvailable() error {
	sshLookPathOnce.Do(func() {
		_, err := exec.LookPath("ssh")
		if err != nil {
			sshLookPathErr = errors.New("ssh binary not found in PATH; install OpenSSH client to use DOKKU_HOST")
		}
	})
	return sshLookPathErr
}

// CallSshCommand executes a remote command over ssh against host using
// the background context.
func CallSshCommand(host string, input ExecCommandInput) (ExecCommandResponse, error) {
	return CallSshCommandWithContext(context.Background(), host, input)
}

// CallSshCommandWithContext executes a remote command over ssh against
// host. The execution pipeline mirrors CallExecCommandWithContext (same
// signal handling, env propagation, DOKKU_TRACE logging, masking, stdio
// wiring) so callers see identical behavior aside from the transport.
//
// On exit code 255 (OpenSSH's transport-failure code), the returned
// error is `*SSHError`. On any other non-zero exit, the returned error
// is the plain underlying error so the formatter renders the failure
// as a remote dokku error.
func CallSshCommandWithContext(ctx context.Context, host string, input ExecCommandInput) (ExecCommandResponse, error) {
	target, err := parseDokkuHost(host)
	if err != nil {
		return ExecCommandResponse{}, &SSHError{Host: host, Err: err}
	}
	if err := ensureSshAvailable(); err != nil {
		return ExecCommandResponse{}, &SSHError{Host: target.UserHost(), Err: err}
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

	remote := append([]string{input.Command}, input.Args...)
	argv := buildSshArgv(target, remote)

	cmd := execute.ExecTask{
		Command:            "ssh",
		Args:               argv,
		Env:                env,
		DisableStdioBuffer: input.DisableStdioBuffer,
	}
	if input.WorkingDirectory != "" {
		cmd.Cwd = input.WorkingDirectory
	}

	if os.Getenv("DOKKU_TRACE") == "1" {
		log.Printf("ssh: %s %s", MaskString("ssh"), MaskString(strings.Join(argv, " ")))
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

	resolved := resolveSshCommandString(input.Command, input.Args)

	res, runErr := cmd.Execute(ctx)
	resp := ExecCommandResponse{
		Command:   resolved,
		Stdout:    res.Stdout,
		Stderr:    res.Stderr,
		ExitCode:  res.ExitCode,
		Cancelled: res.Cancelled,
	}

	return classifySshResult(target, remote, resp, runErr)
}

// classifySshResult maps an ssh ExecTask result onto the docket error
// model. Exit 255 (and any error before the process started) is wrapped
// as *SSHError. Any other non-zero exit returns a plain error built
// from stderr so the existing dokku-error rendering keeps working.
func classifySshResult(target sshTarget, remote []string, resp ExecCommandResponse, runErr error) (ExecCommandResponse, error) {
	if runErr != nil {
		return resp, &SSHError{
			Host:    target.UserHost(),
			Command: remote,
			Err:     runErr,
			Stderr:  resp.Stderr,
		}
	}
	if resp.ExitCode == 255 {
		return resp, &SSHError{
			Host:    target.UserHost(),
			Command: remote,
			Err:     errors.New("ssh exited 255"),
			Stderr:  resp.Stderr,
		}
	}
	if resp.ExitCode != 0 {
		return resp, errors.New(resp.Stderr)
	}
	return resp, nil
}

// CloseSshControlMaster sends `ssh -O exit` to the ControlMaster for
// host so the multiplexed connection is torn down cleanly. Best-effort:
// errors are swallowed because the master may already have exited
// (ControlPersist timeout, kill -9, etc.). Intended to be called as a
// `defer` from command run loops.
func CloseSshControlMaster(host string) error {
	target, err := parseDokkuHost(host)
	if err != nil {
		return nil
	}
	if _, err := exec.LookPath("ssh"); err != nil {
		return nil
	}
	socket := controlPath(target.UserHost(), os.Getpid())
	if _, err := os.Stat(socket); err != nil {
		return nil
	}
	cmd := exec.Command("ssh",
		"-o", "ControlPath="+socket,
		"-O", "exit",
		target.UserHost(),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
	return nil
}
