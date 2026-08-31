package subprocess

import "context"

// Target describes where a dokku command runs and how it is wrapped. It is the
// per-invocation replacement for what used to be three pieces of process
// state: the `defaultHost` package variable, and the DOKKU_SUDO and
// DOKKU_SSH_ACCEPT_NEW_HOST_KEYS environment variables the SSH argv builder
// read back at dispatch time. Being per-invocation is what lets one process
// talk to two servers - concurrently, or from a caller embedding this package
// as a library.
//
// The zero Target runs dokku locally with no elevation, which is docket's
// behaviour when neither --host nor DOKKU_HOST is set.
type Target struct {
	// Host is the remote as [user@]host[:port]. When non-empty, `dokku`
	// invocations are routed through an `ssh` subprocess instead of running
	// locally. Only `dokku` is routed: a task's local helper commands (docker,
	// curl, tar) stay local, because the remote side may not have them.
	Host string

	// Sudo wraps the dokku invocation so it runs as root: `sudo -n` on the
	// remote when Host is set, `sudo -n -u root` locally when it is not.
	// Passwordless sudo only - `-n` never prompts. Like Host, this applies to
	// `dokku` alone, so a task's local helper commands are not elevated.
	Sudo bool

	// AcceptNewHostKeys adds `-o StrictHostKeyChecking=accept-new`, so ssh
	// trusts an unknown host on first connect. Only meaningful with Host.
	AcceptNewHostKeys bool
}

// targetKey is the context key Target is stored under. An unexported struct
// type cannot collide with a key from any other package.
type targetKey struct{}

// ContextWithTarget returns a copy of ctx carrying target. The commands layer
// calls it once per run with the resolved flags and env; a caller that wants a
// single command sent somewhere else derives a child context rather than
// setting a field on the input, which keeps "which server" a question with one
// answer per call.
func ContextWithTarget(ctx context.Context, target Target) context.Context {
	return context.WithValue(ctx, targetKey{}, target)
}

// TargetFromContext returns the target ctx carries, or the zero Target when it
// carries none. A nil context is treated the same way, so a caller that has not
// been threaded a context yet still runs locally rather than panicking.
func TargetFromContext(ctx context.Context) Target {
	if ctx == nil {
		return Target{}
	}
	target, _ := ctx.Value(targetKey{}).(Target)
	return target
}
