package subprocess

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
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

	// closeMaster tears down one master. It is exitControlMaster outside of
	// tests, which replace it to observe what Close would close without an
	// ssh server.
	closeMaster func(target sshTarget, socket string)

	mu      sync.Mutex
	masters map[string]sshTarget
	closed  bool
}

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

// NewSession returns a session whose contexts carry masker and whatever opts
// install. A nil masker leaves whatever masker the parent context carries in
// place.
func NewSession(masker *Masker, opts ...SessionOption) *Session {
	s := &Session{
		sessionID:   sessionIDs.Add(1),
		masker:      masker,
		closeMaster: exitControlMaster,
		masters:     map[string]sshTarget{},
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

// track records that a call is about to use the master on socket. It refuses
// once the session is closed, since a master opened then would outlive the
// session with nothing left to close it. A nil session tracks nothing.
func (s *Session) track(target sshTarget, socket string) error {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return ErrSessionClosed
	}
	s.masters[socket] = target
	return nil
}
