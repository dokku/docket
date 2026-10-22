// Package sdk is the supported way to drive docket's engine from Go: build and
// run tasks, read a server back as a recipe or one resource at a time, and
// describe the registered task types.
//
// Everything else in the module lives under internal/ and cannot be imported
// from outside it. The recipe loader, the codecs, template rendering and the
// CLI are docket's own business; this package is the part meant to be built
// on.
//
// # Stability
//
// The exported identifiers in this package are stable from the release they
// first ship in. While docket is at 0.x, a change that breaks a caller only
// ships in a release that bumps the minor version, and the release notes call
// it out. From 1.0 on, only in a major version.
//
// The one exception is the fields of the task types (AppTask, ConfigTask and
// so on). They mirror the recipe format, and change when it does: a field
// added, renamed or removed in a recipe is added, renamed or removed here in
// the same release.
//
// Types are aliases of the engine's own, so a value built here is the value
// the engine runs. Every type their exported fields and methods take or return
// is named here too.
//
// # Runs
//
// Every call takes a context.Context, and the context carries what a run needs:
// the server to send dokku commands to and the values to mask. Build it from a
// Session, which also owns the SSH connections made under it:
//
//	session := sdk.NewSession(sdk.NewMasker("s3cr3t"))
//	defer func() { _ = session.Close() }()
//	ctx := session.Context(context.Background(), sdk.Target{Host: "dokku@dokku.example.com"})
//
// The zero Target runs dokku locally. Pass WithVersionGate to NewSession to
// refuse tasks the server is too old for, with the server's versions read once
// per session however many contexts it hands out.
//
// Tasks executed at once under one session take turns on the same app or
// datastore service, so concurrent callers do not collide on dokku's deploy
// lock. Hold the same locks across a separate Plan and ExecutePlan with
// LockTask.
//
// # Testing
//
// A test can drive tasks without a server by installing a fake runner with
// ContextWithRunner. The fake answers every command a call makes, and since it
// travels on the context, tests with different fakes can run in parallel.
// Production code must not use it.
//
// Package dokkutest is a stateful fake built on it: it holds apps, services
// and their settings, answers the commands the tasks send, applies their
// changes, and records every call, so a test can assert on the state a task
// leaves rather than on the commands it ran.
package sdk

//go:generate go run ../generate/sdk
