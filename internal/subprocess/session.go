package subprocess

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"time"
)

// ErrSessionClosed is the error a call made under a closed session fails with,
// wrapped in the error the call returns. Test for it with errors.Is.
//
//sdk:export
var ErrSessionClosed = errors.New("ssh session is closed")

// sessionIDs hands out Session ids. They start at 1 so that 0 can mean "no
// session" in controlPath.
var sessionIDs atomic.Uint64

// Session owns the lifetime of the SSH connections a run opens. It hands out
// contexts carrying a Target and a Masker, records every ControlMaster a call
// under one of those contexts opens, and closes them all on Close.
//
// Recording happens when a command is dispatched, not when a context is handed
// out. A context derived from a session context with ContextWithTarget - a
// play with its own `host:` - still belongs to the session, so its master is
// closed too, and a host that was never reached costs nothing.
//
// Each session has its own control sockets, so two sessions in one process
// talking to the same host do not share a master, and closing one cannot cut
// off the other.
//
// A master outlives its last command by 60 seconds unless WithControlPersist
// says otherwise, so a run that sends commands one after another reuses one
// connection. WithControlPersist(0) has the session close a master itself as
// soon as no command is using it, for a caller that may never reach Close.
//
// A session can also own per-run state of the caller's choosing, given as
// SessionOptions: every context it hands out carries the same value, so state
// such as the version gate's cache outlives any one call's context.
//
// A Session is safe for concurrent use. Close is idempotent; call it once the
// calls made under its contexts have returned, since a call still in flight
// loses its connection.
//
//sdk:export
type Session struct {
	sessionID uint64
	masker    *Masker

	// values are installed on every context Context returns, in option
	// order, over whatever the parent carries under the same key.
	values []sessionValue

	// persist is how long a master outlives its last command. Zero means
	// the session closes a master itself as soon as no command is using it.
	persist time.Duration

	// closeMaster tears down one master. It is exitControlMaster outside of
	// tests, which replace it to observe what Close would close without an
	// ssh server.
	closeMaster func(target sshTarget, socket string)

	mu      sync.Mutex
	masters map[string]sshTarget
	// inflight counts the commands using each master right now.
	inflight map[string]int
	// closing holds a channel for each master the session is closing because
	// its last command returned. A command for that socket waits for it to
	// close rather than reaching a master that is on its way out.
	closing map[string]chan struct{}
	closed  bool
}

// defaultControlPersist is how long a master outlives its last command in a
// session given no WithControlPersist option, and for a call made outside any
// session.
const defaultControlPersist = 60 * time.Second

// SessionOption configures a Session when NewSession builds it.
//
//sdk:export
type SessionOption func(*Session)

// sessionValue is one context value a session owns.
type sessionValue struct {
	key   any
	value any
}

// WithSessionValue makes every context the session hands out carry, under
// key, a value newValue builds once when the session is created. Building it
// then, rather than when the option is made, keeps two sessions given the
// same option from sharing one value.
func WithSessionValue(key any, newValue func() any) SessionOption {
	return func(s *Session) {
		s.values = append(s.values, sessionValue{key: key, value: newValue()})
	}
}

// WithControlPersist sets how long each SSH connection the session opens stays
// open after its last command returns, in place of the default 60 seconds. It
// is rounded up to whole seconds, which is what ssh's ControlPersist counts.
//
// Zero, or a negative d, closes a connection as soon as no command under the
// session is using it: the call that finishes the last command closes the
// connection before it returns. That suits an embedder with nowhere to call
// Close, such as a process that is killed rather than shut down, at the cost
// of a new connection for each command that runs after the previous one
// finished. Commands that overlap still share one.
//
//sdk:export
func WithControlPersist(d time.Duration) SessionOption {
	if d < 0 {
		d = 0
	}
	return func(s *Session) {
		s.persist = d
	}
}

// NewSession returns a session whose contexts carry masker and whatever opts
// install. A nil masker leaves whatever masker the parent context carries in
// place.
func NewSession(masker *Masker, opts ...SessionOption) *Session {
	s := &Session{
		sessionID:   sessionIDs.Add(1),
		masker:      masker,
		persist:     defaultControlPersist,
		closeMaster: exitControlMaster,
		masters:     map[string]sshTarget{},
		inflight:    map[string]int{},
		closing:     map[string]chan struct{}{},
	}
	for _, opt := range opts {
		if opt != nil {
			opt(s)
		}
	}
	return s
}

// sessionKey is the context key a Session is stored under.
type sessionKey struct{}

// Context returns a copy of parent carrying target, the session's masker, the
// values its options installed, and the session itself, so every SSH
// connection a call under it opens is closed by Close.
func (s *Session) Context(parent context.Context, target Target) context.Context {
	ctx := ContextWithTarget(parent, target)
	if s.masker != nil {
		ctx = ContextWithMasker(ctx, s.masker)
	}
	for _, v := range s.values {
		ctx = context.WithValue(ctx, v.key, v.value)
	}
	return context.WithValue(ctx, sessionKey{}, s)
}

// Close tears down every ControlMaster opened under the session. It is
// best-effort, like CloseSshControlMaster: a master that has already exited is
// skipped, and the error is always nil. Calling it again does nothing.
func (s *Session) Close() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	masters := s.masters
	s.masters = nil
	s.mu.Unlock()

	for socket, target := range masters {
		s.closeMaster(target, socket)
	}
	return nil
}

// sessionFromContext returns the Session ctx belongs to, or nil when it
// belongs to none.
func sessionFromContext(ctx context.Context) *Session {
	if ctx == nil {
		return nil
	}
	s, _ := ctx.Value(sessionKey{}).(*Session)
	return s
}

// id returns the session's id, or 0 for a nil session.
func (s *Session) id() uint64 {
	if s == nil {
		return 0
	}
	return s.sessionID
}

// controlPersist returns how long the session's masters outlive their last
// command, or the default for a nil session.
func (s *Session) controlPersist() time.Duration {
	if s == nil {
		return defaultControlPersist
	}
	return s.persist
}

// track records that a call is about to use the master on socket, and returns
// the function the call runs once its command has returned. It refuses once
// the session is closed, since a master opened then would outlive the session
// with nothing left to close it. A nil session tracks nothing.
//
// When the session's persist is zero, the release that drops a master's last
// command closes the master before it returns. A call for that socket that
// arrives meanwhile waits until the close is done, then opens a new master,
// rather than reaching one that is exiting.
func (s *Session) track(target sshTarget, socket string) (func(), error) {
	if s == nil {
		return func() {}, nil
	}
	s.mu.Lock()
	for {
		if s.closed {
			s.mu.Unlock()
			return nil, ErrSessionClosed
		}
		done, ok := s.closing[socket]
		if !ok {
			break
		}
		s.mu.Unlock()
		<-done
		s.mu.Lock()
	}
	s.masters[socket] = target
	s.inflight[socket]++
	s.mu.Unlock()

	var once sync.Once
	return func() { once.Do(func() { s.release(socket) }) }, nil
}

// release records that one call using the master on socket has returned, and
// closes the master when it was the last and the session's persist is zero.
func (s *Session) release(socket string) {
	s.mu.Lock()
	s.inflight[socket]--
	if s.inflight[socket] > 0 {
		s.mu.Unlock()
		return
	}
	delete(s.inflight, socket)
	if s.persist > 0 || s.closed {
		// A persisting master is left to ssh's ControlPersist and to Close;
		// after Close, Close has already taken it.
		s.mu.Unlock()
		return
	}
	target := s.masters[socket]
	delete(s.masters, socket)
	done := make(chan struct{})
	s.closing[socket] = done
	s.mu.Unlock()

	s.closeMaster(target, socket)

	s.mu.Lock()
	delete(s.closing, socket)
	close(done)
	s.mu.Unlock()
}
