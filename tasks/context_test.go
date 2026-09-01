package tasks

import (
	"context"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// testCtx is the context unit tests pass to Plan, Execute and the helpers
// below them. It exists so a test file can call `t.Plan(testCtx())` without
// importing the context package, which matters because a great many of these
// files already bind `context` to the sigil input map they build for
// GetPlays - naming the package there would not compile.
//
// Tests that need cancellation or a target build their own context instead.
func testCtx() context.Context {
	return context.Background()
}

// isolateMaskRegistry gives the test an empty sensitive-value registry and puts
// the previous set back when it finishes.
//
// Restoring rather than clearing is what lets TestMain's end-of-run check mean
// anything. Production code in this package registers sensitive values and
// never clears them - tasks/properties.go does it for a property marked
// sensitive, registerSensitiveMapValues does it for a map field - so residue
// outlives the test that created it. A test that cleared on the way out would
// wipe an earlier test's residue and hide exactly what that check looks for.
func isolateMaskRegistry(t *testing.T) {
	t.Helper()
	prev := subprocess.GlobalSensitive()
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(prev) })
}
