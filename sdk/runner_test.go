package sdk_test

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/dokku/docket/sdk"
)

// recorder is a fake dokku that answers from a table keyed by the joined
// arguments, answers everything else with an empty success, and records what
// it was asked in order.
type recorder struct {
	answers map[string]func() (sdk.ExecCommandResponse, error)
	mu      sync.Mutex
	calls   []string
}

func (r *recorder) run(_ context.Context, in sdk.ExecCommandInput) (sdk.ExecCommandResponse, error) {
	key := strings.Join(in.Args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, key)
	r.mu.Unlock()
	if answer, ok := r.answers[key]; ok {
		return answer()
	}
	return sdk.ExecCommandResponse{}, nil
}

func (r *recorder) recorded() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.calls...)
}

// exited answers with a command that ran and exited with code, the way the
// real runner reports it.
func exited(code int, stderr string) func() (sdk.ExecCommandResponse, error) {
	return func() (sdk.ExecCommandResponse, error) {
		response := sdk.ExecCommandResponse{Stderr: stderr, ExitCode: code}
		return response, &sdk.ExecError{Response: response, Err: errors.New(stderr), Ran: true}
	}
}

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
	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		// A response with a non-zero exit and no error is an answer too.
		"--quiet apps:exists api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{ExitCode: 1}, nil
		},
	}}
	ctx := sdk.ContextWithRunner(context.Background(), fake.run)

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
	if want := []string{"--quiet apps:exists api", "--quiet apps:create api"}; !reflect.DeepEqual(fake.recorded(), want) {
		t.Errorf("calls = %q, want %q", fake.recorded(), want)
	}
}

func TestContextWithRunnerAbsentAsExecError(t *testing.T) {
	t.Parallel()
	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet apps:exists api": exited(1, ""),
	}}
	plan := newApp(t, "api").Plan(sdk.ContextWithRunner(context.Background(), fake.run))
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}
}

func TestContextWithRunnerApplyFailure(t *testing.T) {
	t.Parallel()
	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet apps:exists api": exited(1, ""),
		"--quiet apps:create api": exited(1, "name is reserved"),
	}}
	ctx := sdk.ContextWithRunner(context.Background(), fake.run)

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
	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet apps:exists api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{}, errors.New("dokku: command not found")
		},
	}}
	plan := newApp(t, "api").Plan(sdk.ContextWithRunner(context.Background(), fake.run))
	if plan.Status != sdk.PlanStatusError || plan.Error == nil {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusError)
	}
}

func TestContextWithRunnerIsPerContext(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"api", "worker"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			fake := &recorder{}
			ctx := sdk.ContextWithRunner(context.Background(), fake.run)
			if plan := newApp(t, name).Plan(ctx); plan.Status != sdk.PlanStatusOK {
				t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusOK)
			}
			if want := []string{"--quiet apps:exists " + name}; !reflect.DeepEqual(fake.recorded(), want) {
				t.Errorf("calls = %q, want only this context's %q", fake.recorded(), want)
			}
		})
	}
}

func TestContextWithRunnerMasking(t *testing.T) {
	t.Parallel()
	const secret = "s3cr3t-value"
	encoded := base64.StdEncoding.EncodeToString([]byte(secret))
	setArgs := "--quiet config:set --encoded api TOKEN=" + encoded

	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{Stdout: "{}"}, nil
		},
		setArgs: exited(1, "rejected TOKEN="+secret),
	}}

	task, err := sdk.NewTask("dokku_config")
	if err != nil {
		t.Fatal(err)
	}
	cfg := task.(*sdk.ConfigTask)
	cfg.App = "api"
	cfg.Config = map[string]string{"TOKEN": secret}

	masker := sdk.NewMasker(cfg.SensitiveValues()...)
	session := sdk.NewSession(masker)
	defer func() { _ = session.Close() }()
	ctx := sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), fake.run)

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
	if calls := fake.recorded(); len(calls) != 2 || calls[1] != setArgs {
		t.Errorf("calls = %q, want the unmasked %q last", calls, setArgs)
	}
	// Diagnostics come back as dokku wrote them; the caller masks what it shows.
	if got, want := masker.String(state.Message), "rejected TOKEN="+sdk.MaskPlaceholder; got != want {
		t.Errorf("masked message = %q, want %q", got, want)
	}
}
