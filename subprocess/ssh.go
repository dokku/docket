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
	"github.com/fatih/color"
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
func parseDokkuHost(raw string) (sshTarget, error) {
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
		target.User = defaultSshUser()
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
	_, err := parseDokkuHost(raw)
	return err
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

// buildSshArgv assembles the full argv for the `ssh` subprocess. OpenSSH
// does not preserve argv boundaries for the remote command: it space-joins
// the remote tokens into a single string that the remote login shell
// re-parses. So each remote token is POSIX-shell-quoted here (via
// syntax.Quote with syntax.LangPOSIX) to survive that re-parse intact on
// any POSIX shell. An argument that cannot be represented for a POSIX
// shell (a non-printable byte such as a tab, newline, or null) yields an
// error rather than a corrupted remote command.
//
// The sudo and host-key settings come from opts, the caller's per-invocation
// Target. They used to be read from DOKKU_SUDO and
// DOKKU_SSH_ACCEPT_NEW_HOST_KEYS here, which meant the commands layer had to
// write them into the process environment to communicate them - and once
// written, they applied to every invocation in the process for the rest of its
// life.
func buildSshArgv(parsed sshTarget, opts Target, remote []string) ([]string, error) {
	argv := []string{
		"-o", "ControlMaster=auto",
		"-o", "ControlPath=" + controlPath(parsed.UserHost(), os.Getpid()),
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
// executable), and a cancelled probe. Distinguishing "ran and said no"
// from "could not run" relies on `ExecError.Ran`, since binary-not-found
// reports `ExitCode 0` and so cannot be told apart by exit code.
//
// Use this for any plan-time probe that today reads exit code only
// (`apps:exists`, `network:exists`, `<service>:linked`, etc.). Probes
// that need stdout should call CallExecCommand directly and use
// `errors.As(err, &*SSHError)` to discriminate.
func Probe(ctx context.Context, input ExecCommandInput) (bool, error) {
	result, err := CallExecCommand(ctx, input)
	if err != nil {
		// Transport-level failure (ssh connect/auth/host-key): propagate
		// so the caller can render `! ssh: ...`.
		var sshErr *SSHError
		if errors.As(err, &sshErr) {
			return false, err
		}
		// The command executed and exited non-zero: the probed state is
		// absent. Report (false, nil) so idempotent probes need not unwrap.
		var execErr *ExecError
		if errors.As(err, &execErr) && execErr.Ran {
			return false, nil
		}
		// Anything else - the command could not be executed (binary not
		// found, permission denied) or was cancelled - is a real failure
		// the caller must surface, not "state absent".
		return false, err
	}
	return result.ExitCode == 0, nil
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
	parsed, err := parseDokkuHost(target.Host)
	if err != nil {
		return ExecCommandResponse{}, &SSHError{Host: target.Host, Err: err}
	}
	if err := ensureSshAvailable(); err != nil {
		return ExecCommandResponse{}, &SSHError{Host: parsed.UserHost(), Err: err}
	}

	// isatty reports whether our own stdout is a terminal, which is the
	// signal used below to decide whether the child may read the terminal.
	isatty := !color.NoColor
	masker := MaskerFromContext(ctx)

	remote := append([]string{input.Command}, input.Args...)
	argv, err := buildSshArgv(parsed, target, remote)
	if err != nil {
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

	resolved := resolveSshCommandString(masker, input.Command, input.Args)

	res, runErr := cmd.Execute(ctx)
	resp := ExecCommandResponse{
		Command:   resolved,
		Stdout:    res.Stdout,
		Stderr:    res.Stderr,
		ExitCode:  res.ExitCode,
		Cancelled: res.Cancelled,
	}

	return classifySshResult(parsed, remote, resp, runErr)
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
		// The remote dokku command ran and exited non-zero (not a
		// transport failure). Ran marks the exit code as authoritative so
		// Probe treats it as "state absent" rather than an execution error.
		return resp, &ExecError{Response: resp, Err: errors.New(resp.Stderr), Ran: true}
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
