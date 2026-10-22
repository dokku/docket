package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// emptyCollectionRunner answers an app "web" whose config is empty unless
// held is set, and an app "gone" that does not exist, whose config:export
// still answers with an empty map. Every other command succeeds with no
// output. Once a config:unset runs, held is cleared.
func emptyCollectionRunner(held *atomic.Bool) subprocess.ExecRunner {
	return func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		key := strings.Join(in.Args, " ")
		switch {
		case key == "--quiet apps:exists gone":
			res := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "App gone does not exist"}
			return res, &subprocess.ExecError{Response: res, Err: errors.New(res.Stderr), Ran: true}
		case key == "--quiet config:export --format json web" && held != nil && held.Load():
			return subprocess.ExecCommandResponse{Stdout: `{"LOG_LEVEL":"info"}`}, nil
		case strings.HasPrefix(key, "--quiet config:export --format json "):
			return subprocess.ExecCommandResponse{Stdout: `{}`}, nil
		case strings.HasPrefix(key, "--quiet config:unset ") && held != nil:
			held.Store(false)
		}
		return subprocess.ExecCommandResponse{}, nil
	}
}

// TestWholeCollectionExistsWhenEmpty pins #639: a task in a state that owns
// its whole collection reports it as existing whenever the read succeeds, in
// sync or on drift, with the empty collection as observed, while the additive
// states keep reporting whether the members they name are there.
func TestWholeCollectionExistsWhenEmpty(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), emptyCollectionRunner(nil))

	cases := []struct {
		name   string
		task   ConfigTask
		inSync bool
		want   ConfigTask
		exists bool
	}{
		{"clear in sync", ConfigTask{App: "web", State: StateClear}, true, ConfigTask{App: "web", State: StateClear}, true},
		{"empty set in sync", ConfigTask{App: "web", Config: map[string]string{}, State: StateSet}, true, ConfigTask{App: "web", State: StateClear}, true},
		{"set on drift", ConfigTask{App: "web", Config: map[string]string{"A": "1"}, State: StateSet}, false, ConfigTask{App: "web", State: StateClear}, true},
		{"present on drift", ConfigTask{App: "web", Config: map[string]string{"A": "1"}, State: StatePresent}, false, ConfigTask{App: "web", State: StateAbsent}, false},
		{"absent in sync", ConfigTask{App: "web", Config: map[string]string{"A": ""}, State: StateAbsent}, true, ConfigTask{App: "web", State: StateAbsent}, false},
	}
	for _, c := range cases {
		plan := c.task.Plan(ctx)
		if plan.Error != nil {
			t.Fatalf("%s: plan error: %v", c.name, plan.Error)
		}
		if plan.InSync != c.inSync {
			t.Errorf("%s: InSync = %v, want %v", c.name, plan.InSync, c.inSync)
		}
		if plan.Exists != c.exists {
			t.Errorf("%s: PlanResult.Exists = %v, want %v", c.name, plan.Exists, c.exists)
		}
		got, exists, err := ReadTask(ctx, c.task)
		if err != nil {
			t.Fatalf("%s: ReadTask: %v", c.name, err)
		}
		if exists != c.exists {
			t.Errorf("%s: ReadTask exists = %v, want %v", c.name, exists, c.exists)
		}
		if !c.inSync && !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: ReadTask = %#v, want %#v", c.name, got, c.want)
		}
	}

	got, exists, err := ReadAddress(ctx, "dokku_config[app=web]")
	if err != nil {
		t.Fatalf("ReadAddress: %v", err)
	}
	if want := (ConfigTask{App: "web", State: StateClear}); !exists || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadAddress = %#v (exists %v), want %#v existing", got, exists, want)
	}
}

// TestEmptyWholeCollectionOfAMissingAppIsObjectMissing pins that a whole
// collection read empty still checks its app, so a missing app is an
// *ObjectMissingError rather than an empty collection that exists.
func TestEmptyWholeCollectionOfAMissingAppIsObjectMissing(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), emptyCollectionRunner(nil))

	reads := map[string]func() (Task, bool, error){
		"ReadTask clear": func() (Task, bool, error) {
			return ReadTask(ctx, ConfigTask{App: "gone", State: StateClear})
		},
		"ReadTask set": func() (Task, bool, error) {
			return ReadTask(ctx, ConfigTask{App: "gone", Config: map[string]string{"A": "1"}, State: StateSet})
		},
		"ReadAddress": func() (Task, bool, error) {
			return ReadAddress(ctx, "dokku_config[app=gone]")
		},
	}
	for name, read := range reads {
		_, exists, err := read()
		var missing *ObjectMissingError
		if !errors.As(err, &missing) || exists {
			t.Errorf("%s: exists %v, err %v; want an *ObjectMissingError", name, exists, err)
		}
	}
}

// TestServiceExposeExistsByState pins how the rule applies to
// dokku_service_expose, which owns its whole port list in every state:
// present and absent model the exposure, which an unexposed service does not
// have, while set and clear model the port list, which is there empty.
func TestServiceExposeExistsByState(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{Stdout: "-"}, nil
	})
	for _, c := range []struct {
		task   ServiceExposeTask
		exists bool
	}{
		{ServiceExposeTask{Service: "redis", Name: "cache", Ports: []string{"6379"}, State: StatePresent}, false},
		{ServiceExposeTask{Service: "redis", Name: "cache", State: StateAbsent}, false},
		{ServiceExposeTask{Service: "redis", Name: "cache", Ports: []string{"6379"}, State: StateSet}, true},
		{ServiceExposeTask{Service: "redis", Name: "cache", State: StateClear}, true},
	} {
		plan := c.task.Plan(ctx)
		if plan.Error != nil {
			t.Fatalf("%s: plan error: %v", c.task.State, plan.Error)
		}
		if plan.Observed == nil || plan.Exists != c.exists {
			t.Errorf("%s: observed %#v, exists %v; want exists %v", c.task.State, plan.Observed, plan.Exists, c.exists)
		}
	}
}

// TestClearApplyObservesAnExistingCollection pins TaskOutputState.Exists
// after an apply: a clear that empties the collection reports it as there,
// empty, the same as a read of it would.
func TestClearApplyObservesAnExistingCollection(t *testing.T) {
	t.Parallel()
	var held atomic.Bool
	held.Store(true)
	ctx := subprocess.ContextWithRunner(testCtx(), emptyCollectionRunner(&held))

	out := ConfigTask{App: "web", State: StateClear}.Execute(ctx)
	if out.Error != nil {
		t.Fatalf("execute: %v", out.Error)
	}
	if !out.Changed {
		t.Fatal("the clear did not apply")
	}
	if want := (ConfigTask{App: "web", State: StateClear}); !reflect.DeepEqual(out.Observed, want) || !out.Exists {
		t.Errorf("observed = %#v (exists %v), want %#v existing", out.Observed, out.Exists, want)
	}
}

// TestOwnsWholeCollection pins which task values the rule covers: a
// collection task in set or clear, and nothing else.
func TestOwnsWholeCollection(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		task Task
		want bool
	}{
		{ConfigTask{State: StateSet}, true},
		{&ConfigTask{State: StateClear}, true},
		{ConfigTask{State: StatePresent}, false},
		{ConfigTask{State: StateAbsent}, false},
		{PsScaleTask{State: StateClear}, true},
		{AppTask{State: StateAbsent}, false},
		{AppTask{State: StatePresent}, false},
	} {
		if got := ownsWholeCollection(c.task); got != c.want {
			t.Errorf("ownsWholeCollection(%#v) = %v, want %v", c.task, got, c.want)
		}
	}
	if emptyWholeCollection(nil) || emptyWholeCollection(ConfigTask{Config: map[string]string{"A": "1"}, State: StateSet}) {
		t.Error("emptyWholeCollection reports a nil task or a collection with members as empty")
	}
	if !emptyWholeCollection(ConfigTask{State: StateClear}) {
		t.Error("emptyWholeCollection misses a clear collection")
	}
}
