package subprocess

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"
)

// newRecordingSessionWith is newRecordingSession with session options.
func newRecordingSessionWith(opts ...SessionOption) (*Session, *closeRecorder) {
	rec := &closeRecorder{closed: map[string]int{}}
	s := NewSession(nil, opts...)
	s.closeMaster = func(_ sshTarget, socket string) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.closed[socket]++
	}
	return s, rec
}

// controlPersistArgs returns every ControlPersist value in argv.
func controlPersistArgs(argv []string) []string {
	var out []string
	for i := 0; i+1 < len(argv); i++ {
		if argv[i] == "-o" && strings.HasPrefix(argv[i+1], "ControlPersist=") {
			out = append(out, argv[i+1])
		}
	}
	return out
}

// TestControlPersistValue pins how a persist duration becomes ssh's value.
// Zero must never reach ssh as 0, which OpenSSH reads as "keep forever".
func TestControlPersistValue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		persist time.Duration
		want    string
	}{
		{0, "1"},
		{-time.Second, "1"},
		{time.Millisecond, "1"},
		{time.Second, "1"},
		{1500 * time.Millisecond, "2"},
		{defaultControlPersist, "60"},
		{10 * time.Minute, "600"},
	} {
		if got := controlPersistValue(tc.persist); got != tc.want {
			t.Errorf("controlPersistValue(%v) = %q, want %q", tc.persist, got, tc.want)
		}
	}
}

// TestBuildSshArgvControlPersist pins that the argv carries exactly one
// ControlPersist, the one the persist duration asks for.
func TestBuildSshArgvControlPersist(t *testing.T) {
	t.Parallel()

	parsed := sshTarget{User: "alice", Host: "host", Port: "22"}
	for _, tc := range []struct {
		persist time.Duration
		want    string
	}{
		{defaultControlPersist, "ControlPersist=60"},
		{10 * time.Minute, "ControlPersist=600"},
		{0, "ControlPersist=1"},
	} {
		argv, err := buildSshArgv(parsed, Target{}, testSocket, tc.persist, []string{"dokku", "version"})
		if err != nil {
			t.Fatalf("buildSshArgv(%v) returned error: %v", tc.persist, err)
		}
		if got := controlPersistArgs(argv); len(got) != 1 || got[0] != tc.want {
			t.Errorf("buildSshArgv(%v) ControlPersist options = %v, want [%s]", tc.persist, got, tc.want)
		}
	}
}

// TestSessionControlPersistOption pins the default, the option, and that a
// negative duration is read as zero.
func TestSessionControlPersistOption(t *testing.T) {
	t.Parallel()

	var none *Session
	for _, tc := range []struct {
		name string
		s    *Session
		want time.Duration
	}{
		{"no session", none, 60 * time.Second},
		{"default", NewSession(nil), 60 * time.Second},
		{"set", NewSession(nil, WithControlPersist(5*time.Minute)), 5 * time.Minute},
		{"zero", NewSession(nil, WithControlPersist(0)), 0},
		{"negative", NewSession(nil, WithControlPersist(-time.Second)), 0},
		{"last wins", NewSession(nil, WithControlPersist(0), WithControlPersist(time.Second)), time.Second},
	} {
		if got := tc.s.controlPersist(); got != tc.want {
			t.Errorf("%s: controlPersist() = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// TestControlPersistStaysReserved pins that the option does not open
// ControlPersist to callers: SSHOptions naming it is still refused, under a
// session with any persist, before ssh runs.
func TestControlPersistStaysReserved(t *testing.T) {
	t.Parallel()

	for _, opt := range []string{"ControlPersist=0", "ControlPersist no", "controlpersist=600"} {
		if err := ValidateSSHOptions([]string{opt}); err == nil {
			t.Errorf("ValidateSSHOptions(%q) = nil, want a refusal", opt)
		}
	}

	s, rec := newRecordingSessionWith(WithControlPersist(0))
	ctx := s.Context(context.Background(), Target{
		Host:       "docket-test@127.0.0.1:1",
		SSHOptions: []string{"ControlPersist=600"},
	})
	err := dispatch(ctx)
	var sshErr *SSHError
	if !errors.As(err, &sshErr) || !strings.Contains(err.Error(), "ControlPersist") {
		t.Fatalf("dispatch with ControlPersist in SSHOptions = %v, want an ssh: refusal naming it", err)
	}
	if got := rec.sockets(); len(got) != 0 {
		t.Errorf("a refused dispatch closed %v", got)
	}
}

// TestZeroPersistClosesAfterLastCommand pins that a zero-persist session
// closes a master when the call that used it returns, without Close, and that
// Close then has nothing left to close.
func TestZeroPersistClosesAfterLastCommand(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSessionWith(WithControlPersist(0))
	host := "docket-test@127.0.0.1:1"
	ctx := s.Context(context.Background(), Target{Host: host})

	_ = dispatch(ctx)
	socket := sessionSocket(t, s, host)
	if got := rec.sockets(); !equalStrings(got, []string{socket}) {
		t.Fatalf("after one call the session closed %v, want %v", got, []string{socket})
	}
	_ = dispatch(ctx)
	if got := rec.sockets(); !equalStrings(got, []string{socket, socket}) {
		t.Fatalf("after a second call the session closed %v, want the socket twice", got)
	}
	_ = s.Close()
	if got := rec.sockets(); len(got) != 2 {
		t.Errorf("Close closed again: %v", got)
	}
}

// TestDefaultPersistLeavesMasterToClose pins that the default session does not
// close a master after each call; only Close does.
func TestDefaultPersistLeavesMasterToClose(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSessionWith()
	host := "docket-test@127.0.0.1:1"
	_ = dispatch(s.Context(context.Background(), Target{Host: host}))
	if got := rec.sockets(); len(got) != 0 {
		t.Fatalf("a default session closed %v before Close", got)
	}
	_ = s.Close()
	if got := rec.sockets(); !equalStrings(got, []string{sessionSocket(t, s, host)}) {
		t.Errorf("Close closed %v, want the one socket", got)
	}
}

// TestZeroPersistWaitsForOverlappingCommands pins that a master shared by
// overlapping commands is closed only when the last of them returns.
func TestZeroPersistWaitsForOverlappingCommands(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSessionWith(WithControlPersist(0))
	target := sshTarget{User: "docket-test", Host: "127.0.0.1", Port: "1"}
	first, err := s.track(target, "a.sock")
	if err != nil {
		t.Fatal(err)
	}
	second, err := s.track(target, "a.sock")
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.track(target, "b.sock")
	if err != nil {
		t.Fatal(err)
	}

	first()
	first() // a second call of one release must not count twice
	if got := rec.sockets(); len(got) != 0 {
		t.Fatalf("closed %v while a command was still using the master", got)
	}
	other()
	if got := rec.sockets(); !equalStrings(got, []string{"b.sock"}) {
		t.Fatalf("closed %v, want only b.sock, whose one command returned", got)
	}
	second()
	if got := rec.sockets(); !equalStrings(got, []string{"a.sock", "b.sock"}) {
		t.Errorf("closed %v, want both once their last command returned", got)
	}
}

// TestZeroPersistNewCommandWaitsForClose pins that a command arriving while
// its master is being closed waits for the close to finish rather than
// reaching a master that is exiting.
func TestZeroPersistNewCommandWaitsForClose(t *testing.T) {
	t.Parallel()

	s := NewSession(nil, WithControlPersist(0))
	started := make(chan struct{})
	proceed := make(chan struct{})
	var once sync.Once
	s.closeMaster = func(sshTarget, string) {
		once.Do(func() {
			close(started)
			<-proceed
		})
	}
	target := sshTarget{Host: "127.0.0.1", Port: "1"}
	release, err := s.track(target, "a.sock")
	if err != nil {
		t.Fatal(err)
	}
	go release()
	<-started

	tracked := make(chan struct{})
	go func() {
		next, err := s.track(target, "a.sock")
		if err == nil {
			next()
		}
		close(tracked)
	}()
	select {
	case <-tracked:
		t.Fatal("a new command reached the master while it was being closed")
	case <-time.After(50 * time.Millisecond):
	}
	close(proceed)
	<-tracked
}

// TestZeroPersistReleaseAfterClose pins that a command returning after Close
// does not close its master a second time.
func TestZeroPersistReleaseAfterClose(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSessionWith(WithControlPersist(0))
	release, err := s.track(sshTarget{Host: "127.0.0.1", Port: "1"}, "a.sock")
	if err != nil {
		t.Fatal(err)
	}
	_ = s.Close()
	release()
	if got := rec.sockets(); !equalStrings(got, []string{"a.sock"}) {
		t.Errorf("closed %v, want a.sock once", got)
	}
}

// fakeSSH puts an `ssh` on PATH that behaves like OpenSSH's control socket
// handling: a command call creates the ControlPath, `-O exit` removes it. It
// logs every argv to the returned file. A socket left on disk is a master
// still running. Sockets land in a fresh TMPDIR, so the caller can list them.
// It sets the environment, so a test using it cannot run in parallel.
func fakeSSH(t *testing.T) (sockDir, argvLog string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the fake ssh is a shell script")
	}
	bin := t.TempDir()
	sockDir = t.TempDir()
	argvLog = filepath.Join(t.TempDir(), "argv.log")
	script := `#!/bin/sh
echo "$*" >>"` + argvLog + `"
socket=""
exit_master=0
while [ $# -gt 0 ]; do
  case "$1" in
    -o) case "$2" in ControlPath=*) socket="${2#ControlPath=}" ;; esac; shift 2 ;;
    -O) [ "$2" = "exit" ] && exit_master=1; shift 2 ;;
    -p|-i) shift 2 ;;
    --) shift; break ;;
    *) shift ;;
  esac
done
if [ "$exit_master" -eq 1 ]; then
  rm -f "$socket"
  exit 0
fi
touch "$socket"
exit 0
`
	if err := os.WriteFile(filepath.Join(bin, "ssh"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("TMPDIR", sockDir)
	return sockDir, argvLog
}

func liveSockets(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, "docket-*.sock"))
	if err != nil {
		t.Fatal(err)
	}
	return matches
}

// TestZeroPersistLeavesNoMasterRunning drives real ssh dispatch against a fake
// ssh: after each call under a zero-persist session returns, no master is left
// running, with no Close at all. A default session, by contrast, leaves its
// master for Close.
func TestZeroPersistLeavesNoMasterRunning(t *testing.T) {
	sockDir, argvLog := fakeSSH(t)

	s := NewSession(nil, WithControlPersist(0))
	ctx := s.Context(context.Background(), Target{Host: "deploy@one.example.com"})
	for i := 0; i < 2; i++ {
		if err := dispatch(ctx); err != nil {
			t.Fatalf("dispatch %d: %v", i, err)
		}
		if got := liveSockets(t, sockDir); len(got) != 0 {
			t.Fatalf("after call %d a master is still running: %v", i, got)
		}
	}
	logged, err := os.ReadFile(argvLog)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(logged), "-o ControlPersist=1 ") {
		t.Errorf("ssh argv did not carry ControlPersist=1:\n%s", logged)
	}
	if n := strings.Count(string(logged), "-O exit"); n != 2 {
		t.Errorf("ssh -O exit ran %d times, want once per call:\n%s", n, logged)
	}

	persisting := NewSession(nil)
	if err := dispatch(persisting.Context(context.Background(), Target{Host: "deploy@one.example.com"})); err != nil {
		t.Fatalf("dispatch: %v", err)
	}
	if got := liveSockets(t, sockDir); len(got) != 1 {
		t.Fatalf("a default session left %v, want its one master running until Close", got)
	}
	_ = persisting.Close()
	if got := liveSockets(t, sockDir); len(got) != 0 {
		t.Errorf("Close left %v running", got)
	}
}
