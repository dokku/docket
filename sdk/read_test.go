package sdk_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/dokku/docket/sdk"
)

func TestReadTaskReadsTheDeclaredKeys(t *testing.T) {
	fake := newFakeDokku("0.99.0")
	ctx := sdk.ContextWithVersionGate(fake.context())

	task, _ := sdk.NewTask("dokku_config")
	cfg := task.(*sdk.ConfigTask)
	cfg.App = "web"
	cfg.Config = map[string]string{"KEY": "desired", "MISSING": "x"}

	got, exists, err := sdk.ReadTask(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	if !exists {
		t.Error("exists = false, want true")
	}
	observed, ok := got.(sdk.ConfigTask)
	if !ok {
		t.Fatalf("ReadTask returned %T, want sdk.ConfigTask", got)
	}
	want := sdk.ConfigTask{App: "web", Config: map[string]string{"KEY": "value"}, State: sdk.StatePresent}
	if !reflect.DeepEqual(observed, want) {
		t.Errorf("observed = %#v, want %#v", observed, want)
	}

	plan := cfg.Plan(ctx)
	if !reflect.DeepEqual(plan.Observed, got) || plan.Exists != exists {
		t.Errorf("Plan observed %#v (exists %v), ReadTask %#v (exists %v)", plan.Observed, plan.Exists, got, exists)
	}
}

func TestReadTaskRefusesOldDokku(t *testing.T) {
	fake := newFakeDokku("0.38.8")
	ctx := sdk.ContextWithVersionGate(fake.context())
	task, _ := sdk.NewTask("dokku_app")
	task.(*sdk.AppTask).App = "web"

	got, exists, err := sdk.ReadTask(ctx, task)
	var verr *sdk.VersionError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v (%T), want a *VersionError", err, err)
	}
	if verr.TaskType != "dokku_app" {
		t.Errorf("TaskType = %q, want dokku_app", verr.TaskType)
	}
	if got != nil || exists {
		t.Errorf("a refused read returned %#v, %v", got, exists)
	}
	if fake.count("--quiet apps:exists web") != 0 {
		t.Error("a refused read still probed the app")
	}

	plan := task.Plan(ctx)
	if !errors.As(plan.Error, &verr) || plan.Status != sdk.PlanStatusError {
		t.Errorf("Plan = %#v, want the *VersionError", plan)
	}
}

func TestReadTaskWithoutGateReadsOldDokku(t *testing.T) {
	fake := newFakeDokku("0.38.8")
	task, _ := sdk.NewTask("dokku_app")
	task.(*sdk.AppTask).App = "web"

	got, exists, err := sdk.ReadTask(fake.context(), task)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || got != (sdk.AppTask{App: "web", State: sdk.StatePresent}) {
		t.Errorf("got %#v, %v", got, exists)
	}
	if fake.count("version") != 0 {
		t.Error("a context with no gate should not read the version")
	}
}

func TestReadTaskUnreadable(t *testing.T) {
	fake := newFakeDokku("0.99.0")
	task, _ := sdk.NewTask("dokku_storage_ensure")
	task.(*sdk.StorageEnsureTask).App = "web"

	if _, _, err := sdk.ReadTask(fake.context(), task); !errors.Is(err, sdk.ErrStateUnreadable) {
		t.Errorf("error = %v, want sdk.ErrStateUnreadable", err)
	}
}
