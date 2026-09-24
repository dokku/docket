package tasks

import (
	"context"
	"encoding/json"
	"io"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestConfigTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := ConfigTask{
		App:    "test-app",
		Config: map[string]string{"KEY": "VALUE"},
		State:  "invalid",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGetTasksConfigTaskParsedCorrectly(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: set config
      dokku_config:
        app: test-app
        restart: false
        config:
          KEY1: val1
          KEY2: val2
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set config")
	if task == nil {
		t.Fatal("task 'set config' not found")
	}

	configTask, ok := task.(*ConfigTask)
	if !ok {
		// tasks may be stored as value types depending on reflection
		ct, ok2 := task.(ConfigTask)
		if !ok2 {
			t.Fatalf("task is not a ConfigTask (type is %T)", task)
		}
		configTask = &ct
	}

	if configTask.App != "test-app" {
		t.Errorf("App = %q, want %q", configTask.App, "test-app")
	}
	// Restart is a *bool, so an explicit restart: false survives decoding: it is
	// no longer clobbered back to true by defaults.SetDefaults (go-defaults
	// leaves pointer fields untouched).
	if configTask.Restart == nil {
		t.Fatal("Restart is nil, want an explicit false")
	}
	if *configTask.Restart {
		t.Error("Restart = true, want false (explicit restart: false must be preserved)")
	}
	if len(configTask.Config) != 2 {
		t.Fatalf("expected 2 config keys, got %d", len(configTask.Config))
	}
	if configTask.Config["KEY1"] != "val1" {
		t.Errorf("Config[KEY1] = %q, want %q", configTask.Config["KEY1"], "val1")
	}
	if configTask.Config["KEY2"] != "val2" {
		t.Errorf("Config[KEY2] = %q, want %q", configTask.Config["KEY2"], "val2")
	}
}

// TestConfigTaskRestartFlag pins the --no-restart flag to the Restart pointer
// across both config:set and config:unset. An explicit restart: false must emit
// --no-restart; an omitted restart (nil) defaults to true and must not.
func TestConfigTaskRestartFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name         string
		restart      *bool
		state        State
		current      string // stdout of the config:export probe
		wantVerb     string
		wantNoRestrt bool
	}{
		{"set explicit false emits --no-restart", boolPtr(false), StatePresent, "{}", "config:set", true},
		{"set omitted defaults to restart", nil, StatePresent, "{}", "config:set", false},
		{"set explicit true keeps restart", boolPtr(true), StatePresent, "{}", "config:set", false},
		{"unset explicit false emits --no-restart", boolPtr(false), StateAbsent, `{"KEY":"val"}`, "config:unset", true},
		{"unset omitted defaults to restart", nil, StateAbsent, `{"KEY":"val"}`, "config:unset", false},
		{"unset explicit true keeps restart", boolPtr(true), StateAbsent, `{"KEY":"val"}`, "config:unset", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
				"--quiet config:export --format json test-app": tc.current,
			}))

			plan := ConfigTask{
				App:     "test-app",
				Restart: tc.restart,
				Config:  map[string]string{"KEY": "val"},
				State:   tc.state,
			}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if len(plan.Commands) == 0 {
				t.Fatalf("expected a command, got none (plan=%#v)", plan)
			}
			cmd := plan.Commands[0]
			if !strings.Contains(cmd, tc.wantVerb) {
				t.Errorf("command %q does not contain %q", cmd, tc.wantVerb)
			}
			if got := strings.Contains(cmd, "--no-restart"); got != tc.wantNoRestrt {
				t.Errorf("command %q: --no-restart present = %v, want %v", cmd, got, tc.wantNoRestrt)
			}
		})
	}
}

// TestConfigTaskRestartFalseEndToEnd exercises the full parse + SetDefaults
// pipeline: a recipe with restart: false must still yield a --no-restart
// command. This is the regression the go-defaults clobber produced.
func TestConfigTaskRestartFalseEndToEnd(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: set config
      dokku_config:
        app: test-app
        restart: false
        config:
          KEY: val
`)
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet config:export --format json test-app": "{}",
	}))

	parsed, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	task := parsed.Get("set config")
	if task == nil {
		t.Fatal("task 'set config' not found")
	}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if len(plan.Commands) == 0 {
		t.Fatalf("expected a command, got none")
	}
	if !strings.Contains(plan.Commands[0], "--no-restart") {
		t.Errorf("restart: false should emit --no-restart, got %q", plan.Commands[0])
	}
}

// configLinkFixture answers the probes behind configKeptKeys for app "web": one
// linked postgres service and the given git rev-env-var name.
func configLinkFixture(current, revVar string) map[string]string {
	return map[string]string{
		"--quiet config:export --format json web": current,
		"git:report web --git-rev-env-var":        revVar,
		"--quiet plugin:trigger service-list":     "postgres:my-db",
		"--quiet postgres:links my-db":            "web",
		"--quiet postgres:info my-db --dsn":       "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db",
	}
}

// stdinRecorder answers from responses like fakeDokku and keeps the stdin of
// every call that carried one, keyed by the joined args.
func stdinRecorder(responses map[string]string, stdins map[string]string) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	inner := fakeDokku(responses)
	return func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		if in.Stdin != nil {
			b, _ := io.ReadAll(in.Stdin)
			stdins[strings.Join(in.Args, " ")] = string(b)
		}
		return inner(ctx, in)
	}
}

func TestConfigTaskSetReplacesWholeConfig(t *testing.T) {
	t.Parallel()
	current := `{"KEEP":"same","CHANGE":"old","STALE":"gone",` +
		`"DATABASE_URL":"postgres://postgres:pw@dokku-postgres-my-db:5432/my_db",` +
		`"ALT_URL":"postgresql://postgres:pw@dokku-postgres-my-db:5432/my_db?sslmode=disable",` +
		`"NO_VHOST":"1","COMMIT_SHA":"abc123","GIT_REV":"not-the-rev-var","APP_SECRET":"generated"}`
	stdins := map[string]string{}
	ctx := subprocess.ContextWithRunner(testCtx(), stdinRecorder(configLinkFixture(current, "COMMIT_SHA"), stdins))

	task := ConfigTask{
		App:      "web",
		Config:   map[string]string{"KEEP": "same", "CHANGE": "new", "ADDED": "yes"},
		Preserve: []string{"APP_SECRET"},
		State:    StateSet,
	}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {in sync %v, status %q}, want drift with status %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	// GIT_REV is an ordinary key here: the app's rev var is COMMIT_SHA.
	wantMutations := []string{"set ADDED (new)", "set CHANGE (was set)", "unset GIT_REV", "unset STALE"}
	if !reflect.DeepEqual(plan.Mutations, wantMutations) {
		t.Errorf("mutations = %q, want %q", plan.Mutations, wantMutations)
	}
	wantCmd := "dokku --quiet config:import --replace --format json web -"
	if !reflect.DeepEqual(plan.Commands, []string{wantCmd}) {
		t.Fatalf("commands = %q, want [%q]", plan.Commands, wantCmd)
	}

	result := ExecutePlan(ctx, plan)
	if result.Error != nil {
		t.Fatalf("unexpected apply error: %v", result.Error)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(stdins["--quiet config:import --replace --format json web -"]), &payload); err != nil {
		t.Fatalf("config:import stdin is not a JSON object: %v", err)
	}
	wantPayload := map[string]string{
		"KEEP":         "same",
		"CHANGE":       "new",
		"ADDED":        "yes",
		"DATABASE_URL": "postgres://postgres:pw@dokku-postgres-my-db:5432/my_db",
		"ALT_URL":      "postgresql://postgres:pw@dokku-postgres-my-db:5432/my_db?sslmode=disable",
		"NO_VHOST":     "1",
		"COMMIT_SHA":   "abc123",
		"APP_SECRET":   "generated",
	}
	if !reflect.DeepEqual(payload, wantPayload) {
		t.Errorf("config:import payload = %v, want %v", payload, wantPayload)
	}
}

func TestConfigTaskSetKeepsValuesOffArgvAndMutations(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(configLinkFixture(`{"OLD":"old-secret"}`, "GIT_REV")))
	plan := ConfigTask{App: "web", Config: map[string]string{"TOKEN": "new-secret"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	for _, line := range append(append([]string{}, plan.Mutations...), plan.Commands...) {
		if strings.Contains(line, "secret") {
			t.Errorf("plan line %q carries a config value", line)
		}
	}
}

func TestConfigTaskSetInSync(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(configLinkFixture(
		`{"KEY":"val","NO_VHOST":"1","DATABASE_URL":"postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"}`, "GIT_REV")))
	plan := ConfigTask{App: "web", Config: map[string]string{"KEY": "val"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("plan = %+v, want in sync: only kept keys are undeclared", plan)
	}
}

func TestConfigTaskSetCreatesAgainstEmptyConfig(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(configLinkFixture(
		`{"NO_VHOST":"1"}`, "GIT_REV")))
	plan := ConfigTask{App: "web", Config: map[string]string{"KEY": "val"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("status = %q, want %q when the app holds no key the recipe owns", plan.Status, PlanStatusCreate)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"set KEY (new)"}) {
		t.Errorf("mutations = %q, want [set KEY (new)]", plan.Mutations)
	}
}

// TestConfigTaskSetDeclaredKeyOverridesKept pins that declaring a key the task
// would otherwise keep hands it to the recipe: the declared value is written.
func TestConfigTaskSetDeclaredKeyOverridesKept(t *testing.T) {
	t.Parallel()
	stdins := map[string]string{}
	ctx := subprocess.ContextWithRunner(testCtx(), stdinRecorder(configLinkFixture(
		`{"NO_VHOST":"1","DATABASE_URL":"postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"}`, "GIT_REV"), stdins))
	plan := ConfigTask{
		App:    "web",
		Config: map[string]string{"NO_VHOST": "0", "DATABASE_URL": "postgres://external/db"},
		State:  StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"set DATABASE_URL (was set)", "set NO_VHOST (was set)"}) {
		t.Errorf("mutations = %q", plan.Mutations)
	}
	if result := ExecutePlan(ctx, plan); result.Error != nil {
		t.Fatalf("unexpected apply error: %v", result.Error)
	}
	var payload map[string]string
	if err := json.Unmarshal([]byte(stdins["--quiet config:import --replace --format json web -"]), &payload); err != nil {
		t.Fatalf("config:import stdin is not a JSON object: %v", err)
	}
	if !reflect.DeepEqual(payload, map[string]string{"NO_VHOST": "0", "DATABASE_URL": "postgres://external/db"}) {
		t.Errorf("payload = %v, want the declared values", payload)
	}
}

// TestConfigTaskSetSkipsProbesWhenEveryKeyIsDeclared pins that the service and
// git probes only run when there is an undeclared key to classify.
func TestConfigTaskSetSkipsProbesWhenEveryKeyIsDeclared(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(configLinkFixture(`{"KEY":"old"}`, "GIT_REV"), &calls))
	plan := ConfigTask{App: "web", Config: map[string]string{"KEY": "new"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !reflect.DeepEqual(calls, []string{"--quiet config:export --format json web"}) {
		t.Errorf("calls = %q, want only the config:export probe", calls)
	}
}

func TestConfigTaskClearUnsetsOwnedKeys(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(configLinkFixture(
		`{"B":"2","A":"1","NO_VHOST":"1","GIT_REV":"abc","APP_SECRET":"s",`+
			`"DATABASE_URL":"postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"}`, "GIT_REV")))
	plan := ConfigTask{App: "web", Preserve: []string{"APP_SECRET"}, State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"unset A", "unset B"}) {
		t.Errorf("mutations = %q, want [unset A unset B]", plan.Mutations)
	}
	want := "dokku --quiet config:unset web A B"
	if !reflect.DeepEqual(plan.Commands, []string{want}) {
		t.Errorf("commands = %q, want [%q]", plan.Commands, want)
	}
}

func TestConfigTaskClearInSyncWithOnlyKeptKeys(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(configLinkFixture(
		`{"NO_VHOST":"1","DATABASE_URL":"postgres://postgres:pw@dokku-postgres-my-db:5432/my_db"}`, "GIT_REV")))
	plan := ConfigTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("plan = %+v, want in sync", plan)
	}
}

func TestConfigTaskSetAndClearRestartFlag(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    ConfigTask
		want    string
		current string
	}{
		{"set no restart", ConfigTask{App: "web", Restart: boolPtr(false), Config: map[string]string{"K": "v"}, State: StateSet}, "dokku --quiet config:import --replace --format json --no-restart web -", `{}`},
		{"set restart", ConfigTask{App: "web", Config: map[string]string{"K": "v"}, State: StateSet}, "dokku --quiet config:import --replace --format json web -", `{}`},
		{"clear no restart", ConfigTask{App: "web", Restart: boolPtr(false), State: StateClear}, "dokku --quiet config:unset --no-restart web K", `{"K":"v"}`},
		{"clear restart", ConfigTask{App: "web", State: StateClear}, "dokku --quiet config:unset web K", `{"K":"v"}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(configLinkFixture(tc.current, "GIT_REV")))
			plan := tc.task.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if !reflect.DeepEqual(plan.Commands, []string{tc.want}) {
				t.Errorf("commands = %q, want [%q]", plan.Commands, tc.want)
			}
		})
	}
}

func TestConfigTaskValidate(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    ConfigTask
		wantErr string
	}{
		{"present ok", ConfigTask{App: "web", Config: map[string]string{"K": "v"}}, ""},
		{"set ok", ConfigTask{App: "web", Config: map[string]string{"K": "v"}, Preserve: []string{"P"}, State: StateSet}, ""},
		{"clear ok", ConfigTask{App: "web", Preserve: []string{"P"}, State: StateClear}, ""},
		{"set empty", ConfigTask{App: "web", State: StateSet}, "'config' must not be empty for state 'set'"},
		{"clear with config", ConfigTask{App: "web", Config: map[string]string{"K": "v"}, State: StateClear}, "'config' must not be set for state 'clear'"},
		{"preserve under present", ConfigTask{App: "web", Config: map[string]string{"K": "v"}, Preserve: []string{"P"}}, "'preserve' is only valid for state 'set' or 'clear', got state 'present'"},
		{"preserve under absent", ConfigTask{App: "web", Config: map[string]string{"K": "v"}, Preserve: []string{"P"}, State: StateAbsent}, "'preserve' is only valid for state 'set' or 'clear', got state 'absent'"},
		{"preserve overlaps config", ConfigTask{App: "web", Config: map[string]string{"K": "v"}, Preserve: []string{"K"}, State: StateSet}, `key "K" must not be in both 'config' and 'preserve'`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.task.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("Validate() = %v, want nil", err)
				}
				return
			}
			if err == nil || err.Error() != tc.wantErr {
				t.Errorf("Validate() = %v, want %q", err, tc.wantErr)
			}
		})
	}
}
