package sdk_test

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"testing"

	"github.com/dokku/docket/sdk"
	"github.com/dokku/docket/sdk/dokkutest"
)

func newApp(t *testing.T, name string) *sdk.AppTask {
	t.Helper()
	task, err := sdk.NewTask("dokku_app")
	if err != nil {
		t.Fatal(err)
	}
	app := task.(*sdk.AppTask)
	app.App = name
	return app
}

func TestContextWithRunnerFakesDokku(t *testing.T) {
	t.Parallel()
	fake := dokkutest.New(t)
	// A response with a non-zero exit and no error is an answer too.
	fake.Handle("--quiet apps:exists api", func(context.Context, sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
		return sdk.ExecCommandResponse{ExitCode: 1}, nil
	})
	ctx := sdk.ContextWithRunner(context.Background(), fake.Run)

	plan := newApp(t, "api").Plan(ctx)
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}
	if want := []string{"dokku --quiet apps:create api"}; !reflect.DeepEqual(plan.Commands, want) {
		t.Errorf("Commands = %q, want %q", plan.Commands, want)
	}

	state := sdk.ExecutePlan(ctx, plan)
	if state.Error != nil || !state.Changed {
		t.Fatalf("state = %#v", state)
	}
	if want := []string{"--quiet apps:exists api", "--quiet apps:create api"}; !reflect.DeepEqual(fake.Lines(), want) {
		t.Errorf("calls = %q, want %q", fake.Lines(), want)
	}
}

func TestContextWithRunnerAbsentAsExecError(t *testing.T) {
	t.Parallel()
	// The fake holds no app api, so apps:exists exits non-zero with an
	// *ExecError, as the real runner reports it.
	fake := dokkutest.New(t)
	plan := newApp(t, "api").Plan(sdk.ContextWithRunner(context.Background(), fake.Run))
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}
}

func TestContextWithRunnerApplyFailure(t *testing.T) {
	t.Parallel()
	fake := dokkutest.New(t).Handle("--quiet apps:create api", dokkutest.Exit(1, "name is reserved"))
	ctx := sdk.ContextWithRunner(context.Background(), fake.Run)

	state := sdk.ExecutePlan(ctx, newApp(t, "api").Plan(ctx))
	var execErr *sdk.ExecError
	if !errors.As(state.Error, &execErr) {
		t.Fatalf("Error = %v (%T), want an *ExecError", state.Error, state.Error)
	}
	if execErr.Response.ExitCode != 1 || state.Message != "name is reserved" || state.Changed {
		t.Errorf("state = %#v", state)
	}
}

func TestContextWithRunnerExecutionFailure(t *testing.T) {
	t.Parallel()
	// An error that is not an *ExecError with Ran set is a command that never
	// ran, which a probe must not read as the app being absent.
	fake := dokkutest.New(t).Handle("--quiet apps:exists api", dokkutest.Unavailable(errors.New("dokku: command not found")))
	plan := newApp(t, "api").Plan(sdk.ContextWithRunner(context.Background(), fake.Run))
	if plan.Status != sdk.PlanStatusError || plan.Error == nil {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusError)
	}
}

func TestContextWithRunnerIsPerContext(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"api", "worker"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := dokkutest.New(t).SeedApp(name)
			ctx := sdk.ContextWithRunner(context.Background(), fake.Run)
			if plan := newApp(t, name).Plan(ctx); plan.Status != sdk.PlanStatusOK {
				t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusOK)
			}
			if want := []string{"--quiet apps:exists " + name}; !reflect.DeepEqual(fake.Lines(), want) {
				t.Errorf("calls = %q, want only this context's %q", fake.Lines(), want)
			}
		})
	}
}

func TestContextWithRunnerMasking(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-value"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	setArgs := "--quiet config:set --encoded api TOKEN=" + encoded

	fake := dokkutest.New(t).SeedApp("api").Handle(setArgs, dokkutest.Exit(1, "rejected TOKEN="+secret))

	task, err := sdk.NewTask("dokku_config")
	if err != nil {
		t.Fatal(err)
	}
	cfg := task.(*sdk.ConfigTask)
	cfg.App = "api"
	cfg.Config = map[string]string{"TOKEN": secret}

	// Planning registers the task's own secrets with the session's masker.
	masker := sdk.NewMasker()
	session := sdk.NewSession(masker)
	defer func() { _ = session.Close() }()
	ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), fake.Run)

	plan := cfg.Plan(ctx)
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}
	if want := []string{"dokku --quiet config:set --encoded api TOKEN=" + sdk.MaskPlaceholder}; !reflect.DeepEqual(plan.Commands, want) {
		t.Errorf("Commands = %q, want %q", plan.Commands, want)
	}

	state := sdk.ExecutePlan(ctx, plan)
	if state.Error == nil {
		t.Fatal("the fake rejects config:set, want an error")
	}
	// The fake is handed the command as it would have run.
	if calls := fake.Lines(); len(calls) != 2 || calls[1] != setArgs {
		t.Errorf("calls = %q, want the unmasked %q last", calls, setArgs)
	}
	// Diagnostics come back as dokku wrote them; the caller masks what it shows.
	if got, want := masker.String(state.Message), "rejected TOKEN="+sdk.MaskPlaceholder; got != want {
		t.Errorf("masked message = %q, want %q", got, want)
	}
}
