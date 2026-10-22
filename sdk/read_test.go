package sdk_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

func TestReadTaskReadsTheDeclaredKeys(t *testing.T) {
	fake := newFakeDokku(t, "0.99.0")
	ctx := sdk.ContextWithVersionGate(fake.Context(context.Background()))

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

// TestReadTaskReturnsTheDeclaredTaskInSync pins #637: a formation that plans
// in sync reads back as declared, including a process type at zero that the
// server's report leaves out, so a consumer diffing the read against the task
// sees no change. ReadAddress, with no declared task, reads what the server
// reports.
func TestReadTaskReturnsTheDeclaredTaskInSync(t *testing.T) {
	ctx := dokkutest.New(t).SeedScale("web", map[string]int{"web": 2, "worker": 0}).Context(context.Background())

	task := sdk.PsScaleTask{App: "web", Scale: map[string]int{"web": 2, "worker": 0}, State: sdk.StateSet}
	plan := task.Plan(ctx)
	if !plan.InSync {
		t.Fatalf("plan reports drift: %s", plan.Reason)
	}
	got, exists, err := sdk.ReadTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, task) || !exists {
		t.Errorf("ReadTask = %#v (exists %v), want the declared task", got, exists)
	}
	if !reflect.DeepEqual(plan.Observed, got) || plan.Exists != exists {
		t.Errorf("Plan observed %#v (exists %v), ReadTask %#v (exists %v)", plan.Observed, plan.Exists, got, exists)
	}

	got, _, err = sdk.ReadAddress(ctx, "dokku_ps_scale[app=web]")
	want := sdk.PsScaleTask{App: "web", Scale: map[string]int{"web": 2}, State: sdk.StateSet}
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("ReadAddress = %#v, %v; want %#v", got, err, want)
	}
}

func TestReadAddressReadsTheWholeResource(t *testing.T) {
	fake := newFakeDokku(t, "0.99.0")
	ctx := sdk.NewSession(nil, sdk.WithVersionGate()).Context(fake.Context(context.Background()), sdk.Target{})

	got, exists, err := sdk.ReadAddress(ctx, "dokku_app[app=web]")
	if err != nil {
		t.Fatal(err)
	}
	if want := (sdk.AppTask{App: "web", State: sdk.StatePresent}); !exists || got != want {
		t.Errorf("ReadAddress = %#v (exists %v), want %#v", got, exists, want)
	}
}

func TestReadAddressRefusesOldDokku(t *testing.T) {
	fake := newFakeDokku(t, "0.38.8")
	ctx := sdk.NewSession(nil, sdk.WithVersionGate()).Context(fake.Context(context.Background()), sdk.Target{})

	_, _, err := sdk.ReadAddress(ctx, "dokku_app[app=web]")
	var verr *sdk.VersionError
	if !errors.As(err, &verr) {
		t.Fatalf("error = %v, want a *VersionError", err)
	}
	if fake.Count("--quiet apps:exists web") != 0 {
		t.Error("a refused read should not probe the app")
	}
}

func TestReadTaskRefusesOldDokku(t *testing.T) {
	fake := newFakeDokku(t, "0.38.8")
	ctx := sdk.ContextWithVersionGate(fake.Context(context.Background()))
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
	if fake.Count("--quiet apps:exists web") != 0 {
		t.Error("a refused read still probed the app")
	}

	plan := task.Plan(ctx)
	if !errors.As(plan.Error, &verr) || plan.Status != sdk.PlanStatusError {
		t.Errorf("Plan = %#v, want the *VersionError", plan)
	}
}

func TestReadTaskWithoutGateReadsOldDokku(t *testing.T) {
	fake := newFakeDokku(t, "0.38.8")
	task, _ := sdk.NewTask("dokku_app")
	task.(*sdk.AppTask).App = "web"

	got, exists, err := sdk.ReadTask(fake.Context(context.Background()), task)
	if err != nil {
		t.Fatal(err)
	}
	if !exists || got != (sdk.AppTask{App: "web", State: sdk.StatePresent}) {
		t.Errorf("got %#v, %v", got, exists)
	}
	if fake.Count("version") != 0 {
		t.Error("a context with no gate should not read the version")
	}
}

func TestReadTaskUnreadable(t *testing.T) {
	fake := newFakeDokku(t, "0.99.0")
	task, _ := sdk.NewTask("dokku_storage_ensure")
	task.(*sdk.StorageEnsureTask).App = "web"

	if _, _, err := sdk.ReadTask(fake.Context(context.Background()), task); !errors.Is(err, sdk.ErrStateUnreadable) {
		t.Errorf("error = %v, want sdk.ErrStateUnreadable", err)
	}
}

func TestReadAddressReportsAMissingApp(t *testing.T) {
	// The fake holds no app gone, so every command naming it fails the way
	// dokku's do.
	ctx := dokkutest.New(t).Context(context.Background())

	_, exists, err := sdk.ReadAddress(ctx, "dokku_config[app=gone]")
	if !errors.Is(err, sdk.ErrObjectMissing) {
		t.Fatalf("error = %v, want sdk.ErrObjectMissing", err)
	}
	if exists {
		t.Error("exists = true, want false")
	}
	var missing *sdk.ObjectMissingError
	if !errors.As(err, &missing) {
		t.Fatalf("error = %T, want *sdk.ObjectMissingError", err)
	}
	if missing.Object.Kind != sdk.ObjectApp || !reflect.DeepEqual(missing.Object.Keys, []string{"app"}) || !reflect.DeepEqual(missing.Object.Values, []string{"gone"}) {
		t.Errorf("Object = %#v, want app gone", missing.Object)
	}

	if _, exists, err := sdk.ReadAddress(ctx, "dokku_app[app=gone]"); err != nil || exists {
		t.Errorf("dokku_app: exists = %v, error = %v; want an absent app", exists, err)
	}
}
