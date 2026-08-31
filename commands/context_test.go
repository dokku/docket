package commands

import "context"

// testCtx is the context the command tests plan and execute under. Same
// reason as the tasks package's copy: `context` is already bound to an input
// map in several of these files.
func testCtx() context.Context {
	return context.Background()
}
