package tasks

import (
	"context"
	"reflect"
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
func planCreateCommand(t *testing.T, task ServiceCreateTask) string {
	t.Helper()
	plan := task.Plan()
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
	result := task.Execute()
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
			if got := planCreateCommand(t, task); got != tc.want {
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
			plan := task.Plan()
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

	subprocess.SetGlobalSensitive(sensitiveValuesFromTask(&task))
	defer subprocess.SetGlobalSensitive(nil)
	defer subprocess.SetExecRunner(serviceMissing())()

	got := planCreateCommand(t, task)
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
			if plan := tc.task.Plan(); plan.Error == nil || plan.Error.Error() != tc.wantErr {
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
		State:             StatePresent,
	}
	if !reflect.DeepEqual(*scTask, want) {
		t.Errorf("parsed task = %#v, want %#v", *scTask, want)
	}
}
