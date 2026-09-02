package commands

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/tasks"
)

// StubTask is a test-only Task type registered as `dokku_stub` so apply
// / plan tests can drive the executor without contacting a Dokku
// server. Each stub instance carries a `Key` field that maps into the
// per-key fixture in stubFixtures; the test sets the desired
// TaskOutputState for that key and the executor picks it up via
// Execute(). Plan() returns a Drift / InSync result based on the
// fixture's Changed flag plus its Error.
type StubTask struct {
	// Key is the lookup into stubFixtures. Required, and the stub's identity:
	// one fixture per key is exactly the "which resource" question every real
	// task answers with its `identity:"key"` fields.
	Key string `yaml:"key" required:"true" identity:"key"`
}

// StubTaskExample wraps the stub under its recipe key, the way every real
// task's example type does.
type StubTaskExample struct {
	// Name is the task name holding the StubTask description
	Name string `yaml:"-"`

	// StubTask is the StubTask configuration
	StubTask StubTask `yaml:"dokku_stub"`
}

// GetName returns the name of the example
func (e StubTaskExample) GetName() string { return e.Name }

// Doc / Examples are not exercised by the apply / plan tests. They exist so
// StubTask satisfies the Task interface.
func (t StubTask) Doc() string { return "stub task for tests" }

func (t StubTask) Examples() ([]tasks.Doc, error) {
	return tasks.MarshalExamples([]StubTaskExample{
		{Name: "Run the stub", StubTask: StubTask{Key: "example"}},
	})
}

// ExportSupport / ProbeSupport are declared for the same reason every real
// task declares them: the stub stands in for one, and anything that walks the
// registry - the task catalog, --list-tasks - would otherwise see a task with
// no declaration at all, which the coverage tests make impossible for a task
// that actually ships.
func (t StubTask) ExportSupport() tasks.ExportSupport {
	return tasks.ExportSupport{Status: tasks.ExportUnsupported, Caveat: "a test fixture has no server state to read back"}
}

func (t StubTask) ProbeSupport() tasks.ProbeSupport {
	return tasks.ProbeSupport{Status: tasks.ProbeSupported}
}

func (t StubTask) Plan(ctx context.Context) tasks.PlanResult {
	fixture := stubFixtureFor(ctx, t.Key)
	if fixture.Hook != nil {
		fixture.Hook()
	}
	if fixture.PlanError != nil {
		return tasks.PlanResult{
			Status:       tasks.PlanStatusError,
			Error:        fixture.PlanError,
			DesiredState: tasks.StatePresent,
			Stdout:       fixture.Stdout,
			Stderr:       fixture.Stderr,
			ExitCode:     fixture.ExitCode,
		}
	}
	if !fixture.Changed && fixture.ExecuteError == nil {
		// Warnings ride out on the in-sync result too. A task can read its
		// state perfectly well, find a difference, and decline to reconcile
		// it - dokku_service_create's image_drift does exactly that - so the
		// stub has to be able to produce an in-sync-with-warning plan.
		return tasks.PlanResult{
			InSync:       true,
			Status:       tasks.PlanStatusOK,
			DesiredState: tasks.StatePresent,
			Warnings:     fixture.Warnings,
		}
	}
	return tasks.PlanResult{
		Status:       tasks.PlanStatusModify,
		DesiredState: tasks.StatePresent,
		Warnings:     fixture.Warnings,
	}
}

func (t StubTask) Execute(ctx context.Context) tasks.TaskOutputState {
	fixture := stubFixtureFor(ctx, t.Key)
	if fixture.Hook != nil {
		fixture.Hook()
	}
	if fixture.ExecuteError != nil {
		return tasks.TaskOutputState{
			Error:        fixture.ExecuteError,
			Stdout:       fixture.Stdout,
			Stderr:       fixture.Stderr,
			ExitCode:     fixture.ExitCode,
			DesiredState: tasks.StatePresent,
			State:        tasks.StatePresent,
		}
	}
	if fixture.PlanError != nil {
		return tasks.TaskOutputState{
			Error:        fixture.PlanError,
			Stdout:       fixture.Stdout,
			Stderr:       fixture.Stderr,
			ExitCode:     fixture.ExitCode,
			DesiredState: tasks.StatePresent,
			State:        tasks.StatePresent,
		}
	}
	if fixture.MismatchState {
		return tasks.TaskOutputState{
			DesiredState: tasks.StatePresent,
			State:        tasks.StateAbsent,
			Changed:      fixture.Changed,
		}
	}
	return tasks.TaskOutputState{
		Changed:      fixture.Changed,
		DesiredState: tasks.StatePresent,
		State:        tasks.StatePresent,
		Stdout:       fixture.Stdout,
		Stderr:       fixture.Stderr,
		ExitCode:     fixture.ExitCode,
		Warnings:     fixture.Warnings,
	}
}

// StubFixture controls the TaskOutputState a StubTask returns for a
// given Key. Tests set fields here and the stub task echoes them back
// from Execute() / Plan().
type StubFixture struct {
	Changed       bool
	ExecuteError  error
	PlanError     error
	Stdout        string
	Stderr        string
	ExitCode      int
	MismatchState bool
	// Warnings are echoed onto the drift PlanResult (plan mode) and the
	// success TaskOutputState (apply mode) so tests can drive the run loops'
	// warning drain. #353.
	Warnings []tasks.PlanWarning
	// Hook, when set, runs as the first thing Plan and Execute do. It exists
	// so a test can make something happen *during* the run - cancelling the
	// run context, recording that this task was reached - at a deterministic
	// point rather than racing the executor from another goroutine.
	Hook func()
}

// stubFixtures is one test's set of stub answers, keyed the way that test
// chose. It travels to the task on the run context, so two tests can use the
// same key for different answers.
type stubFixtures map[string]StubFixture

// stubFixturesKey is the context key a test's fixture set travels under.
type stubFixturesKey struct{}

// withStubFixtures returns a context carrying f, which is how the stub task
// finds its answers without a package-level map.
func withStubFixtures(ctx context.Context, f stubFixtures) context.Context {
	if len(f) == 0 {
		return ctx
	}
	return context.WithValue(ctx, stubFixturesKey{}, f)
}

// stubFixtureFor returns the fixture registered for key on ctx, or the zero
// fixture when none is - which is the "nothing to do, task is in sync" answer.
func stubFixtureFor(ctx context.Context, key string) StubFixture {
	if ctx == nil {
		return StubFixture{}
	}
	f, _ := ctx.Value(stubFixturesKey{}).(stubFixtures)
	return f[key]
}

// pendingStubs collects what each test registered before it started a run.
// Keyed by the test rather than by fixture name, so the names tests pick -
// "a" appears in 38 of them - cannot collide, and finishing one test cannot
// wipe another's answers the way the single shared map it replaced did.
var (
	pendingMu sync.Mutex
	pending   = map[*testing.T]stubFixtures{}
)

// stubSet registers a fixture for this test. The run helpers lift what a test
// registered onto the context they build, so it reaches the task from there.
func stubSet(t *testing.T, key string, f StubFixture) {
	t.Helper()
	pendingMu.Lock()
	defer pendingMu.Unlock()
	if pending[t] == nil {
		pending[t] = stubFixtures{}
		t.Cleanup(func() {
			pendingMu.Lock()
			defer pendingMu.Unlock()
			delete(pending, t)
		})
	}
	pending[t][key] = f
}

// stubsFor returns a copy of what t registered, so the run reads a set nothing
// can mutate underneath it.
func stubsFor(t *testing.T) stubFixtures {
	pendingMu.Lock()
	defer pendingMu.Unlock()
	out := stubFixtures{}
	for k, v := range pending[t] {
		out[k] = v
	}
	return out
}

// stubExecError returns an error that, when threaded through the
// production ExecutePlan path, would have populated Stderr. Used in
// tests that exercise `failed_when: result.Stderr contains "..."`.
func stubExecError(stderr string) error {
	return errors.New(stderr)
}

func init() {
	tasks.RegisterTask(&StubTask{})
}

// TestStubFixturesDoNotCollideAcrossTests is the property #506 exists for.
// The registry this replaced was a single process-wide map keyed by whatever
// name each test picked, and the names collide hard - "a" appears in 38 tests.
// Two of them running at once clobbered each other, and either one finishing
// wiped the whole map while the other was still running.
//
// Both subtests register "a" with opposite answers and run in parallel. Under
// the old map one of them would have read the other's fixture, or none at all.
func TestStubFixturesDoNotCollideAcrossTests(t *testing.T) {
	t.Parallel()

	cases := map[string]bool{"changed": true, "unchanged": false}
	for name, changed := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			stubSet(t, "a", StubFixture{Changed: changed})

			path := writeTasksFile(t, `---
- tasks:
    - name: only
      dokku_stub: { key: a }
`)
			stdout, _, exit := runApply(t, path, "--detailed-exitcode")
			if exit != map[bool]int{true: 2, false: 0}[changed] {
				t.Errorf("exit = %d for Changed=%v; the other subtest's fixture answered", exit, changed)
			}
			want := map[bool]string{true: "[changed]", false: "[ok]"}[changed]
			if !strings.Contains(stdout, want) {
				t.Errorf("output should contain %q for Changed=%v:\n%s", want, changed, stdout)
			}
		})
	}
}
