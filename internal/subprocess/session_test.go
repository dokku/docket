package subprocess

import (
	"context"
	"errors"
	"os"
	"sort"
	"sync"
	"testing"
)

// closeRecorder stands in for exitControlMaster, counting how many times each
// socket was closed so a test can see what Close would tear down without an
// ssh server to open a real master against.
type closeRecorder struct {
	mu     sync.Mutex
	closed map[string]int
}

func newRecordingSession(masker *Masker) (*Session, *closeRecorder) {
	rec := &closeRecorder{closed: map[string]int{}}
	s := NewSession(masker)
	s.closeMaster = func(_ sshTarget, socket string) {
		rec.mu.Lock()
		defer rec.mu.Unlock()
		rec.closed[socket]++
	}
	return s, rec
}

func (r *closeRecorder) sockets() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for socket, n := range r.closed {
		for i := 0; i < n; i++ {
			out = append(out, socket)
		}
	}
	sort.Strings(out)
	return out
}

// sessionSocket is the control path a call to raw under s uses.
func sessionSocket(t *testing.T, s *Session, raw string) string {
	t.Helper()
	parsed, err := parseDokkuHost(raw, os.Getenv)
	if err != nil {
		t.Fatalf("parseDokkuHost(%q): %v", raw, err)
	}
	return controlPath(parsed, os.Getpid(), s.id())
}

// dispatch sends a dokku command over ssh under ctx. The targets these tests
// use are closed loopback ports, so ssh fails fast with a transport error; the
// call still counts, because the session records a master before dialling.
func dispatch(ctx context.Context) error {
	_, err := CallExecCommand(ctx, ExecCommandInput{Command: "dokku", Args: []string{"version"}})
	return err
}

func TestSessionContextCarriesTargetAndMasker(t *testing.T) {
	t.Parallel()

	masker := NewMasker("s3cr3t")
	s := NewSession(masker)
	target := Target{Host: "deploy@one", Sudo: true}
	ctx := s.Context(context.Background(), target)

	if got := TargetFromContext(ctx); got != target {
		t.Errorf("TargetFromContext = %+v, want %+v", got, target)
	}
	if got := MaskerFromContext(ctx); got != masker {
		t.Errorf("MaskerFromContext = %p, want the session's masker %p", got, masker)
	}
	if got := sessionFromContext(ctx); got != s {
		t.Errorf("sessionFromContext = %p, want %p", got, s)
	}
}

// TestSessionNilMaskerKeepsParent pins that a session built without a masker
// does not blank out one the caller already put on the context.
func TestSessionNilMaskerKeepsParent(t *testing.T) {
	t.Parallel()

	parent := NewMasker("s3cr3t")
	ctx := NewSession(nil).Context(ContextWithMasker(context.Background(), parent), Target{})
	if got := MaskerFromContext(ctx); got != parent {
		t.Errorf("MaskerFromContext = %p, want the parent's masker %p", got, parent)
	}
}

// TestSessionClosesEveryDispatchedHost is the case the session exists for: a
// per-play target derived from the session context with ContextWithTarget is
// recorded as well as the run-wide one, and each master is closed once however
// many calls went through it.
func TestSessionClosesEveryDispatchedHost(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSession(nil)
	run := s.Context(context.Background(), Target{Host: "docket-test@127.0.0.1:1"})
	play := ContextWithTarget(run, Target{Host: "docket-test@127.0.0.1:2"})

	for _, ctx := range []context.Context{run, run, play} {
		var sshErr *SSHError
		if err := dispatch(ctx); !errors.As(err, &sshErr) {
			t.Fatalf("dispatch to a closed port should fail with *SSHError, got %T (%v)", err, err)
		}
	}

	if err := s.Close(); err != nil {
		t.Fatalf("Close returned %v", err)
	}
	want := []string{
		sessionSocket(t, s, "docket-test@127.0.0.1:1"),
		sessionSocket(t, s, "docket-test@127.0.0.1:2"),
	}
	sort.Strings(want)
	if got := rec.sockets(); !equalStrings(got, want) {
		t.Errorf("Close closed %v, want each dispatched socket once: %v", got, want)
	}
}

// TestSessionCloseSkipsUnreachedHosts pins that a context handed out but never
// dispatched through leaves nothing to close.
func TestSessionCloseSkipsUnreachedHosts(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSession(nil)
	_ = s.Context(context.Background(), Target{Host: "docket-test@127.0.0.1:1"})
	_ = s.Close()
	if got := rec.sockets(); len(got) != 0 {
		t.Errorf("Close closed %v for a session that never dispatched", got)
	}
}

func TestSessionCloseIsIdempotent(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSession(nil)
	_ = dispatch(s.Context(context.Background(), Target{Host: "docket-test@127.0.0.1:1"}))

	if err := s.Close(); err != nil {
		t.Fatalf("first Close returned %v", err)
	}
	if err := s.Close(); err != nil {
		t.Fatalf("second Close returned %v", err)
	}
	if got := rec.sockets(); len(got) != 1 {
		t.Errorf("two Close calls closed %v, want the one socket once", got)
	}
}

// TestSessionRefusesDispatchAfterClose pins that a call under a closed session
// fails rather than opening a master nothing is left to close.
func TestSessionRefusesDispatchAfterClose(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSession(nil)
	ctx := s.Context(context.Background(), Target{Host: "docket-test@127.0.0.1:1"})
	_ = s.Close()

	err := dispatch(ctx)
	var sshErr *SSHError
	if !errors.As(err, &sshErr) {
		t.Fatalf("dispatch after Close should fail with *SSHError, got %T (%v)", err, err)
	}
	if !errors.Is(err, ErrSessionClosed) {
		t.Errorf("dispatch after Close should wrap ErrSessionClosed, got %v", err)
	}
	if got := rec.sockets(); len(got) != 0 {
		t.Errorf("a refused dispatch should leave nothing to close, closed %v", got)
	}
}

// TestSessionConcurrentUse drives one session from several goroutines, then
// closes it from several more. Run under -race it pins that the bookkeeping is
// safe to share; the counts pin that concurrent Close calls still close each
// master exactly once.
func TestSessionConcurrentUse(t *testing.T) {
	t.Parallel()

	s, rec := newRecordingSession(nil)
	hosts := []string{"docket-test@127.0.0.1:1", "docket-test@127.0.0.1:2", "docket-test@127.0.0.1:3"}

	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func(host string) {
			defer wg.Done()
			_ = dispatch(s.Context(context.Background(), Target{Host: host}))
		}(hosts[i%len(hosts)])
	}
	wg.Wait()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.Close()
		}()
	}
	wg.Wait()

	var want []string
	for _, host := range hosts {
		want = append(want, sessionSocket(t, s, host))
	}
	sort.Strings(want)
	if got := rec.sockets(); !equalStrings(got, want) {
		t.Errorf("concurrent Close closed %v, want each socket once: %v", got, want)
	}
}

// TestSessionsDoNotShareMasters pins that two sessions talking to the same host
// use different sockets, so one closing cannot cut the other off.
func TestSessionsDoNotShareMasters(t *testing.T) {
	t.Parallel()

	a, recA := newRecordingSession(nil)
	b, recB := newRecordingSession(nil)
	host := "docket-test@127.0.0.1:1"
	_ = dispatch(a.Context(context.Background(), Target{Host: host}))
	_ = dispatch(b.Context(context.Background(), Target{Host: host}))

	if sessionSocket(t, a, host) == sessionSocket(t, b, host) {
		t.Fatal("two sessions on one host share a control socket")
	}
	_ = a.Close()
	if got := recB.sockets(); len(got) != 0 {
		t.Errorf("closing one session closed %v in the other", got)
	}
	if got := recA.sockets(); len(got) != 1 || got[0] != sessionSocket(t, a, host) {
		t.Errorf("closing a session closed %v, want only its own socket", got)
	}
}
