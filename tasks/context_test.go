package tasks

import "context"

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
