package tasks

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/dokku/docket/internal/subprocess"
)

// taskLocksKey is the context key for the lock table. The table is a pointer,
// so a play context derived with ContextWithTarget still sees the one the
// run installed.
type taskLocksKey struct{}

// heldTaskLocksKey is the context key for the locks a caller already holds.
type heldTaskLocksKey struct{}

// taskLocks serializes concurrent tasks on the same object. Each object on
// each server is one entry: a channel holding a token while some task has it.
// A session-owned table serves every call the session hands a context to, so
// two tasks an embedder runs at once on one app take turns instead of
// colliding on dokku's deploy lock.
type taskLocks struct {
	mu      sync.Mutex
	entries map[string]*taskLockEntry
}

// taskLockEntry is one object's lock. refs counts its holder and waiters, so
// the entry is dropped once nobody needs it and the table does not grow with
// every app a long-lived session has touched.
type taskLockEntry struct {
	token chan struct{}
	refs  int
}

func newTaskLocks() *taskLocks {
	return &taskLocks{entries: map[string]*taskLockEntry{}}
}

// acquire takes the lock on key, waiting for its holder to release it. A
// waiter whose ctx ends stops waiting with ctx's error and holds nothing.
func (l *taskLocks) acquire(ctx context.Context, key string) error {
	l.mu.Lock()
	entry, ok := l.entries[key]
	if !ok {
		entry = &taskLockEntry{token: make(chan struct{}, 1)}
		l.entries[key] = entry
	}
	entry.refs++
	l.mu.Unlock()

	select {
	case entry.token <- struct{}{}:
		return nil
	case <-ctx.Done():
		l.drop(key, entry)
		return ctx.Err()
	}
}

// release gives up the lock on key, which the caller holds.
func (l *taskLocks) release(key string) {
	l.mu.Lock()
	entry := l.entries[key]
	l.mu.Unlock()
	<-entry.token
	l.drop(key, entry)
}

// drop forgets one holder or waiter of entry.
func (l *taskLocks) drop(key string, entry *taskLockEntry) {
	l.mu.Lock()
	defer l.mu.Unlock()
	entry.refs--
	if entry.refs == 0 {
		delete(l.entries, key)
	}
}

// heldTaskLocks is the set of locks a context's caller holds, and the table
// they were taken from. It is never changed once stored on a context: a
// nested LockTask stores a new one.
type heldTaskLocks struct {
	table *taskLocks
	keys  map[string]bool
	max   taskLockKey
}

// taskLockKey is one lock a task needs. key orders the locks and picks the
// table entry; label names the object in an error.
type taskLockKey struct {
	key   string
	label string
}

// ContextWithTaskLocks returns a context on which Execute serializes tasks
// that change the same app or datastore service. A context that already
// carries a table is returned as-is, so installing it twice does not split
// it. Without a table, Execute and LockTask lock nothing.
func ContextWithTaskLocks(ctx context.Context) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if taskLocksFrom(ctx) != nil {
		return ctx
	}
	return context.WithValue(ctx, taskLocksKey{}, newTaskLocks())
}

// SessionTaskLocks is a session option that installs one lock table on every
// context the session hands out, so every call it serves - each with its own
// context and cancellation - takes turns on the same app or service. Each
// session built with it gets its own table. The session's table replaces one
// already on the parent context.
func SessionTaskLocks() subprocess.SessionOption {
	return subprocess.WithSessionValue(taskLocksKey{}, func() any { return newTaskLocks() })
}

func taskLocksFrom(ctx context.Context) *taskLocks {
	if ctx == nil {
		return nil
	}
	table, _ := ctx.Value(taskLocksKey{}).(*taskLocks)
	return table
}

// taskLockKeys returns the locks t needs on the server ctx targets, sorted.
// The app sorts before the service, so a task needing both - and any two tasks
// sharing one - take them in the same order. An object t leaves unset, such
// as the app of a task with `global: true`, needs none.
func taskLockKeys(ctx context.Context, t Task) []taskLockKey {
	target := targetCacheKey(ctx)
	var keys []taskLockKey
	for _, object := range TaskObjects(t) {
		if !object.Set {
			continue
		}
		keys = append(keys, taskLockKey{
			key:   string(object.Kind) + "\x00" + target + "\x00" + strings.Join(object.Values, "\x00"),
			label: string(object.Kind) + " " + strings.Join(object.Values, "/"),
		})
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i].key < keys[j].key })
	return keys
}

// LockTask takes the locks t needs: one for each app or datastore service it
// changes, on the server ctx targets. It returns a context recording them, so
// an Execute under it does not wait on the locks its caller already holds,
// and a function that releases them. The function is safe to call more than
// once.
//
// Execute takes the same locks itself. LockTask is for a caller that plans
// and applies separately: hold the locks across both, so no other task on the
// same object runs between the plan and its apply.
//
// A context with no lock table returns ctx and locks nothing. A caller whose
// ctx ends while it waits gets ctx's error and holds nothing. Locks are always
// taken in one order, so a nested call that needs a lock ordered before one
// its caller holds - executing a dokku_service_link while holding only its
// service - is refused rather than risking a deadlock: lock the task that
// needs both first.
func LockTask(ctx context.Context, t Task) (context.Context, func(), error) {
	table := taskLocksFrom(ctx)
	if table == nil || t == nil {
		return ctx, func() {}, nil
	}

	held, _ := ctx.Value(heldTaskLocksKey{}).(*heldTaskLocks)
	if held != nil && held.table != table {
		held = nil
	}

	var need []taskLockKey
	for _, key := range taskLockKeys(ctx, t) {
		if held != nil && held.keys[key.key] {
			continue
		}
		if held != nil && key.key < held.max.key {
			return ctx, func() {}, fmt.Errorf("%s: cannot lock %s while holding only %s; lock the task that changes both first", typeKeyOf(t), key.label, held.max.label)
		}
		need = append(need, key)
	}
	if len(need) == 0 {
		return ctx, func() {}, nil
	}

	for i, key := range need {
		if err := table.acquire(ctx, key.key); err != nil {
			for j := i - 1; j >= 0; j-- {
				table.release(need[j].key)
			}
			return ctx, func() {}, fmt.Errorf("waiting for the lock on %s: %w", key.label, err)
		}
	}

	next := &heldTaskLocks{table: table, keys: map[string]bool{}}
	if held != nil {
		for key := range held.keys {
			next.keys[key] = true
		}
		next.max = held.max
	}
	for _, key := range need {
		next.keys[key.key] = true
		if key.key > next.max.key {
			next.max = key
		}
	}

	var once sync.Once
	release := func() {
		once.Do(func() {
			for i := len(need) - 1; i >= 0; i-- {
				table.release(need[i].key)
			}
		})
	}
	return context.WithValue(ctx, heldTaskLocksKey{}, next), release, nil
}

// executeTask is the canonical implementation of Task.Execute: it takes the
// locks t needs, plans it, and applies the plan before releasing them, so no
// other task on the same app or service runs between its plan and its apply.
// TestEveryExecuteTakesTaskLocks fails the build for an Execute that skips it.
func executeTask(ctx context.Context, t Task) TaskOutputState {
	ctx, release, err := LockTask(ctx, t)
	if err != nil {
		return TaskOutputState{Error: err, Message: err.Error()}
	}
	defer release()
	return ExecutePlan(ctx, t.Plan(ctx))
}
