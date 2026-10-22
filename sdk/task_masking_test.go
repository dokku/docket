package sdk_test

import (
	"context"
	"encoding/base64"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/sdk"
)

// The calls below register a task's own secrets with the context's masker, so
// none of these tests calls masker.Add or builds the masker from
// TaskSensitiveValues.

const maskingSecret = "s3cr3t-value"

// maskingConfig is a dokku_config declaring maskingSecret, and the config:set
// line its apply runs.
func maskingConfig(t *testing.T) (*sdk.ConfigTask, string) {
	t.Helper()
	task, err := sdk.NewTask("dokku_config")
	if err != nil {
		t.Fatal(err)
	}
	cfg := task.(*sdk.ConfigTask)
	cfg.App = "api"
	cfg.Config = map[string]string{"TOKEN": maskingSecret}
	encoded := base64.StdEncoding.EncodeToString([]byte(maskingSecret))
	return cfg, "--quiet config:set --encoded api TOKEN=" + encoded
}

// maskingContext is a session context with an empty masker and fake as its
// runner.
func maskingContext(t *testing.T, fake *recorder) (context.Context, *sdk.Masker) {
	t.Helper()
	masker := sdk.NewMasker()
	session := sdk.NewSession(masker)
	t.Cleanup(func() { _ = session.Close() })
	return sdk.ContextWithRunner(session.Context(context.Background(), sdk.Target{}), fake.run), masker
}

// assertMasked fails when masking got with masker leaves the secret readable
// or masks nothing.
func assertMasked(t *testing.T, masker *sdk.Masker, what, got string) {
	t.Helper()
	if !strings.Contains(got, maskingSecret) {
		t.Fatalf("%s = %q, want it to carry the secret before masking", what, got)
	}
	shown := masker.String(got)
	if strings.Contains(shown, maskingSecret) || !strings.Contains(shown, sdk.MaskPlaceholder) {
		t.Errorf("masked %s = %q, want the secret replaced by %q", what, shown, sdk.MaskPlaceholder)
	}
}

func TestPlanRegistersTheTasksSecrets(t *testing.T) {
	t.Parallel()
	cfg, _ := maskingConfig(t)
	ctx, _ := maskingContext(t, &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{Stdout: "{}"}, nil
		},
	}})

	plan := cfg.Plan(ctx)
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}
	if want := []string{"dokku --quiet config:set --encoded api TOKEN=" + sdk.MaskPlaceholder}; !reflect.DeepEqual(plan.Commands, want) {
		t.Errorf("Commands = %q, want %q", plan.Commands, want)
	}
}

func TestPlanErrorMasksTheTasksSecrets(t *testing.T) {
	t.Parallel()
	cfg, _ := maskingConfig(t)
	ctx, masker := maskingContext(t, &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": exited(1, "cannot read TOKEN="+maskingSecret),
	}})

	plan := cfg.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("the fake fails config:export, want an error")
	}
	assertMasked(t, masker, "plan error", plan.Error.Error())
}

func TestExecuteRegistersTheTasksSecrets(t *testing.T) {
	t.Parallel()
	cfg, setArgs := maskingConfig(t)
	ctx, masker := maskingContext(t, &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{Stdout: "{}"}, nil
		},
		setArgs: exited(1, "rejected TOKEN="+maskingSecret),
	}})

	state := cfg.Execute(ctx)
	if state.Error == nil {
		t.Fatal("the fake rejects config:set, want an error")
	}
	assertMasked(t, masker, "message", state.Message)
	assertMasked(t, masker, "error", state.Error.Error())
	assertMasked(t, masker, "stderr", state.Stderr)
}

func TestReadTaskRegistersTheTasksSecrets(t *testing.T) {
	t.Parallel()
	cfg, _ := maskingConfig(t)
	ctx, masker := maskingContext(t, &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": exited(1, "cannot read TOKEN="+maskingSecret),
	}})

	_, _, err := sdk.ReadTask(ctx, cfg)
	if err == nil {
		t.Fatal("the fake fails config:export, want an error")
	}
	assertMasked(t, masker, "read error", err.Error())
	var execErr *sdk.ExecError
	if !errors.As(err, &execErr) {
		t.Fatalf("error = %v, want an *sdk.ExecError", err)
	}
	assertMasked(t, masker, "read stderr", execErr.Response.Stderr)
}

// TestExecutePlanRegistersThePlannedTasksSecrets plans with no masker on the
// context and applies under a session whose masker has never seen the task,
// so only the plan itself can tell ExecutePlan what to mask.
func TestExecutePlanRegistersThePlannedTasksSecrets(t *testing.T) {
	t.Parallel()
	cfg, setArgs := maskingConfig(t)
	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{Stdout: "{}"}, nil
		},
		setArgs: exited(1, "rejected TOKEN="+maskingSecret),
	}}

	plan := cfg.Plan(sdk.ContextWithRunner(context.Background(), fake.run))
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}

	ctx, masker := maskingContext(t, fake)
	state := sdk.ExecutePlan(ctx, plan)
	if state.Error == nil {
		t.Fatal("the fake rejects config:set, want an error")
	}
	assertMasked(t, masker, "message", state.Message)
	assertMasked(t, masker, "stderr", state.Stderr)
}

// TestExecutePlanRegistersARefusedPlansSecrets applies a plan its task's own
// validation refused, which never probes, under a fresh masker.
func TestExecutePlanRegistersARefusedPlansSecrets(t *testing.T) {
	t.Parallel()
	task, err := sdk.NewTask("dokku_http_auth_user")
	if err != nil {
		t.Fatal(err)
	}
	user := task.(*sdk.HttpAuthUserTask)
	user.App = "api"
	// A password and a hash are mutually exclusive.
	user.Users = []sdk.HttpAuthUser{{Username: "admin", Password: maskingSecret, Hash: "admin:$apr1$x"}}

	plan := user.Plan(context.Background())
	if plan.Error == nil {
		t.Fatal("a user with a password and a hash plans an error, got none")
	}

	ctx, masker := maskingContext(t, &recorder{})
	sdk.ExecutePlan(ctx, plan)
	if shown := masker.String("password " + maskingSecret); strings.Contains(shown, maskingSecret) {
		t.Errorf("masked = %q, want the refused plan's secret registered", shown)
	}
}

func TestPlanWithoutAMaskerRegistersNothing(t *testing.T) {
	t.Parallel()
	cfg, _ := maskingConfig(t)
	fake := &recorder{answers: map[string]func() (sdk.ExecCommandResponse, error){
		"--quiet config:export --format json api": func() (sdk.ExecCommandResponse, error) {
			return sdk.ExecCommandResponse{Stdout: "{}"}, nil
		},
	}}

	plan := cfg.Plan(sdk.ContextWithRunner(context.Background(), fake.run))
	if plan.Status != sdk.PlanStatusCreate {
		t.Fatalf("Status = %q (error %v), want %q", plan.Status, plan.Error, sdk.PlanStatusCreate)
	}
	encoded := base64.StdEncoding.EncodeToString([]byte(maskingSecret))
	if want := []string{"dokku --quiet config:set --encoded api TOKEN=" + encoded}; !reflect.DeepEqual(plan.Commands, want) {
		t.Errorf("Commands = %q, want %q unmasked with no masker", plan.Commands, want)
	}
}
