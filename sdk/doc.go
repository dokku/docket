// Package sdk is the supported way to drive docket's engine from Go: build and
// run tasks, read a server back as a recipe, and describe the registered task
// types.
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
//	defer session.Close()
//	ctx := session.Context(context.Background(), sdk.Target{Host: "dokku@dokku.example.com"})
//
// The zero Target runs dokku locally.
package sdk

//go:generate go run ../generate/sdk
