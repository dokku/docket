// Package subprocess SSH transport.
//
// When the context's Target names a host - resolved by the commands layer from
// --host or DOKKU_HOST - every dokku subprocess invocation is routed through an
// `ssh` subprocess wrapper instead of executing locally. We shell out to the
// user's `ssh` binary rather than using a Go SSH library so we inherit the
// user's `~/.ssh/config`, `ProxyJump`, agent, and known_hosts handling for
// free.
//
// All invocations in a single docket run share one TCP+SSH handshake
// via OpenSSH ControlMaster multiplexing. The first `ssh` invocation
// negotiates the master connection and writes a unix-domain socket at
// `<tmpdir>/docket-<hash>.sock`; subsequent invocations reuse it. The
// socket name hashes the resolved user, host and port, the docket PID, and
// the Session the call ran under, so neither two docket processes nor two
// sessions in one process targeting the same host collide on the socket
// path.
//
// The ControlPersist option keeps the master alive 60 seconds past the
// last command exit. A Session records every master its calls opened and
// tears them down on Close, which is how the commands layer ends a run.
//
// Error attribution. OpenSSH exits with code 255 when the transport
// itself fails (connect refused, auth, host-key mismatch) and forwards
// the remote command's exit code otherwise. We use exit 255 to classify
// failures: a 255 exit is wrapped as `*SSHError` so the formatter can
// render it with an `ssh:` prefix; any other non-zero exit is returned
// as the underlying error so the formatter renders it as a `dokku:`
// failure.
//
// Where a command runs, whether it is sudo-wrapped, and whether an unknown
// host key is accepted all travel on the context as a `Target`. SSH dispatch
// reads it the same way the local path does, so `--sudo` means "run dokku as
// root" on both sides - remotely as `sudo -n` inside the ssh argv, locally as
// `sudo -n -u root` around the child.
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
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"

	execute "github.com/alexellis/go-execute/v2"
	"mvdan.cc/sh/v3/syntax"
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

// sshTarget is the parsed form of a target host.
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

// parseDokkuHost parses a target host of the form `[user@]host[:port]`, as
// supplied by DOKKU_HOST, --host, or a play's own `host:` key.
// We prepend `ssh://` and use net/url so port and IPv6 hosts get parsed
// correctly. An empty user defaults to $USER (then $LOGNAME, then
// user.Current); an empty port defaults to "22".
//
// getenv is how those two variables are read - `os.Getenv` in production.
// Taking it rather than calling `os.Getenv` here is what lets the tests for
// the default state their own environment and still call t.Parallel(), which
// `t.Setenv` panics on.
func parseDokkuHost(raw string, getenv func(string) string) (sshTarget, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return sshTarget{}, errors.New("host is empty")
	}
	// A scheme is rejected rather than parsed. The value is joined onto
	// "ssh://" below, so `ssh://example.com` would become
	// `ssh://ssh://example.com` and yield the hostname "ssh" - connecting
	// somewhere the user plainly did not mean, with no error to say so.
	if strings.Contains(raw, "://") {
		return sshTarget{}, fmt.Errorf("invalid host %q: remove the scheme, the form is [user@]host[:port]", raw)
	}
	u, err := url.Parse("ssh://" + raw)
	if err != nil {
		return sshTarget{}, fmt.Errorf("invalid host %q: %w", raw, err)
	}
	host := u.Hostname()
	if host == "" {
		return sshTarget{}, fmt.Errorf("invalid host %q: no hostname", raw)
	}
	target := sshTarget{
		Host: host,
		Port: u.Port(),
	}
	if u.User != nil {
		target.User = u.User.Username()
	}
	if target.User == "" {
		target.User = defaultSshUser(getenv)
	}
	if target.Port == "" {
		target.Port = "22"
	}
	return target, nil
}

// ValidateHost reports whether raw is a usable `[user@]host[:port]` target,
// without contacting anything. It exists so `docket validate` can reject a
// typo in a play's `host:` offline rather than leaving it to surface as an
// `ssh:` failure partway through a run.
func ValidateHost(raw string) error {
	_, err := parseDokkuHost(raw, os.Getenv)
	return err
}

func defaultSshUser(getenv func(string) string) string {
	if v := getenv("USER"); v != "" {
		return v
	}
	if v := getenv("LOGNAME"); v != "" {
		return v
	}
	if u, err := user.Current(); err == nil {
		return u.Username
	}
	return ""
}

// controlPath returns the unix-domain socket path used by ControlMaster
// for the given target, PID and session. Hashing the PID gives concurrent
// docket runs against the same host distinct sockets so they cannot
// collide, and hashing the session does the same for two sessions in one
// process, so closing one cannot tear down a connection the other is using.
// session is 0 for a call made outside any Session.
//
// The port is part of the key because ssh reuses whatever master answers on
// the socket: without it, `host:22` and `host:2222` in one run would share
// the first connection and the second would never reach its own port.
func controlPath(target sshTarget, pid int, session uint64) string {
	key := target.UserHost() + ":" + target.Port + ":" + strconv.Itoa(pid)
	if session != 0 {
		key += ":" + strconv.FormatUint(session, 10)
	}
	sum := sha256.Sum256([]byte(key))
	return filepath.Join(os.TempDir(), "docket-"+hex.EncodeToString(sum[:])[:16]+".sock")
}

// buildSshArgv assembles the full argv for the `ssh` subprocess. OpenSSH
// does not preserve argv boundaries for the remote command: it space-joins
// the remote tokens into a single string that the remote login shell
// re-parses. So each remote token is POSIX-shell-quoted here (via
// syntax.Quote with syntax.LangPOSIX) to survive that re-parse intact on
// any POSIX shell. An argument that cannot be represented for a POSIX
// shell (a non-printable byte such as a tab, newline, or null) yields an
// error rather than a corrupted remote command.
//
// socket is the ControlPath, computed by the caller because the session
// that owns the connection has to record it too.
//
// The sudo and host-key settings come from opts, the caller's per-invocation
// Target. They used to be read from DOKKU_SUDO and
// DOKKU_SSH_ACCEPT_NEW_HOST_KEYS here, which meant the commands layer had to
// write them into the process environment to communicate them - and once
// written, they applied to every invocation in the process for the rest of its
// life.
func buildSshArgv(parsed sshTarget, opts Target, socket string, remote []string) ([]string, error) {
	argv := []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + socket,
		"-o", "ControlPersist=60",
		"-o", "BatchMode=yes",
	}
	if opts.AcceptNewHostKeys {
		argv = append(argv, "-o", "StrictHostKeyChecking=accept-new")
	}
	if parsed.Port != "" && parsed.Port != "22" {
		argv = append(argv, "-p", parsed.Port)
	}
	argv = append(argv, parsed.UserHost(), "--")
	if opts.Sudo {
		argv = append(argv, "sudo", "-n")
	}
	for _, arg := range remote {
		quoted, err := syntax.Quote(arg, syntax.LangPOSIX)
		if err != nil {
			return nil, fmt.Errorf("cannot send argument %q to remote shell: %w", arg, err)
		}
		argv = append(argv, quoted)
	}
	return argv, nil
}

// sshLookPathOnce caches the result of looking up the `ssh` binary so
// we don't pay LookPath on every dispatch.
var (
	sshLookPathOnce sync.Once
	sshLookPathErr  error
)

// Probe runs input as a state probe and reports whether it matched
// (exit 0). A non-zero exit from a command that actually ran is reported
// as `(false, nil)`, i.e. "the probed state is absent," so callers can
// write idempotent probes without unwrapping errors themselves.
//
// Any failure that means the probe never produced a real answer is
// propagated as `(false, err)` so the caller can short-circuit `Plan()`
// with `PlanResult{Error: err}` and let the formatter render `[!]`. That
// covers a transport-level failure (`*SSHError`), a command that could
// not be executed at all (the dokku binary is missing or not
// executable), a cancelled probe, and a `dokku` or `ssh` child killed by
// a signal - which exits with no status of its own, so its exit code is
// not an answer either. Distinguishing "ran and said no"
// from "could not run" relies on `ExecError.Ran`, since binary-not-found
// reports `ExitCode 0` and so cannot be told apart by exit code.
//
// Use this for any plan-time probe that today reads exit code only
// (`apps:exists`, `network:exists`, `<service>:linked`, etc.). A probe
// whose command distinguishes its answers by exit code - `git:auth-status`
// and `registry:auth-status` both do - wants ProbeCode instead, which is
// the same discrimination with the code left intact. Probes that need
// stdout should call CallExecCommand directly and use
// `errors.As(err, &*SSHError)` to discriminate.
func Probe(ctx context.Context, input ExecCommandInput) (bool, error) {
	result, err := ProbeCode(ctx, input)
	if err != nil {
		return false, err
	}
	return result.ExitCode == 0, nil
}

// ProbeCode is Probe with the exit code left intact, for a probe whose
// command answers with more than yes or no. dokku's `*:auth-status`
// comparators are the motivating case: they separate "nothing is stored"
// from "something else is stored" from "stored, but I cannot read it",
// and collapsing those into one "no" loses the distinction between a
// create and a replacement - and, for the third, between drift and a
// server that will never converge.
//
// The error discrimination is exactly Probe's, because it is the same
// question: a command that ran and exited non-zero produced a real
// answer, and anything that stopped it from running did not. On the
// answered path the response is returned with a nil error, so callers
// switch on `result.ExitCode` without unwrapping. On the unanswered path
// the error is propagated and the response is not meaningful.
//
// Note that the exit code is read off the response on both paths. A
// non-zero exit arrives as an `*ExecError` from the real runner, but an
// injected test runner reports one by returning a response with a
// non-zero ExitCode and no error at all.
func ProbeCode(ctx context.Context, input ExecCommandInput) (ExecCommandResponse, error) {
	result, err := CallExecCommand(ctx, input)
	if err != nil {
		// Transport-level failure (ssh connect/auth/host-key): propagate
		// so the caller can render `! ssh: ...`.
		var sshErr *SSHError
		if errors.As(err, &sshErr) {
			return ExecCommandResponse{}, err
		}
		// The command executed and exited non-zero: that is the probe's
		// answer, so the response carrying it is returned rather than the
		// error wrapping it.
		var execErr *ExecError
		if errors.As(err, &execErr) && execErr.Ran {
			return execErr.Response, nil
		}
		// Anything else - the command could not be executed (binary not
		// found, permission denied), was cancelled, or was killed by a
		// signal - is a real failure the caller must surface, not an answer.
		return ExecCommandResponse{}, err
	}
	return result, nil
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

// CallSshCommand executes a remote command over ssh against target under ctx.
// The execution pipeline mirrors CallExecCommand (same DOKKU_TRACE logging,
// masking, stdio wiring) so callers see identical behavior aside from the
// transport.
//
// It takes the whole Target rather than a host string because the argv it
// builds depends on the sudo and host-key settings too, and a signature that
// carried only the host would have to fetch those from somewhere else - which
// is exactly how they ended up in the process environment.
//
// On exit code 255 (OpenSSH's transport-failure code), the returned
// error is `*SSHError`. On any other non-zero exit, the returned error
// is the plain underlying error so the formatter renders the failure
// as a remote dokku error.
func CallSshCommand(ctx context.Context, target Target, input ExecCommandInput) (ExecCommandResponse, error) {
	parsed, err := parseDokkuHost(target.Host, os.Getenv)
	if err != nil {
		return ExecCommandResponse{}, &SSHError{Host: target.Host, Err: err}
	}
	if err := ensureSshAvailable(); err != nil {
		return ExecCommandResponse{}, &SSHError{Host: parsed.UserHost(), Err: err}
	}

	// Whether our own stdout is a terminal is the signal used below to decide
	// whether the child may read ours.
	interactive := stdoutIsTerminal()
	masker := MaskerFromContext(ctx)

	remote := append([]string{input.Command}, input.Args...)
	session := sessionFromContext(ctx)
	socket := controlPath(parsed, os.Getpid(), session.id())
	argv, err := buildSshArgv(parsed, target, socket, remote)
	if err != nil {
		return ExecCommandResponse{}, &SSHError{Host: parsed.UserHost(), Command: remote, Err: err}
	}
	// Recorded before the connection is made rather than after, so a master
	// that came up for a command that then failed is still closed.
	if err := session.track(parsed, socket); err != nil {
		return ExecCommandResponse{}, &SSHError{Host: parsed.UserHost(), Command: remote, Err: err}
	}

	// The `ssh` client inherits docket's environment and directory; nothing is
	// layered on top. Decorating this process would be pointless anyway, since
	// only the argv assembled above crosses to the remote shell.
	cmd := execute.ExecTask{
		Command: "ssh",
		Args:    argv,
	}

	if os.Getenv("DOKKU_TRACE") == "1" {
		log.Printf("ssh: %s %s", masker.String("ssh"), masker.String(strings.Join(argv, " ")))
	}

	if input.Stdin != nil {
		cmd.Stdin = input.Stdin
	} else if interactive {
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

	resolved := resolveSshCommandString(masker, input.Command, input.Args)

	res, runErr := cmd.Execute(ctx)
	// An ssh killed by a signal - usually the interrupt that is also about to
	// cancel this run - answered nothing, so it must not reach the exit-code
	// branches below as though the remote command had.
	runErr = signalDeathErr(ctx, res.ExitCode, runErr, signalDeathGrace)
	resp := ExecCommandResponse{
		Command:   resolved,
		Stdout:    res.Stdout,
		Stderr:    res.Stderr,
		ExitCode:  res.ExitCode,
		Cancelled: res.Cancelled || errors.Is(runErr, context.Canceled),
	}

	return classifySshResult(parsed, remote, resp, runErr)
}

// classifySshResult maps an ssh ExecTask result onto the docket error
// model. Exit 255 (and any error before the process started) is wrapped
// as *SSHError, as is a negative exit code: ssh was killed by a signal, so
// neither it nor the remote command produced a status. Any other non-zero
// exit returns a plain error built from stderr so the existing dokku-error
// rendering keeps working.
//
// CallSshCommand has already turned a signal death into runErr by the time it
// gets here; the negative-code check keeps this function from ever marking
// such a code as a real answer on its own.
func classifySshResult(target sshTarget, remote []string, resp ExecCommandResponse, runErr error) (ExecCommandResponse, error) {
	if runErr != nil {
		return resp, &SSHError{
			Host:    target.UserHost(),
			Command: remote,
			Err:     runErr,
			Stderr:  resp.Stderr,
		}
	}
	if resp.ExitCode < 0 {
		return resp, &SSHError{
			Host:    target.UserHost(),
			Command: remote,
			Err:     errKilledBySignal,
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
		// The remote dokku command ran and exited non-zero (not a
		// transport failure). Ran marks the exit code as authoritative so
		// Probe treats it as "state absent" rather than an execution error.
		return resp, &ExecError{Response: resp, Err: errors.New(resp.Stderr), Ran: true}
	}
	return resp, nil
}

// CloseSshControlMaster sends `ssh -O exit` to the ControlMaster a call made
// outside any Session opened for host, so the multiplexed connection is torn
// down cleanly. Best-effort: errors are swallowed because the master may
// already have exited (ControlPersist timeout, kill -9, etc.).
//
// Prefer a Session, which closes every master its calls opened - including
// ones for hosts the caller never named, such as a play's own target.
func CloseSshControlMaster(host string) error {
	target, err := parseDokkuHost(host, os.Getenv)
	if err != nil {
		return nil
	}
	exitControlMaster(target, controlPath(target, os.Getpid(), 0))
	return nil
}

// exitControlMaster asks the master listening on socket to exit. A socket
// that does not exist - the connection never came up, or ControlPersist
// already expired it - is skipped.
func exitControlMaster(target sshTarget, socket string) {
	if _, err := exec.LookPath("ssh"); err != nil {
		return
	}
	if _, err := os.Stat(socket); err != nil {
		return
	}
	cmd := exec.Command("ssh",
		"-o", "ControlPath="+socket,
		"-O", "exit",
		target.UserHost(),
	)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}
