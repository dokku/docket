package subprocess

import (
	"errors"
	"strings"
	"testing"
)

func TestParseDokkuHost(t *testing.T) {
	t.Setenv("USER", "deploy")
	t.Setenv("LOGNAME", "")

	tests := []struct {
		name     string
		raw      string
		wantUser string
		wantHost string
		wantPort string
		wantErr  bool
	}{
		{"bare host", "host", "deploy", "host", "22", false},
		{"user@host", "alice@host", "alice", "host", "22", false},
		{"user@host:port", "alice@host.example:2222", "alice", "host.example", "2222", false},
		{"host:port", "host:2222", "deploy", "host", "2222", false},
		{"ipv6 with port", "[::1]:2222", "deploy", "::1", "2222", false},
		{"empty", "", "", "", "", true},
		{"whitespace", "   ", "", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseDokkuHost(tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q", tt.raw)
				}
				return
			}
			if err != nil {
				t.Fatalf("parseDokkuHost(%q) returned error: %v", tt.raw, err)
			}
			if got.User != tt.wantUser {
				t.Errorf("user = %q, want %q", got.User, tt.wantUser)
			}
			if got.Host != tt.wantHost {
				t.Errorf("host = %q, want %q", got.Host, tt.wantHost)
			}
			if got.Port != tt.wantPort {
				t.Errorf("port = %q, want %q", got.Port, tt.wantPort)
			}
		})
	}
}

func TestParseDokkuHostFallsBackToLogname(t *testing.T) {
	t.Setenv("USER", "")
	t.Setenv("LOGNAME", "fallback")

	target, err := parseDokkuHost("host")
	if err != nil {
		t.Fatalf("parseDokkuHost returned error: %v", err)
	}
	if target.User != "fallback" {
		t.Errorf("user = %q, want %q", target.User, "fallback")
	}
}

func TestSshTargetUserHost(t *testing.T) {
	if got := (sshTarget{User: "alice", Host: "host"}).UserHost(); got != "alice@host" {
		t.Errorf("UserHost() = %q, want alice@host", got)
	}
	if got := (sshTarget{Host: "host"}).UserHost(); got != "host" {
		t.Errorf("UserHost() = %q, want host", got)
	}
}

func TestControlPathStableForSamePidHost(t *testing.T) {
	a := controlPath("alice@host", 1234)
	b := controlPath("alice@host", 1234)
	if a != b {
		t.Errorf("controlPath should be stable: %q vs %q", a, b)
	}
}

func TestControlPathDiffersByPid(t *testing.T) {
	a := controlPath("alice@host", 1234)
	b := controlPath("alice@host", 5678)
	if a == b {
		t.Errorf("controlPath should differ by pid (got %q for both)", a)
	}
}

func TestControlPathDiffersByHost(t *testing.T) {
	a := controlPath("alice@hostA", 1234)
	b := controlPath("alice@hostB", 1234)
	if a == b {
		t.Errorf("controlPath should differ by host (got %q for both)", a)
	}
}

func TestControlPathExtension(t *testing.T) {
	got := controlPath("host", 1)
	if !strings.HasSuffix(got, ".sock") {
		t.Errorf("controlPath should end with .sock: %q", got)
	}
	if !strings.Contains(got, "docket-") {
		t.Errorf("controlPath should contain docket- prefix: %q", got)
	}
}

func TestBuildSshArgvDefault(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "")
	t.Setenv("DOKKU_SUDO", "")

	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	argv, err := buildSshArgv(target, []string{"dokku", "apps:list"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}

	wantOpts := []string{
		"ControlMaster=auto",
		"ControlPersist=60",
		"BatchMode=yes",
	}
	joined := strings.Join(argv, " ")
	for _, opt := range wantOpts {
		if !strings.Contains(joined, opt) {
			t.Errorf("argv missing %q: %v", opt, argv)
		}
	}
	if !strings.Contains(joined, "ControlPath=") {
		t.Errorf("argv missing ControlPath=: %v", argv)
	}
	if !containsExact(argv, "alice@host") {
		t.Errorf("argv missing user@host: %v", argv)
	}
	if !containsExact(argv, "--") {
		t.Errorf("argv missing -- separator: %v", argv)
	}
	// Remote command must come after `--`.
	dashIdx := indexOf(argv, "--")
	if dashIdx < 0 || dashIdx == len(argv)-1 {
		t.Fatalf("argv has no remote portion after --: %v", argv)
	}
	if argv[dashIdx+1] != "dokku" || argv[dashIdx+2] != "apps:list" {
		t.Errorf("remote command not at expected position: %v", argv[dashIdx+1:])
	}
	if containsExact(argv, "-p") {
		t.Errorf("argv should not include -p for default port 22: %v", argv)
	}
	if containsExact(argv, "StrictHostKeyChecking=accept-new") {
		t.Errorf("argv should not include accept-new without env var: %v", argv)
	}
}

func TestBuildSshArgvNonStandardPort(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "")
	t.Setenv("DOKKU_SUDO", "")

	target := sshTarget{User: "alice", Host: "host", Port: "2222"}
	argv, err := buildSshArgv(target, []string{"dokku", "version"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}
	if !containsExact(argv, "-p") {
		t.Errorf("argv missing -p flag: %v", argv)
	}
	if !containsExact(argv, "2222") {
		t.Errorf("argv missing port value: %v", argv)
	}
}

func TestBuildSshArgvAcceptNewHostKeys(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "1")
	t.Setenv("DOKKU_SUDO", "")

	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	argv, err := buildSshArgv(target, []string{"dokku", "version"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}
	joined := strings.Join(argv, " ")
	if !strings.Contains(joined, "StrictHostKeyChecking=accept-new") {
		t.Errorf("argv missing StrictHostKeyChecking=accept-new: %v", argv)
	}
}

func TestBuildSshArgvDokkuSudo(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "")
	t.Setenv("DOKKU_SUDO", "1")

	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	argv, err := buildSshArgv(target, []string{"dokku", "version"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}

	// sudo must appear after `--`, not before (we never run `sudo ssh`).
	dashIdx := indexOf(argv, "--")
	if dashIdx < 0 {
		t.Fatalf("argv missing -- separator: %v", argv)
	}
	pre, post := argv[:dashIdx], argv[dashIdx+1:]
	if containsExact(pre, "sudo") {
		t.Errorf("sudo should not appear before --: %v", pre)
	}
	if len(post) < 4 || post[0] != "sudo" || post[1] != "-n" || post[2] != "dokku" || post[3] != "version" {
		t.Errorf("expected post-`--` argv [sudo -n dokku version], got %v", post)
	}
}

func TestBuildSshArgvDoubleDashSeparator(t *testing.T) {
	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	argv, err := buildSshArgv(target, []string{"dokku", "config:set", "--no-restart"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}
	dashIdx := indexOf(argv, "--")
	if dashIdx < 0 {
		t.Fatalf("argv missing -- separator: %v", argv)
	}
	post := argv[dashIdx+1:]
	if len(post) < 3 || post[0] != "dokku" || post[1] != "config:set" || post[2] != "--no-restart" {
		t.Errorf("remote command not preserved across --: %v", post)
	}
}

func TestBuildSshArgvQuotesRemoteArgs(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "")
	t.Setenv("DOKKU_SUDO", "")

	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	argv, err := buildSshArgv(target, []string{"dokku", "ps:set", "app", "start-cmd", "npm run start"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}
	dashIdx := indexOf(argv, "--")
	if dashIdx < 0 {
		t.Fatalf("argv missing -- separator: %v", argv)
	}
	post := argv[dashIdx+1:]
	// Metacharacter-free tokens stay bare; the value with a space is
	// single-quoted so OpenSSH's space-joined string re-parses to one
	// argument on the remote login shell.
	want := []string{"dokku", "ps:set", "app", "start-cmd", "'npm run start'"}
	if !equalStrings(post, want) {
		t.Errorf("post-`--` argv = %v, want %v", post, want)
	}
}

func TestBuildSshArgvQuotesInjection(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "")
	t.Setenv("DOKKU_SUDO", "")

	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	argv, err := buildSshArgv(target, []string{"dokku", "config:set", "app", "x; rm -rf ~"})
	if err != nil {
		t.Fatalf("buildSshArgv returned error: %v", err)
	}
	// The payload must survive as a single quoted token so the remote shell
	// cannot execute the `;` or expand `~`.
	if !containsExact(argv, "'x; rm -rf ~'") {
		t.Errorf("injection payload not single-quoted as one token: %v", argv)
	}
	if containsExact(argv, "x; rm -rf ~") {
		t.Errorf("injection payload present unquoted: %v", argv)
	}
}

func TestBuildSshArgvRejectsUnquotable(t *testing.T) {
	t.Setenv("DOKKU_SSH_ACCEPT_NEW_HOST_KEYS", "")
	t.Setenv("DOKKU_SUDO", "")

	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	// A newline has no POSIX-shell escape, so we refuse to build the remote
	// command rather than corrupt it.
	if _, err := buildSshArgv(target, []string{"dokku", "config:set", "app", "a\nb"}); err == nil {
		t.Fatal("expected error for argument containing a newline")
	}

	// The same failure surfaces through the dispatcher as a transport-level
	// *SSHError so plan/apply render it with the `ssh:` prefix.
	_, err := CallSshCommand("alice@host", ExecCommandInput{
		Command: "dokku",
		Args:    []string{"config:set", "app", "a\nb"},
	})
	if err == nil {
		t.Fatal("expected error from CallSshCommand for unquotable argument")
	}
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("expected *SSHError, got %T (%v)", err, err)
	}
}

func TestSSHErrorMessage(t *testing.T) {
	e := &SSHError{Host: "alice@host", Stderr: "Permission denied (publickey)."}
	got := e.Error()
	want := "ssh alice@host: Permission denied (publickey)."
	if got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}
}

func TestSSHErrorMessageFallsBackToErr(t *testing.T) {
	inner := errors.New("dial tcp: connection refused")
	e := &SSHError{Host: "alice@host", Err: inner}
	got := e.Error()
	if !strings.Contains(got, "alice@host") {
		t.Errorf("Error() = %q, want it to contain host", got)
	}
	if !strings.Contains(got, "connection refused") {
		t.Errorf("Error() = %q, want it to contain inner error", got)
	}
}

func TestSSHErrorUnwrap(t *testing.T) {
	inner := errors.New("inner")
	e := &SSHError{Host: "host", Err: inner}
	if !errors.Is(e, inner) {
		t.Error("errors.Is should match inner error")
	}
	var target *SSHError
	if !errors.As(e, &target) {
		t.Error("errors.As should match *SSHError")
	}
}

func TestClassifySshResultExit255(t *testing.T) {
	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	resp := ExecCommandResponse{ExitCode: 255, Stderr: "ssh: connect to host: Connection refused"}
	_, err := classifySshResult(target, []string{"dokku", "version"}, resp, nil)
	if err == nil {
		t.Fatal("expected error for exit 255")
	}
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("expected *SSHError, got %T", err)
	}
}

func TestClassifySshResultRemoteFailureIsNotSshError(t *testing.T) {
	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	resp := ExecCommandResponse{ExitCode: 1, Stderr: "App test does not exist"}
	_, err := classifySshResult(target, []string{"dokku", "apps:exists", "test"}, resp, nil)
	if err == nil {
		t.Fatal("expected error for non-zero exit")
	}
	var sshErr *SSHError
	if errors.As(err, &sshErr) {
		t.Errorf("non-255 exit should not be SSHError; got %#v", err)
	}
	if !strings.Contains(err.Error(), "App test does not exist") {
		t.Errorf("error should preserve remote stderr: %v", err)
	}
}

func TestClassifySshResultPreProcessErrorIsSshError(t *testing.T) {
	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	resp := ExecCommandResponse{}
	runErr := errors.New("exec: \"ssh\": executable file not found")
	_, err := classifySshResult(target, []string{"dokku", "version"}, resp, runErr)
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("expected *SSHError, got %T", err)
	}
}

func TestClassifySshResultSuccess(t *testing.T) {
	target := sshTarget{User: "alice", Host: "host", Port: "22"}
	resp := ExecCommandResponse{ExitCode: 0}
	_, err := classifySshResult(target, []string{"dokku", "version"}, resp, nil)
	if err != nil {
		t.Errorf("expected no error for exit 0, got %v", err)
	}
}

func TestCallSshCommandReturnsSshErrorOnEmptyHost(t *testing.T) {
	_, err := CallSshCommand("", ExecCommandInput{Command: "dokku", Args: []string{"version"}})
	if err == nil {
		t.Fatal("expected error for empty host")
	}
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("expected *SSHError, got %T", err)
	}
}

func TestProbeSuccess(t *testing.T) {
	matched, err := Probe(ExecCommandInput{Command: "true"})
	if err != nil {
		t.Fatalf("Probe(true) returned error: %v", err)
	}
	if !matched {
		t.Error("Probe(true) should report matched=true")
	}
}

func TestProbeDokkuLevelFailure(t *testing.T) {
	matched, err := Probe(ExecCommandInput{Command: "false"})
	if err != nil {
		t.Errorf("Probe(false) returned non-nil error %v - dokku-level exit should be normalised", err)
	}
	if matched {
		t.Error("Probe(false) should report matched=false")
	}
}

func TestProbeSshTransportErrorPropagates(t *testing.T) {
	// Inject a default host that points at a closed port so the SSH
	// dispatcher routes the probe and OpenSSH exits 255 (transport
	// failure). Dispatch only triggers for input.Command=="dokku".
	t.Cleanup(func() { SetDefaultHost("") })
	SetDefaultHost("docket-test@127.0.0.1:1")

	matched, err := Probe(ExecCommandInput{
		Command: "dokku",
		Args:    []string{"--quiet", "apps:exists", "anything"},
	})
	if matched {
		t.Error("Probe should report matched=false on transport failure")
	}
	if err == nil {
		t.Fatal("Probe should propagate the SSH transport error")
	}
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("Probe error should be *SSHError, got %T (%v)", err, err)
	}
}

func TestSetAndGetDefaultHost(t *testing.T) {
	t.Cleanup(func() { SetDefaultHost("") })
	SetDefaultHost("alice@host:2222")
	if got := GetDefaultHost(); got != "alice@host:2222" {
		t.Errorf("GetDefaultHost() = %q, want %q", got, "alice@host:2222")
	}
	SetDefaultHost("")
	if got := GetDefaultHost(); got != "" {
		t.Errorf("GetDefaultHost() = %q, want empty after clear", got)
	}
}

// helpers

func containsExact(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}

func indexOf(haystack []string, needle string) int {
	for i, s := range haystack {
		if s == needle {
			return i
		}
	}
	return -1
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
