package tasks

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	_ "github.com/gliderlabs/sigil/builtin"
)

// serviceMissing stubs every dokku call as a clean non-zero exit, which is how
// subprocess.Probe reports `<service>:exists` saying "absent". The shared
// fakeDokku always reports exit 0, so it cannot express a missing service and
// any test that needs Plan() to build a create command needs this instead.
func serviceMissing() func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	return func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{ExitCode: 1}, nil
	}
}

// planCreateCommand returns the single command Plan() would run to create the
// task's service, failing the test if the plan did not reach a create.
func planCreateCommand(t *testing.T, ctx context.Context, task ServiceCreateTask) string {
	t.Helper()
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected 1 command, got %d (plan=%#v)", len(plan.Commands), plan)
	}
	return plan.Commands[0]
}

func TestServiceCreateTaskInvalidState(t *testing.T) {
	task := ServiceCreateTask{Service: "redis", Name: "test-service", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGetTasksServiceCreateTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create redis service
      dokku_service_create:
        service: redis
        name: my-redis
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("create redis service")
	if task == nil {
		t.Fatal("task 'create redis service' not found")
	}

	scTask, ok := task.(*ServiceCreateTask)
	if !ok {
		st, ok2 := task.(ServiceCreateTask)
		if !ok2 {
			t.Fatalf("task is not a ServiceCreateTask (type is %T)", task)
		}
		scTask = &st
	}

	if scTask.Service != "redis" {
		t.Errorf("Service = %q, want %q", scTask.Service, "redis")
	}
	if scTask.Name != "my-redis" {
		t.Errorf("Name = %q, want %q", scTask.Name, "my-redis")
	}
	if scTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", scTask.State)
	}
}

func TestGetTasksServiceCreateWithTemplateContext(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create {{ .service_type }} service
      dokku_service_create:
        service: {{ .service_type }}
        name: {{ .service_name }}
`)
	context := map[string]interface{}{
		"service_type": "postgres",
		"service_name": "my-db",
	}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("create postgres service")
	if task == nil {
		t.Fatal("task 'create postgres service' not found")
	}

	scTask, ok := task.(*ServiceCreateTask)
	if !ok {
		st, ok2 := task.(ServiceCreateTask)
		if !ok2 {
			t.Fatalf("task is not a ServiceCreateTask (type is %T)", task)
		}
		scTask = &st
	}

	if scTask.Service != "postgres" {
		t.Errorf("Service = %q, want %q", scTask.Service, "postgres")
	}
	if scTask.Name != "my-db" {
		t.Errorf("Name = %q, want %q", scTask.Name, "my-db")
	}
}

// TestServiceCreateTaskCreateFlags pins each create-time option to the
// `<service>:create` flag it renders as. These are the flags every datastore
// plugin accepts (verified identical in dokku-postgres and dokku-redis), and
// they are what lets a recipe pin an image without depending on environment
// variables that would not survive the trip to a remote shell.
func TestServiceCreateTaskCreateFlags(t *testing.T) {
	cases := []struct {
		name string
		task ServiceCreateTask
		want string
	}{
		{
			name: "no options renders a bare create",
			task: ServiceCreateTask{Service: "redis", Name: "cache"},
			want: "dokku --quiet redis:create cache",
		},
		{
			name: "image and version",
			task: ServiceCreateTask{Service: "postgres", Name: "db", Image: "postgis/postgis", ImageVersion: "13-master"},
			want: "dokku --quiet postgres:create db --image postgis/postgis --image-version 13-master",
		},
		{
			name: "image alone",
			task: ServiceCreateTask{Service: "postgres", Name: "db", Image: "postgis/postgis"},
			want: "dokku --quiet postgres:create db --image postgis/postgis",
		},
		{
			name: "version alone pins the tag on the default image",
			task: ServiceCreateTask{Service: "postgres", Name: "db", ImageVersion: "13"},
			want: "dokku --quiet postgres:create db --image-version 13",
		},
		{
			name: "custom env is sorted and semicolon joined",
			task: ServiceCreateTask{Service: "postgres", Name: "db", CustomEnv: map[string]string{"USER": "alpha", "HOST": "beta"}},
			want: "dokku --quiet postgres:create db --custom-env HOST=beta;USER=alpha",
		},
		{
			name: "networks are comma joined",
			task: ServiceCreateTask{
				Service:           "redis",
				Name:              "cache",
				InitialNetwork:    "initial",
				PostCreateNetwork: []string{"created-a", "created-b"},
				PostStartNetwork:  []string{"started"},
			},
			want: "dokku --quiet redis:create cache --initial-network initial --post-create-network created-a,created-b --post-start-network started",
		},
		{
			name: "every remaining option",
			task: ServiceCreateTask{
				Service:       "postgres",
				Name:          "db",
				ConfigOptions: "--shm-size 128m",
				Memory:        512,
				ShmSize:       "128m",
				Password:      "user-pw",
				RootPassword:  "root-pw",
			},
			want: "dokku --quiet postgres:create db --config-options --shm-size 128m --memory 512 --shm-size 128m --password user-pw --root-password root-pw",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(serviceMissing())()
			// The recipe default, applied by SetDefaults on the parse path;
			// a struct built in Go has to state it for DispatchPlan.
			task := tc.task
			task.State = StatePresent
			if got := planCreateCommand(t, testCtx(), task); got != tc.want {
				t.Errorf("command = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestServiceCreateTaskMutationNamesPinnedImage keeps the pinned image visible
// in the plan. Text `docket plan` renders Mutations and never Commands, so a
// pin that only reached the argv would not show up in a plan at all.
func TestServiceCreateTaskMutationNamesPinnedImage(t *testing.T) {
	cases := []struct {
		name string
		task ServiceCreateTask
		want string
	}{
		{"unpinned", ServiceCreateTask{Service: "redis", Name: "cache"}, "redis:create cache"},
		{"image and version", ServiceCreateTask{Service: "postgres", Name: "db", Image: "postgis/postgis", ImageVersion: "13-master"}, "postgres:create db (image postgis/postgis:13-master)"},
		{"image alone", ServiceCreateTask{Service: "postgres", Name: "db", Image: "postgis/postgis"}, "postgres:create db (image postgis/postgis)"},
		{"version alone", ServiceCreateTask{Service: "postgres", Name: "db", ImageVersion: "13"}, "postgres:create db (image version 13)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(serviceMissing())()
			task := tc.task
			task.State = StatePresent
			plan := task.Plan(testCtx())
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if len(plan.Mutations) != 1 || plan.Mutations[0] != tc.want {
				t.Errorf("mutations = %v, want [%s]", plan.Mutations, tc.want)
			}
		})
	}
}

// TestServiceCreateTaskMasksSecretsInCommands asserts the two password flags
// and every custom_env value are registered as sensitive, so they never reach
// the JSON `commands` stream or `apply --verbose` in the clear.
func TestServiceCreateTaskMasksSecretsInCommands(t *testing.T) {
	task := ServiceCreateTask{
		Service:      "postgres",
		Name:         "db",
		CustomEnv:    map[string]string{"POSTGRES_PASSWORD": "env-secret"},
		Password:     "user-secret",
		RootPassword: "root-secret",
		State:        StatePresent,
	}

	masker := subprocess.NewMasker(sensitiveValuesFromTask(&task)...)
	ctx := subprocess.ContextWithMasker(testCtx(), masker)
	defer subprocess.SetExecRunner(serviceMissing())()

	got := planCreateCommand(t, ctx, task)
	for _, secret := range []string{"env-secret", "user-secret", "root-secret"} {
		if strings.Contains(got, secret) {
			t.Errorf("command %q leaks %q", got, secret)
		}
	}
	// The custom_env key is not a secret and must stay legible.
	if !strings.Contains(got, "POSTGRES_PASSWORD=***") {
		t.Errorf("command %q should mask the value but keep the key", got)
	}
}

func TestServiceCreateTaskValidate(t *testing.T) {
	cases := []struct {
		name    string
		task    ServiceCreateTask
		wantErr string
	}{
		{
			name:    "create options rejected under absent",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", Memory: 512, State: StateAbsent},
			wantErr: "'image', 'memory' must not be set for state 'absent'",
		},
		{
			name:    "negative memory",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Memory: -1},
			wantErr: "'memory' must not be negative",
		},
		{
			name:    "empty custom env key",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", CustomEnv: map[string]string{"": "v"}},
			wantErr: "'custom_env' keys must not be empty",
		},
		{
			name:    "custom env key with a delimiter",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", CustomEnv: map[string]string{"A=B": "v"}},
			wantErr: `'custom_env' key "A=B" must not contain '=' or ';'`,
		},
		{
			name:    "custom env value with a delimiter",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", CustomEnv: map[string]string{"A": "one;two"}},
			wantErr: `'custom_env' value for "A" must not contain ';'`,
		},
		{
			name:    "empty network entry",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", PostCreateNetwork: []string{""}},
			wantErr: "'post_create_network' entries must not be empty",
		},
		{
			name:    "network entry with a delimiter",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", PostStartNetwork: []string{"a,b"}},
			wantErr: `'post_start_network' entry "a,b" must not contain ','`,
		},
		{
			name:    "unknown image drift mode",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", ImageDrift: "sometimes"},
			wantErr: `'image_drift' must be one of ignore, warn, error, upgrade, got "sometimes"`,
		},
		{
			name:    "image drift without a pin",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", ImageDrift: imageDriftUpgrade},
			wantErr: "'image_drift' requires 'image' or 'image_version'",
		},
		{
			// The running tag belongs to the image being replaced, so there is
			// no safe value to carry over.
			name:    "upgrade with an image but no version",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", ImageDrift: imageDriftUpgrade},
			wantErr: "'image_drift: upgrade' requires 'image_version' when 'image' is set",
		},
		{
			name:    "restart apps without an upgrade",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", ImageVersion: "7.2.5", RestartApps: true},
			wantErr: "'restart_apps' requires 'image_drift: upgrade'",
		},
		{
			name:    "image drift under absent",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", ImageDrift: imageDriftUpgrade, State: StateAbsent},
			wantErr: "'image_drift' must not be set for state 'absent'",
		},
		{
			name:    "restart apps under absent",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", RestartApps: true, State: StateAbsent},
			wantErr: "'restart_apps' must not be set for state 'absent'",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.task.Validate()
			if err == nil {
				t.Fatalf("expected an error, got none")
			}
			if err.Error() != tc.wantErr {
				t.Errorf("error = %q, want %q", err.Error(), tc.wantErr)
			}
			// Plan() must report the same message, so plan, apply, and
			// validate all read alike.
			if plan := tc.task.Plan(testCtx()); plan.Error == nil || plan.Error.Error() != tc.wantErr {
				t.Errorf("Plan error = %v, want %q", plan.Error, tc.wantErr)
			}
		})
	}
}

func TestServiceCreateTaskValidateAcceptsDestroyWithoutOptions(t *testing.T) {
	task := ServiceCreateTask{Service: "redis", Name: "cache", State: StateAbsent}
	if err := task.Validate(); err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGetTasksServiceCreateCreateOptionsParsed(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: create postgres service
      dokku_service_create:
        service: postgres
        name: my-db
        image: postgis/postgis
        image_version: "13-master"
        config_options: "--restart always"
        custom_env:
          POSTGRES_INITDB_ARGS: "--data-checksums"
        memory: 512
        shm_size: 128m
        initial_network: my-network
        post_create_network:
          - created-a
          - created-b
        post_start_network:
          - started
        password: user-pw
        root_password: root-pw
`)

	tasks, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("create postgres service")
	if task == nil {
		t.Fatal("task 'create postgres service' not found")
	}
	scTask, ok := task.(*ServiceCreateTask)
	if !ok {
		t.Fatalf("task is not a *ServiceCreateTask (type is %T)", task)
	}

	want := ServiceCreateTask{
		Service:           "postgres",
		Name:              "my-db",
		Image:             "postgis/postgis",
		ImageVersion:      "13-master",
		ConfigOptions:     "--restart always",
		CustomEnv:         map[string]string{"POSTGRES_INITDB_ARGS": "--data-checksums"},
		Memory:            512,
		ShmSize:           "128m",
		InitialNetwork:    "my-network",
		PostCreateNetwork: []string{"created-a", "created-b"},
		PostStartNetwork:  []string{"started"},
		Password:          "user-pw",
		RootPassword:      "root-pw",
		// The recipe says nothing about drift, so defaults.SetDefaults fills
		// the `default:` tag in on the way through.
		ImageDrift: imageDriftWarn,
		State:      StatePresent,
	}
	if !reflect.DeepEqual(*scTask, want) {
		t.Errorf("parsed task = %#v, want %#v", *scTask, want)
	}
}

// TestGetTasksServiceCreateImageDriftParsed covers the drift fields a recipe
// sets explicitly. The defaulted case is covered by the test above.
func TestGetTasksServiceCreateImageDriftParsed(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: upgrade redis service
      dokku_service_create:
        service: redis
        name: cache
        image: redis
        image_version: "7.2.5"
        image_drift: upgrade
        restart_apps: true
`)

	tasks, err := GetTasks(data, map[string]interface{}{})
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}
	task := tasks.Get("upgrade redis service")
	if task == nil {
		t.Fatal("task 'upgrade redis service' not found")
	}
	scTask, ok := task.(*ServiceCreateTask)
	if !ok {
		t.Fatalf("task is not a *ServiceCreateTask (type is %T)", task)
	}

	want := ServiceCreateTask{
		Service:      "redis",
		Name:         "cache",
		Image:        "redis",
		ImageVersion: "7.2.5",
		ImageDrift:   imageDriftUpgrade,
		RestartApps:  true,
		State:        StatePresent,
	}
	if !reflect.DeepEqual(*scTask, want) {
		t.Errorf("parsed task = %#v, want %#v", *scTask, want)
	}
}

// recordingDokku wraps fakeDokku so a test can assert which dokku commands the
// plan issued - specifically that a probe it should have skipped never ran,
// which no assertion on the returned PlanResult can show.
func recordingDokku(responses map[string]string, calls *[]string) func(context.Context, subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
	inner := fakeDokku(responses)
	return func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		*calls = append(*calls, strings.Join(in.Args, " "))
		return inner(ctx, in)
	}
}

// driftFixture answers the two reads the present branch makes: the service
// exists (fakeDokku always exits 0) and reports the given image reference.
func driftFixture(service, name, runningRef string) map[string]string {
	return map[string]string{
		fmt.Sprintf("--quiet %s:info %s --version", service, name): runningRef,
	}
}

// driftTask is a service pinned to redis:7.2.5, the shape most drift tests want.
func driftTask() ServiceCreateTask {
	return ServiceCreateTask{
		Service:      "redis",
		Name:         "cache",
		Image:        "redis",
		ImageVersion: "7.2.5",
		State:        StatePresent,
	}
}

// planImageWarning returns the single drift warning on a plan, failing the
// test when the plan is not the in-sync-with-one-warning shape #435 describes.
func planImageWarning(t *testing.T, plan PlanResult) PlanWarning {
	t.Helper()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("expected an in-sync plan, got InSync=%v status=%q", plan.InSync, plan.Status)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("an in-sync plan must carry no commands, got %v", plan.Commands)
	}
	if len(plan.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d (%v)", len(plan.Warnings), plan.Warnings)
	}
	w := plan.Warnings[0]
	if w.Reason != WarnReasonServiceImageDrift {
		t.Errorf("warning reason = %q, want %q", w.Reason, WarnReasonServiceImageDrift)
	}
	if w.Message == "" {
		// Both emitters drop an empty message on the floor, so an empty one
		// is the same as no warning at all.
		t.Error("warning message must not be empty")
	}
	return w
}

// TestServiceCreateTaskImageDriftDefaultsToWarn pins that the zero value of
// ImageDrift behaves as "warn". The `default:` tag only reaches a decoded
// recipe, so every task built in Go - ExportGlobal, Examples, the integration
// tests - carries the empty string and depends on this.
func TestServiceCreateTaskImageDriftDefaultsToWarn(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis:7.2.4")))()

	task := driftTask()
	if got := task.imageDriftMode(); got != imageDriftWarn {
		t.Errorf("imageDriftMode() = %q, want %q", got, imageDriftWarn)
	}
	planImageWarning(t, task.Plan(testCtx()))
}

func TestServiceCreateTaskImageDriftInSyncWhenPinsMatch(t *testing.T) {
	cases := []struct {
		name    string
		task    ServiceCreateTask
		running string
	}{
		{"both halves pinned", driftTask(), "redis:7.2.5"},
		{
			name:    "version only",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", ImageVersion: "7.2.5", State: StatePresent},
			running: "redis:7.2.5",
		},
		{
			name:    "image only",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Image: "redis", State: StatePresent},
			running: "redis:7.2.5",
		},
		{
			name:    "registry port is not a tag",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", Image: "registry.example.com:5000/redis", ImageVersion: "7.2.5", State: StatePresent},
			running: "registry.example.com:5000/redis:7.2.5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", tc.running)))()
			plan := tc.task.Plan(testCtx())
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if !plan.InSync || plan.Status != PlanStatusOK {
				t.Errorf("expected in sync, got InSync=%v status=%q", plan.InSync, plan.Status)
			}
			if len(plan.Warnings) != 0 {
				t.Errorf("expected no warnings, got %v", plan.Warnings)
			}
		})
	}
}

func TestServiceCreateTaskImageDriftWarnStaysInSync(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis:7.2.4")))()

	task := driftTask()
	task.ImageDrift = imageDriftWarn
	w := planImageWarning(t, task.Plan(testCtx()))
	for _, want := range []string{"redis:7.2.4", "redis:7.2.5", "image_drift: upgrade"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("warning message %q does not mention %q", w.Message, want)
		}
	}

	// ExecutePlan carries plan warnings out of the in-sync branch, so apply
	// reports the same diagnostic plan did.
	state := task.Execute(testCtx())
	if state.Error != nil {
		t.Fatalf("unexpected execute error: %v", state.Error)
	}
	if state.Changed {
		t.Error("a warn-mode drift must not change the server")
	}
	if len(state.Warnings) != 1 || state.Warnings[0].Reason != WarnReasonServiceImageDrift {
		t.Errorf("expected the warning to survive onto the apply state, got %v", state.Warnings)
	}
}

// TestServiceCreateTaskImageDriftWarnsOnUnreadableImage covers a service whose
// container is gone: dokku prints nothing, so there is nothing to compare.
// Reporting in sync silently would tell a recipe that asked for drift to be
// caught that everything is fine.
func TestServiceCreateTaskImageDriftWarnsOnUnreadableImage(t *testing.T) {
	for _, mode := range []string{imageDriftWarn, imageDriftError, imageDriftUpgrade} {
		t.Run(mode, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "")))()
			task := driftTask()
			task.ImageDrift = mode
			w := planImageWarning(t, task.Plan(testCtx()))
			if !strings.Contains(w.Message, "no running image") {
				t.Errorf("warning message %q does not say the image could not be read", w.Message)
			}
		})
	}
}

func TestServiceCreateTaskImageDriftIgnoreSkipsTheProbe(t *testing.T) {
	var calls []string
	defer subprocess.SetExecRunner(recordingDokku(driftFixture("redis", "cache", "redis:7.2.4"), &calls))()

	task := driftTask()
	task.ImageDrift = imageDriftIgnore
	plan := task.Plan(testCtx())
	if !plan.InSync || len(plan.Warnings) != 0 {
		t.Errorf("expected a silent in-sync plan, got InSync=%v warnings=%v", plan.InSync, plan.Warnings)
	}
	for _, call := range calls {
		if strings.Contains(call, "redis:info") {
			t.Errorf("ignore mode must not read the image, but issued %q", call)
		}
	}
}

// TestServiceCreateTaskImageDriftSkipsTheProbeWithoutAPin pins that an
// unpinned recipe costs no extra round trip: with no declared image there is
// nothing to compare the running one against.
func TestServiceCreateTaskImageDriftSkipsTheProbeWithoutAPin(t *testing.T) {
	var calls []string
	defer subprocess.SetExecRunner(recordingDokku(driftFixture("redis", "cache", "redis:7.2.4"), &calls))()

	task := ServiceCreateTask{Service: "redis", Name: "cache", State: StatePresent}
	if plan := task.Plan(testCtx()); !plan.InSync || len(plan.Warnings) != 0 {
		t.Errorf("expected a silent in-sync plan, got InSync=%v warnings=%v", plan.InSync, plan.Warnings)
	}
	for _, call := range calls {
		if strings.Contains(call, "redis:info") {
			t.Errorf("an unpinned task must not read the image, but issued %q", call)
		}
	}
}

func TestServiceCreateTaskImageDriftErrorReportsMismatch(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis:7.2.4")))()

	task := driftTask()
	task.ImageDrift = imageDriftError
	plan := task.Plan(testCtx())
	if plan.Error == nil {
		t.Fatal("expected a plan error, got none")
	}
	if plan.Status != PlanStatusError {
		t.Errorf("status = %q, want %q", plan.Status, PlanStatusError)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("a probe-error plan must carry no commands, got %v", plan.Commands)
	}
	want := `redis service cache is running redis:7.2.4, recipe pins redis:7.2.5`
	if plan.Error.Error() != want {
		t.Errorf("error = %q, want %q", plan.Error.Error(), want)
	}
}

func TestServiceCreateTaskImageDriftUpgradeCommand(t *testing.T) {
	cases := []struct {
		name    string
		task    ServiceCreateTask
		running string
		want    string
	}{
		{
			name:    "both halves pinned",
			task:    driftTask(),
			running: "redis:7.2.4",
			want:    "dokku --quiet redis:upgrade cache --image redis --image-version 7.2.5",
		},
		{
			// The recipe named no image, so the running one is carried over
			// rather than letting dokku fall back to its plugin default.
			name:    "version only keeps the running image name",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", ImageVersion: "7.2.5", State: StatePresent},
			running: "custom/redis:7.2.4",
			want:    "dokku --quiet redis:upgrade cache --image custom/redis --image-version 7.2.5",
		},
		{
			name:    "registry port in the running image",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", ImageVersion: "7.2.5", State: StatePresent},
			running: "registry.example.com:5000/redis:7.2.4",
			want:    "dokku --quiet redis:upgrade cache --image registry.example.com:5000/redis --image-version 7.2.5",
		},
		{
			name:    "untagged running image",
			task:    ServiceCreateTask{Service: "redis", Name: "cache", ImageVersion: "7.2.5", State: StatePresent},
			running: "redis",
			want:    "dokku --quiet redis:upgrade cache --image redis --image-version 7.2.5",
		},
		{
			name: "every option the upgrade accepts is re-passed",
			task: ServiceCreateTask{
				Service:           "redis",
				Name:              "cache",
				Image:             "redis",
				ImageVersion:      "7.2.5",
				ConfigOptions:     "--cpus 2",
				CustomEnv:         map[string]string{"B": "two", "A": "one"},
				ShmSize:           "128m",
				InitialNetwork:    "net-a",
				PostCreateNetwork: []string{"net-b", "net-c"},
				PostStartNetwork:  []string{"net-d"},
				State:             StatePresent,
			},
			running: "redis:7.2.4",
			want: "dokku --quiet redis:upgrade cache --image redis --image-version 7.2.5 " +
				"--config-options --cpus 2 --custom-env A=one;B=two --shm-size 128m " +
				"--initial-network net-a --post-create-network net-b,net-c --post-start-network net-d",
		},
		{
			// `<service>:upgrade` accepts none of these three, and the plugin
			// keeps its own copies across a container rebuild.
			name: "options the upgrade does not accept are omitted",
			task: ServiceCreateTask{
				Service:      "redis",
				Name:         "cache",
				Image:        "redis",
				ImageVersion: "7.2.5",
				Memory:       512,
				Password:     "hunter2",
				RootPassword: "hunter3",
				State:        StatePresent,
			},
			running: "redis:7.2.4",
			want:    "dokku --quiet redis:upgrade cache --image redis --image-version 7.2.5",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", tc.running)))()
			task := tc.task
			task.ImageDrift = imageDriftUpgrade
			plan := task.Plan(testCtx())
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync || plan.Status != PlanStatusModify {
				t.Errorf("expected drift, got InSync=%v status=%q", plan.InSync, plan.Status)
			}
			if len(plan.Commands) != 1 {
				t.Fatalf("expected 1 command, got %v", plan.Commands)
			}
			if plan.Commands[0] != tc.want {
				t.Errorf("command = %q, want %q", plan.Commands[0], tc.want)
			}
		})
	}
}

// TestServiceCreateTaskImageDriftUpgradeReportsTheEffectiveTarget pins that
// the plan's prose names the reference the upgrade will actually use, not the
// half the recipe happened to write.
func TestServiceCreateTaskImageDriftUpgradeReportsTheEffectiveTarget(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "custom/redis:7.2.4")))()

	task := ServiceCreateTask{Service: "redis", Name: "cache", ImageVersion: "7.2.5", ImageDrift: imageDriftUpgrade, State: StatePresent}
	plan := task.Plan(testCtx())
	if want := "image drift: custom/redis:7.2.4 -> custom/redis:7.2.5"; plan.Reason != want {
		t.Errorf("reason = %q, want %q", plan.Reason, want)
	}
	if len(plan.Mutations) == 0 || plan.Mutations[0] != "redis:upgrade cache (image custom/redis:7.2.5)" {
		t.Errorf("mutations = %v, want the effective target first", plan.Mutations)
	}
}

// TestServiceCreateTaskImageDriftUpgradeNamesWhatItResets pins the one
// destructive thing an upgrade does that nothing else in the plan would show.
// The plugin rewrites CONFIG_OPTIONS and ENV from the flags it is given and
// blanks them when it is given none, and docket has no read command for either,
// so an undeclared value is lost with no flag on the argv to hint at it.
func TestServiceCreateTaskImageDriftUpgradeNamesWhatItResets(t *testing.T) {
	cases := []struct {
		name string
		task ServiceCreateTask
		want string
	}{
		{
			name: "neither declared",
			task: driftTask(),
			want: "reset config_options and custom_env, which the recipe does not declare",
		},
		{
			name: "config_options declared",
			task: func() ServiceCreateTask { tk := driftTask(); tk.ConfigOptions = "--cpus 2"; return tk }(),
			want: "reset custom_env, which the recipe does not declare",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis:7.2.4")))()
			task := tc.task
			task.ImageDrift = imageDriftUpgrade
			plan := task.Plan(testCtx())
			if len(plan.Mutations) != 2 || plan.Mutations[1] != tc.want {
				t.Errorf("mutations = %v, want %q as the second entry", plan.Mutations, tc.want)
			}
		})
	}

	t.Run("both declared", func(t *testing.T) {
		defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis:7.2.4")))()
		task := driftTask()
		task.ImageDrift = imageDriftUpgrade
		task.ConfigOptions = "--cpus 2"
		task.CustomEnv = map[string]string{"A": "one"}
		if plan := task.Plan(testCtx()); len(plan.Mutations) != 1 {
			t.Errorf("nothing is reset when the recipe declares both, got %v", plan.Mutations)
		}
	})
}

func TestServiceCreateTaskImageDriftUpgradeRestartsOnlyLinkedApps(t *testing.T) {
	cases := []struct {
		name        string
		restartApps bool
		links       string
		wantFlag    bool
	}{
		{"asked for and linked", true, "my-app\nother-app\n", true},
		{"asked for but nothing linked", true, "", false},
		{"linked but not asked for", false, "my-app\n", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := driftFixture("redis", "cache", "redis:7.2.4")
			responses["--quiet redis:links cache"] = tc.links
			defer subprocess.SetExecRunner(fakeDokku(responses))()

			task := driftTask()
			task.ImageDrift = imageDriftUpgrade
			task.RestartApps = tc.restartApps
			plan := task.Plan(testCtx())
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if len(plan.Commands) != 1 {
				t.Fatalf("expected 1 command, got %v", plan.Commands)
			}
			got := strings.Contains(plan.Commands[0], "--restart-apps true")
			if got != tc.wantFlag {
				t.Errorf("--restart-apps true present = %v, want %v (command %q)", got, tc.wantFlag, plan.Commands[0])
			}
			hasMutation := slices.Contains(plan.Mutations, "restart the linked app(s)")
			if hasMutation != tc.wantFlag {
				t.Errorf("restart mutation present = %v, want %v (%v)", hasMutation, tc.wantFlag, plan.Mutations)
			}
		})
	}
}

func TestServiceCreateTaskImageDriftUpgradeMasksSecrets(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis:7.2.4")))()

	task := driftTask()
	task.ImageDrift = imageDriftUpgrade
	task.CustomEnv = map[string]string{"GREETING": "s3cret"}
	ctx := subprocess.ContextWithMasker(testCtx(), subprocess.NewMasker(sensitiveValuesFromTask(&task)...))

	plan := task.Plan(ctx)
	if len(plan.Commands) != 1 {
		t.Fatalf("expected 1 command, got %v", plan.Commands)
	}
	if strings.Contains(plan.Commands[0], "s3cret") {
		t.Errorf("custom_env value leaked into the upgrade command: %q", plan.Commands[0])
	}
	if !strings.Contains(plan.Commands[0], "GREETING=") {
		t.Errorf("custom_env key should stay legible: %q", plan.Commands[0])
	}
}

// TestServiceCreateTaskImageDriftUpgradeRefusesADigestRef covers the one
// running image splitImageRef cannot represent: it divides `redis@sha256:abc`
// on the digest's own colon, and `redis@sha256` is not a name to hand back to
// dokku. Refusing beats rendering a reference that fails only after the old
// container has already been removed.
func TestServiceCreateTaskImageDriftUpgradeRefusesADigestRef(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(driftFixture("redis", "cache", "redis@sha256:abc123")))()

	task := ServiceCreateTask{Service: "redis", Name: "cache", ImageVersion: "7.2.5", ImageDrift: imageDriftUpgrade, State: StatePresent}
	plan := task.Plan(testCtx())
	if plan.Error == nil {
		t.Fatal("expected a plan error, got none")
	}
	if plan.Status != PlanStatusError {
		t.Errorf("status = %q, want %q", plan.Status, PlanStatusError)
	}
	if !strings.Contains(plan.Error.Error(), "set 'image' alongside 'image_version'") {
		t.Errorf("error %q does not say how to resolve it", plan.Error.Error())
	}
}

func TestServiceCreateTaskImageDriftAppliesUpgrade(t *testing.T) {
	var calls []string
	defer subprocess.SetExecRunner(recordingDokku(driftFixture("redis", "cache", "redis:7.2.4"), &calls))()

	task := driftTask()
	task.ImageDrift = imageDriftUpgrade
	state := task.Execute(testCtx())
	if state.Error != nil {
		t.Fatalf("unexpected execute error: %v", state.Error)
	}
	if !state.Changed || state.State != StatePresent {
		t.Errorf("expected a changed, present service, got changed=%v state=%q", state.Changed, state.State)
	}
	if !slices.Contains(calls, "--quiet redis:upgrade cache --image redis --image-version 7.2.5") {
		t.Errorf("the upgrade was not run; calls: %v", calls)
	}
}

// TestServiceCreateTaskImageDriftPropagatesSSHErrors pins that a transport
// failure on either read becomes a probe error rather than being mistaken for
// "no image" or "no linked apps".
func TestServiceCreateTaskImageDriftPropagatesSSHErrors(t *testing.T) {
	cases := []struct {
		name string
		fail string
	}{
		{"image read", "--quiet redis:info cache --version"},
		{"links read", "--quiet redis:links cache"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			responses := driftFixture("redis", "cache", "redis:7.2.4")
			responses["--quiet redis:links cache"] = "my-app\n"
			inner := fakeDokku(responses)
			defer subprocess.SetExecRunner(func(ctx context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
				if strings.Join(in.Args, " ") == tc.fail {
					return subprocess.ExecCommandResponse{}, &subprocess.SSHError{}
				}
				return inner(ctx, in)
			})()

			task := driftTask()
			task.ImageDrift = imageDriftUpgrade
			task.RestartApps = true
			plan := task.Plan(testCtx())
			var sshErr *subprocess.SSHError
			if !errors.As(plan.Error, &sshErr) {
				t.Fatalf("expected an SSHError, got %v", plan.Error)
			}
			if plan.Status != PlanStatusError {
				t.Errorf("status = %q, want %q", plan.Status, PlanStatusError)
			}
		})
	}
}
