package commands

import (
	"errors"
	"sync"

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
	// Key is the lookup into stubFixtures. Required.
	Key string `yaml:"key" required:"true"`
}

// Doc / Examples are not exercised by the apply / plan tests. They
// exist so StubTask satisfies the Task interface.
func (t StubTask) Doc() string                    { return "stub task for tests" }
func (t StubTask) Examples() ([]tasks.Doc, error) { return nil, nil }

func (t StubTask) Plan() tasks.PlanResult {
	fixture := stubGet(t.Key)
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
		return tasks.PlanResult{
			InSync:       true,
			Status:       tasks.PlanStatusOK,
			DesiredState: tasks.StatePresent,
		}
	}
	return tasks.PlanResult{
		Status:       tasks.PlanStatusModify,
		DesiredState: tasks.StatePresent,
	}
}

func (t StubTask) Execute() tasks.TaskOutputState {
	fixture := stubGet(t.Key)
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
}

var (
	stubMu       sync.Mutex
	stubFixtures = map[string]StubFixture{}
)

func stubSet(key string, f StubFixture) {
	stubMu.Lock()
	defer stubMu.Unlock()
	stubFixtures[key] = f
}

func stubGet(key string) StubFixture {
	stubMu.Lock()
	defer stubMu.Unlock()
	return stubFixtures[key]
}

func stubReset() {
	stubMu.Lock()
	defer stubMu.Unlock()
	stubFixtures = map[string]StubFixture{}
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
