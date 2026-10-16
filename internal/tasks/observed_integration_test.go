package tasks

import (
	"errors"
	"reflect"
	"testing"
)

// TestIntegrationReadTaskApp reads an app back from a live server before and
// after it is created, and checks the gate refuses nothing on the release the
// suite installs.
func TestIntegrationReadTaskApp(t *testing.T) {
	skipIfNoDokkuT(t)

	app := "docket-test-read-app"
	destroyApp(testCtx(), app)
	defer destroyApp(testCtx(), app)
	ctx := ContextWithVersionGate(testCtx())

	got, exists, err := ReadTask(ctx, AppTask{App: app, State: StatePresent})
	if err != nil {
		t.Fatalf("read a missing app: %v", err)
	}
	if exists || got != (AppTask{App: app, State: StateAbsent}) {
		t.Errorf("missing app read as %#v, exists %v", got, exists)
	}

	createApp(testCtx(), app)
	got, exists, err = ReadTask(ctx, AppTask{App: app, State: StatePresent})
	if err != nil {
		t.Fatalf("read the app: %v", err)
	}
	if !exists || got != (AppTask{App: app, State: StatePresent}) {
		t.Errorf("app read as %#v, exists %v", got, exists)
	}
}

// TestIntegrationReadTaskConfig reads an app's config back from a live server:
// the declared keys for state present, every owned key for state set.
func TestIntegrationReadTaskConfig(t *testing.T) {
	skipIfNoDokkuT(t)

	app := "docket-test-read-config"
	destroyApp(testCtx(), app)
	createApp(testCtx(), app)
	defer destroyApp(testCtx(), app)
	ctx := ContextWithVersionGate(testCtx())

	set := ConfigTask{App: app, Restart: boolPtr(false), Config: map[string]string{"READ_ONE": "one", "READ_TWO": "two"}, State: StatePresent}
	if r := set.Execute(ctx); r.Error != nil {
		t.Fatalf("set config: %v", r.Error)
	}

	got, exists, err := ReadTask(ctx, ConfigTask{App: app, Config: map[string]string{"READ_ONE": "changed", "READ_MISSING": "x"}, State: StatePresent})
	if err != nil {
		t.Fatalf("read declared keys: %v", err)
	}
	want := ConfigTask{App: app, Config: map[string]string{"READ_ONE": "one"}, State: StatePresent}
	if !exists || !reflect.DeepEqual(got, want) {
		t.Errorf("declared keys read as %#v, exists %v; want %#v", got, exists, want)
	}

	plan := ConfigTask{App: app, Config: map[string]string{"READ_ONE": "one"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("plan state set: %v", plan.Error)
	}
	owned, ok := plan.Observed.(ConfigTask)
	if !ok || owned.State != StateSet || owned.Config["READ_ONE"] != "one" || owned.Config["READ_TWO"] != "two" {
		t.Errorf("state set observed %#v, want both owned keys", plan.Observed)
	}

	if _, _, err := ReadTask(ctx, StorageEnsureTask{App: app, State: StatePresent}); !errors.Is(err, ErrStateUnreadable) {
		t.Errorf("storage ensure read error = %v, want ErrStateUnreadable", err)
	}
}
