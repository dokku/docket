package subprocess

import (
	"context"
	"errors"
	"runtime"
	"testing"
	"time"
)

// TestCallExecCommandHonorsAlreadyCancelledContext pins that a context which
// is already dead short-circuits rather than running the command. Before the
// context reached this far, the only thing that could stop a subprocess was a
// signal handled inside this package, so a caller holding a cancelled context
// had no way to prevent the next command from running to completion.
func TestCallExecCommandHonorsAlreadyCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	resp, err := CallExecCommand(ctx, ExecCommandInput{
		Command: "true",
	})
	if err == nil {
		t.Fatal("expected an error from an already-cancelled context")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("err = %v, want it to wrap context.Canceled", err)
	}
	if !resp.Cancelled {
		t.Error("response should report Cancelled")
	}
}

// TestProbePropagatesCancellation pins that a cancelled probe is reported as a
// failure rather than as "the probed state is absent". Reading a cancellation
// as absence would make a plan claim drift and an apply then mutate a server
// it never successfully read.
func TestProbePropagatesCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	ok, err := Probe(ctx, ExecCommandInput{Command: "true"})
	if err == nil {
		t.Fatal("expected a cancelled probe to propagate its error")
	}
	if ok {
		t.Error("a cancelled probe must not report a match")
	}
}

// TestCallExecCommandDeadlineBoundsTheChild pins that a deadline actually
// bounds a slow command, which is the property an embedding caller needs and
// a signal handler cannot provide.
func TestCallExecCommandDeadlineBoundsTheChild(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := CallExecCommand(ctx, ExecCommandInput{
		Command: "sleep",
		Args:    []string{"10"},
	}); err == nil {
		t.Fatal("expected the deadline to end the call")
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Errorf("call took %s; the deadline did not bound the child", elapsed)
	}
}

// TestCallExecCommandDoesNotLeakGoroutines is the regression lock on removing
// the per-call signal handler. Each call used to register a channel with
// signal.Notify without ever calling signal.Stop, and park a goroutine on that
// channel that nothing ever woke - so a run leaked one goroutine, one channel
// registration and one derived context per dokku command it issued.
func TestCallExecCommandDoesNotLeakGoroutines(t *testing.T) {
	ctx := context.Background()
	// Warm up so one-off runtime goroutines are not counted as growth.
	for i := 0; i < 5; i++ {
		_, _ = CallExecCommand(ctx, ExecCommandInput{Command: "true"})
	}
	settle()
	before := runtime.NumGoroutine()

	for i := 0; i < 100; i++ {
		if _, err := CallExecCommand(ctx, ExecCommandInput{Command: "true"}); err != nil {
			t.Fatalf("CallExecCommand: %v", err)
		}
	}
	settle()

	if grew := runtime.NumGoroutine() - before; grew > 5 {
		t.Errorf("goroutine count grew by %d over 100 calls; the per-call handler is leaking again", grew)
	}
}

// settle gives finished commands a moment to release their goroutines before
// the count is read, so the assertion measures a leak rather than a race with
// normal teardown.
func settle() {
	for i := 0; i < 10; i++ {
		runtime.Gosched()
		time.Sleep(10 * time.Millisecond)
	}
	runtime.GC()
}
