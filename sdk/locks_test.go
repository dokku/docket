package sdk_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/dokku/docket/sdk"
)

// callerKey labels the context of one call in a lock test.
type callerKey struct{}

// orderedRunner is a fake dokku that records which caller sent each command
// as the command starts and again as it returns, so a command another call
// runs alongside shows up between the two. The first command from blockFirst waits on release, so a test can
// hold one call inside dokku while another starts.
type orderedRunner struct {
	blockFirst string
	entered    chan struct{}
	release    chan struct{}
	once       sync.Once
	mu         sync.Mutex
	callers    []string
}

func newOrderedRunner(blockFirst string) *orderedRunner {
	return &orderedRunner{blockFirst: blockFirst, entered: make(chan struct{}), release: make(chan struct{})}
}

func (r *orderedRunner) run(ctx context.Context, _ sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
	caller, _ := ctx.Value(callerKey{}).(string)
	r.mu.Lock()
	r.callers = append(r.callers, caller)
	r.mu.Unlock()
	if caller == r.blockFirst {
		r.once.Do(func() {
			close(r.entered)
			<-r.release
		})
	}
	r.mu.Lock()
	r.callers = append(r.callers, caller)
	r.mu.Unlock()
	return sdk.ExecCommandResponse{}, nil
}

// sequence returns the callers in the order their commands ran, with each
// run of one caller collapsed: "first", "second" means every command of the
// first call ran before any of the second's.
func (r *orderedRunner) sequence() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for _, caller := range r.callers {
		if len(out) == 0 || out[len(out)-1] != caller {
			out = append(out, caller)
		}
	}
	return out
}

func newConfig(t *testing.T, app string) *sdk.ConfigTask {
	t.Helper()
	task, err := sdk.NewTask("dokku_config")
	if err != nil {
		t.Fatal(err)
	}
	config := task.(*sdk.ConfigTask)
	config.App = app
	config.Config = map[string]string{"KEY": "value"}
	return config
}

// TestSessionSerializesExecuteOnOneApp pins the default an embedder gets: two
// Execute calls on one app under a plain NewSession take turns, so the second
// sends nothing to dokku until the first is done.
func TestSessionSerializesExecuteOnOneApp(t *testing.T) {
	t.Parallel()
	runner := newOrderedRunner("first")
	session := sdk.NewSession(nil)
	defer func() { _ = session.Close() }()
	ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), runner.run)

	first, second := newConfig(t, "api"), newApp(t, "api")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		first.Execute(context.WithValue(ctx, callerKey{}, "first"))
	}()
	<-runner.entered
	go func() {
		defer wg.Done()
		second.Execute(context.WithValue(ctx, callerKey{}, "second"))
	}()
	// Give the second call time to reach dokku if nothing held it back.
	time.Sleep(50 * time.Millisecond)
	close(runner.release)
	wg.Wait()

	if got := runner.sequence(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("callers reached dokku in the order %v, want every command of the first before the second's", got)
	}
}

// TestLockTaskHoldsAcrossPlanAndExecutePlan pins the plan-then-apply shape: a
// caller holding LockTask between Plan and ExecutePlan keeps an Execute on the
// same app out until it releases.
func TestLockTaskHoldsAcrossPlanAndExecutePlan(t *testing.T) {
	t.Parallel()
	runner := newOrderedRunner("")
	session := sdk.NewSession(nil)
	defer func() { _ = session.Close() }()
	ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), runner.run)

	// The fake answers apps:exists with success, so an absent app plans a
	// destroy and its apply sends a command of its own.
	planner := newApp(t, "api")
	planner.State = sdk.StateAbsent
	plannerCtx := context.WithValue(ctx, callerKey{}, "planner")
	held, release, err := sdk.LockTask(plannerCtx, planner)
	if err != nil {
		t.Fatal(err)
	}
	plan := planner.Plan(held)

	other := newApp(t, "api")
	done := make(chan struct{})
	go func() {
		defer close(done)
		other.Execute(context.WithValue(ctx, callerKey{}, "other"))
	}()
	// Give the other call time to reach dokku if nothing held it back.
	time.Sleep(50 * time.Millisecond)
	if plan.InSync {
		t.Fatal("the planner must plan a change so its apply reaches dokku")
	}
	sdk.ExecutePlan(held, plan)
	release()
	<-done

	if got := runner.sequence(); len(got) != 2 || got[0] != "planner" || got[1] != "other" {
		t.Fatalf("callers reached dokku in the order %v, want the plan and its apply before the other call", got)
	}
}

// TestContextWithTaskLocksWithoutSession pins that a context built without a
// session gets the same serialization from ContextWithTaskLocks, and that
// TaskObjects names what a task locks.
func TestContextWithTaskLocksWithoutSession(t *testing.T) {
	t.Parallel()
	runner := newOrderedRunner("first")
	ctx := sdk.ContextWithTaskLocks(sdk.ContextWithRunner(context.Background(), runner.run))

	objects := sdk.TaskObjects(newConfig(t, "api"))
	if len(objects) != 1 || objects[0].Kind != sdk.ObjectApp || objects[0].Values[0] != "api" {
		t.Fatalf("TaskObjects = %+v, want the app api", objects)
	}

	first, second := newConfig(t, "api"), newConfig(t, "api")
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		first.Execute(context.WithValue(ctx, callerKey{}, "first"))
	}()
	<-runner.entered
	go func() {
		defer wg.Done()
		second.Execute(context.WithValue(ctx, callerKey{}, "second"))
	}()
	time.Sleep(50 * time.Millisecond)
	close(runner.release)
	wg.Wait()

	if got := runner.sequence(); len(got) != 2 || got[0] != "first" || got[1] != "second" {
		t.Fatalf("callers reached dokku in the order %v, want every command of the first before the second's", got)
	}
}
