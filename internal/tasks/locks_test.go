package tasks

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"math/rand"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dokku/docket/internal/subprocess"
)

// gateLabelKey labels the context of one Execute in a lock test, so the
// shared fake runner knows which call a command belongs to.
type gateLabelKey struct{}

// lockGate is a fake dokku for lock tests. The first command a labelled call
// sends reports the label on entered and then waits for that label's release
// channel to close; every other command answers at once. It answers each
// command with an empty success, which is enough for a plan to read and the
// lock test does not care what the plan decides.
type lockGate struct {
	entered chan string
	mu      sync.Mutex
	release map[string]chan struct{}
	seen    map[string]bool
}

func newLockGate(labels ...string) *lockGate {
	g := &lockGate{
		entered: make(chan string, len(labels)),
		release: map[string]chan struct{}{},
		seen:    map[string]bool{},
	}
	for _, label := range labels {
		g.release[label] = make(chan struct{})
	}
	return g
}

func (g *lockGate) runner(ctx context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	label, _ := ctx.Value(gateLabelKey{}).(string)
	g.mu.Lock()
	gate, gated := g.release[label]
	first := gated && !g.seen[label]
	g.seen[label] = true
	g.mu.Unlock()
	if first {
		g.entered <- label
		<-gate
	}
	return subprocess.ExecCommandResponse{}, nil
}

// open releases label's first command.
func (g *lockGate) open(label string) {
	close(g.release[label])
}

// lockTestContext is a context carrying a fresh lock table and g as the
// runner.
func lockTestContext(g *lockGate) context.Context {
	return ContextWithTaskLocks(subprocess.ContextWithRunner(context.Background(), g.runner))
}

// startExecute runs task.Execute under ctx labelled label, and returns the
// channel its state arrives on.
func startExecute(ctx context.Context, label string, task Task) <-chan TaskOutputState {
	done := make(chan TaskOutputState, 1)
	go func() { done <- task.Execute(context.WithValue(ctx, gateLabelKey{}, label)) }()
	return done
}

// awaitEntered waits for label to reach the runner.
func awaitEntered(t *testing.T, g *lockGate, label string) {
	t.Helper()
	select {
	case got := <-g.entered:
		if got != label {
			t.Fatalf("%s reached dokku, want %s", got, label)
		}
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never reached dokku", label)
	}
}

// assertNotEntered fails when any call has reached the runner and not yet
// been read off g.entered.
func assertNotEntered(t *testing.T, g *lockGate) {
	t.Helper()
	select {
	case got := <-g.entered:
		t.Fatalf("%s reached dokku while another task held its lock", got)
	default:
	}
}

// awaitLockRefs waits until the table counts want holders and waiters across
// its entries, which is how a test knows a call is parked on a lock rather
// than not yet started.
func awaitLockRefs(t *testing.T, ctx context.Context, want int) {
	t.Helper()
	table := taskLocksFrom(ctx)
	deadline := time.Now().Add(10 * time.Second)
	for {
		table.mu.Lock()
		got := 0
		for _, entry := range table.entries {
			got += entry.refs
		}
		table.mu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("lock holders and waiters = %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func awaitDone(t *testing.T, label string, done <-chan TaskOutputState) TaskOutputState {
	t.Helper()
	select {
	case state := <-done:
		return state
	case <-time.After(10 * time.Second):
		t.Fatalf("%s never finished", label)
		return TaskOutputState{}
	}
}

func appTask(app string) AppTask {
	return AppTask{App: app, State: StatePresent}
}

func configTask(app string) ConfigTask {
	return ConfigTask{App: app, Config: map[string]string{"KEY": "value"}, State: StatePresent}
}

func servicePropertyTask(service, name string) ServicePropertyTask {
	return ServicePropertyTask{Service: service, Name: name, Property: "memory", Value: "512", State: StatePresent}
}

func serviceLinkTask(app, service, name string) ServiceLinkTask {
	return ServiceLinkTask{App: app, Service: service, Name: name, State: StatePresent}
}

// TestExecuteSerializesTasksOnOneApp pins the issue's own case: two tasks an
// embedder runs at once on the same app take turns, so the second does not
// reach dokku until the first has applied.
func TestExecuteSerializesTasksOnOneApp(t *testing.T) {
	t.Parallel()
	g := newLockGate("config", "app")
	ctx := lockTestContext(g)

	first := startExecute(ctx, "config", configTask("api"))
	awaitEntered(t, g, "config")
	second := startExecute(ctx, "app", appTask("api"))
	awaitLockRefs(t, ctx, 2)
	assertNotEntered(t, g)

	g.open("config")
	awaitDone(t, "config", first)
	awaitEntered(t, g, "app")
	g.open("app")
	awaitDone(t, "app", second)
	awaitLockRefs(t, ctx, 0)
}

// TestExecuteRunsTasksOnDifferentAppsConcurrently pins that the locks are per
// app: tasks on two apps run at the same time.
func TestExecuteRunsTasksOnDifferentAppsConcurrently(t *testing.T) {
	t.Parallel()
	g := newLockGate("api", "web")
	ctx := lockTestContext(g)

	first := startExecute(ctx, "api", configTask("api"))
	second := startExecute(ctx, "web", configTask("web"))
	seen := map[string]bool{}
	for i := 0; i < 2; i++ {
		select {
		case label := <-g.entered:
			seen[label] = true
		case <-time.After(10 * time.Second):
			t.Fatalf("only %v reached dokku; tasks on different apps must not wait on each other", seen)
		}
	}
	g.open("api")
	g.open("web")
	awaitDone(t, "api", first)
	awaitDone(t, "web", second)
}

// TestServiceLinkHoldsTheAppAndTheService pins that a task changing both an
// app and a service holds both: an app task and a service task wait for it,
// while a task on another app does not.
func TestServiceLinkHoldsTheAppAndTheService(t *testing.T) {
	t.Parallel()
	g := newLockGate("link", "config", "property", "other")
	ctx := lockTestContext(g)

	link := startExecute(ctx, "link", serviceLinkTask("api", "redis", "db"))
	awaitEntered(t, g, "link")

	config := startExecute(ctx, "config", configTask("api"))
	property := startExecute(ctx, "property", servicePropertyTask("redis", "db"))
	// The link holds two locks, and each waiter adds one.
	awaitLockRefs(t, ctx, 4)
	assertNotEntered(t, g)

	other := startExecute(ctx, "other", configTask("web"))
	awaitEntered(t, g, "other")
	g.open("other")
	awaitDone(t, "other", other)

	g.open("link")
	awaitDone(t, "link", link)
	for i := 0; i < 2; i++ {
		select {
		case label := <-g.entered:
			g.open(label)
		case <-time.After(10 * time.Second):
			t.Fatal("a task waiting on the link never ran")
		}
	}
	awaitDone(t, "config", config)
	awaitDone(t, "property", property)
	awaitLockRefs(t, ctx, 0)
}

// TestAcquireOnSameServiceAcrossTaskTypes pins that dokku_acl_service, which
// names the instance `service` and the datastore `type`, takes the same lock
// as the datastore tasks that name them the other way round.
func TestAcquireOnSameServiceAcrossTaskTypes(t *testing.T) {
	t.Parallel()
	ctx := ContextWithTaskLocks(context.Background())
	acl := taskLockKeys(ctx, AclServiceTask{Service: "db", Type: "redis"})
	property := taskLockKeys(ctx, servicePropertyTask("redis", "db"))
	if len(acl) != 1 || len(property) != 1 || acl[0].key != property[0].key {
		t.Fatalf("acl_service locks %v, service_property locks %v, want the same service", acl, property)
	}
}

// TestTaskLocksDoNotDeadlock runs overlapping tasks - two links sharing a
// service, and tasks on each of their objects - many times over in shuffled
// order. Locks are always taken in one order, so every run finishes.
func TestTaskLocksDoNotDeadlock(t *testing.T) {
	t.Parallel()
	ctx := ContextWithTaskLocks(subprocess.ContextWithRunner(context.Background(), func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		time.Sleep(10 * time.Microsecond)
		return subprocess.ExecCommandResponse{}, nil
	}))
	tasks := []Task{
		serviceLinkTask("api", "redis", "db"),
		serviceLinkTask("web", "redis", "db"),
		serviceLinkTask("web", "postgres", "main"),
		configTask("api"),
		configTask("web"),
		servicePropertyTask("redis", "db"),
		servicePropertyTask("postgres", "main"),
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		rng := rand.New(rand.NewSource(1))
		for round := 0; round < 50; round++ {
			order := rng.Perm(len(tasks))
			var wg sync.WaitGroup
			for _, i := range order {
				wg.Add(1)
				go func(task Task) {
					defer wg.Done()
					task.Execute(ctx)
				}(tasks[i])
			}
			wg.Wait()
		}
	}()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("overlapping tasks deadlocked")
	}
	awaitLockRefs(t, ctx, 0)
}

// TestCancelledLockWaiterStopsWaiting pins that a call waiting on a lock gives
// up when its context ends, reaches no server, and leaves the lock usable.
func TestCancelledLockWaiterStopsWaiting(t *testing.T) {
	t.Parallel()
	g := newLockGate("holder", "after")
	ctx := lockTestContext(g)

	holder := startExecute(ctx, "holder", configTask("api"))
	awaitEntered(t, g, "holder")

	waiterCtx, cancel := context.WithCancel(ctx)
	waiter := startExecute(waiterCtx, "waiter", configTask("api"))
	awaitLockRefs(t, ctx, 2)
	cancel()
	state := awaitDone(t, "waiter", waiter)
	if !errors.Is(state.Error, context.Canceled) {
		t.Fatalf("waiter error = %v, want context.Canceled", state.Error)
	}
	if !strings.Contains(state.Message, "app api") {
		t.Errorf("waiter message = %q, want it to name the app it waited on", state.Message)
	}
	awaitLockRefs(t, ctx, 1)

	g.open("holder")
	awaitDone(t, "holder", holder)
	after := startExecute(ctx, "after", configTask("api"))
	awaitEntered(t, g, "after")
	g.open("after")
	awaitDone(t, "after", after)
	awaitLockRefs(t, ctx, 0)
}

// TestLockTaskHeldAcrossPlanAndApply pins the plan-then-apply shape: a caller
// holding LockTask keeps every other task on the object out until it releases,
// and an Execute under its context does not wait on the locks it holds.
func TestLockTaskHeldAcrossPlanAndApply(t *testing.T) {
	t.Parallel()
	g := newLockGate("other")
	ctx := lockTestContext(g)

	task := configTask("api")
	held, release, err := LockTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	ExecutePlan(held, task.Plan(held))

	nested := make(chan TaskOutputState, 1)
	go func() { nested <- appTask("api").Execute(held) }()
	awaitDone(t, "nested", nested)

	other := startExecute(ctx, "other", configTask("api"))
	awaitLockRefs(t, ctx, 2)
	assertNotEntered(t, g)

	release()
	release()
	awaitEntered(t, g, "other")
	g.open("other")
	awaitDone(t, "other", other)
	awaitLockRefs(t, ctx, 0)
}

// TestLockTaskRefusesOutOfOrderNesting pins that a nested call needing a lock
// ordered before one its caller holds is refused rather than taken, since
// taking it could deadlock with a task that holds it and waits on the other.
// A nested call needing a later lock takes it.
func TestLockTaskRefusesOutOfOrderNesting(t *testing.T) {
	t.Parallel()
	ctx := lockTestContext(newLockGate())

	held, release, err := LockTask(ctx, servicePropertyTask("redis", "db"))
	if err != nil {
		t.Fatal(err)
	}
	state := serviceLinkTask("api", "redis", "db").Execute(held)
	if state.Error == nil || !strings.Contains(state.Error.Error(), "cannot lock app api while holding only service redis/db") {
		t.Fatalf("nested link error = %v, want the out-of-order refusal", state.Error)
	}
	release()

	held, release, err = LockTask(ctx, configTask("api"))
	if err != nil {
		t.Fatal(err)
	}
	if state := serviceLinkTask("api", "redis", "db").Execute(held); state.Error != nil && strings.Contains(state.Error.Error(), "cannot lock") {
		t.Fatalf("nested link error = %v, want the service lock taken after the held app", state.Error)
	}
	release()
	awaitLockRefs(t, ctx, 0)
}

// TestNoLockTableLocksNothing pins that a context without a table - a bare
// context, as every task unit test uses - serializes nothing.
func TestNoLockTableLocksNothing(t *testing.T) {
	t.Parallel()
	g := newLockGate("first", "second")
	ctx := subprocess.ContextWithRunner(context.Background(), g.runner)

	first := startExecute(ctx, "first", configTask("api"))
	second := startExecute(ctx, "second", configTask("api"))
	for i := 0; i < 2; i++ {
		select {
		case <-g.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("without a lock table, tasks on one app must not wait on each other")
		}
	}
	g.open("first")
	g.open("second")
	awaitDone(t, "first", first)
	awaitDone(t, "second", second)
}

// TestTaskLocksAreScopedToTheServer pins that the same app name on two servers
// is two objects.
func TestTaskLocksAreScopedToTheServer(t *testing.T) {
	t.Parallel()
	g := newLockGate("one", "two")
	ctx := lockTestContext(g)

	first := startExecute(subprocess.ContextWithTarget(ctx, subprocess.Target{Host: "dokku@one"}), "one", configTask("api"))
	second := startExecute(subprocess.ContextWithTarget(ctx, subprocess.Target{Host: "dokku@two"}), "two", configTask("api"))
	for i := 0; i < 2; i++ {
		select {
		case <-g.entered:
		case <-time.After(10 * time.Second):
			t.Fatal("tasks on one app name on two servers must not wait on each other")
		}
	}
	g.open("one")
	g.open("two")
	awaitDone(t, "one", first)
	awaitDone(t, "two", second)
}

// TestGlobalTaskTakesNoLock pins that an app-or-global task with
// `global: true` names no app, so it locks nothing.
func TestGlobalTaskTakesNoLock(t *testing.T) {
	t.Parallel()
	ctx := ContextWithTaskLocks(context.Background())
	if keys := taskLockKeys(ctx, NginxPropertyTask{Global: true, Property: "bind-address-ipv4"}); len(keys) != 0 {
		t.Fatalf("global nginx property locks %v, want none", keys)
	}
	if keys := taskLockKeys(ctx, NginxPropertyTask{App: "api", Property: "bind-address-ipv4"}); len(keys) != 1 {
		t.Fatalf("app nginx property locks %v, want the app", keys)
	}
}

// TestSessionTaskLocksShareOneTable pins that every context a session hands
// out carries the same table, and that two sessions do not share one.
func TestSessionTaskLocksShareOneTable(t *testing.T) {
	t.Parallel()
	session := subprocess.NewSession(nil, SessionTaskLocks())
	other := subprocess.NewSession(nil, SessionTaskLocks())
	a := taskLocksFrom(session.Context(context.Background(), subprocess.Target{}))
	b := taskLocksFrom(session.Context(context.Background(), subprocess.Target{Host: "dokku@one"}))
	c := taskLocksFrom(other.Context(context.Background(), subprocess.Target{}))
	if a == nil || a != b {
		t.Fatal("a session's contexts must share one lock table")
	}
	if a == c {
		t.Fatal("two sessions must not share a lock table")
	}
	if ContextWithTaskLocks(session.Context(context.Background(), subprocess.Target{})) == nil {
		t.Fatal("ContextWithTaskLocks returned nil")
	}
}

// TestEveryExecuteTakesTaskLocks fails the build for a registered task whose
// Execute is anything but `return executeTask(ctx, t)`: one that planned and
// applied itself would skip the locks concurrent sdk callers rely on.
func TestEveryExecuteTakesTaskLocks(t *testing.T) {
	t.Parallel()
	registered := map[string]bool{}
	for _, task := range allRegisteredTasks() {
		registered[taskStructOf(task).Name()] = true
	}

	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	seen := map[string]bool{}
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv == nil || fn.Name.Name != "Execute" || len(fn.Recv.List) != 1 {
				continue
			}
			recv := fn.Recv.List[0].Type
			if star, ok := recv.(*ast.StarExpr); ok {
				recv = star.X
			}
			ident, ok := recv.(*ast.Ident)
			if !ok || !registered[ident.Name] {
				continue
			}
			seen[ident.Name] = true
			if !isExecuteTaskBody(fn.Body) {
				t.Errorf("%s.Execute is not `return executeTask(ctx, t)`", ident.Name)
			}
		}
	}
	for name := range registered {
		if !seen[name] {
			t.Errorf("no Execute method found for registered task %s", name)
		}
	}
}

// isExecuteTaskBody reports whether body is exactly
// `return executeTask(ctx, t)`.
func isExecuteTaskBody(body *ast.BlockStmt) bool {
	if body == nil || len(body.List) != 1 {
		return false
	}
	ret, ok := body.List[0].(*ast.ReturnStmt)
	if !ok || len(ret.Results) != 1 {
		return false
	}
	call, ok := ret.Results[0].(*ast.CallExpr)
	if !ok || len(call.Args) != 2 {
		return false
	}
	ident, ok := call.Fun.(*ast.Ident)
	return ok && ident.Name == "executeTask"
}
