package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestSchedulerK3sAutoscalingAuthTaskSensitiveValues(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:     "test-app",
		Trigger: "aws-secret-manager",
		Metadata: map[string]string{
			"awsRegion":          "us-east-1",
			"awsSecretAccessKey": "REALSECRET",
		},
		State: StatePresent,
	}
	got := task.SensitiveValues()
	// Every non-empty metadata value is masked, regardless of key.
	if !sortedEqual(got, []string{"us-east-1", "REALSECRET"}) {
		t.Errorf("SensitiveValues() = %v, want both metadata values", got)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskSensitiveValuesAbsentEmpty(t *testing.T) {
	t.Parallel()
	// On absent the values are empty (only keys matter), so nothing is
	// contributed from the struct; probed values are registered at plan time.
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StateAbsent,
	}
	if got := task.SensitiveValues(); len(got) != 0 {
		t.Errorf("SensitiveValues() = %v, want empty for absent", got)
	}
}

func TestSchedulerK3sAutoscalingAuthUnsetMasksProbedSecrets(t *testing.T) {
	t.Parallel()
	// The probed secrets are registered with whatever masker the planning
	// context carries, so the test supplies one and reads back through it.
	masker := subprocess.NewMasker()
	ctx := subprocess.ContextWithMasker(testCtx(), masker)

	// The server holds two metadata keys; the task clears one, so the other
	// survives and is read back (with its secret value) to be re-declared in
	// the replacing call.
	ctx = subprocess.ContextWithRunner(ctx, fakeDokku(map[string]string{
		"--quiet scheduler-k3s:autoscaling-auth:report test-app --format json": `{"aws-secret-manager.secretName":"my-secret","aws-secret-manager.awsSecretAccessKey":"REALSECRET"}`,
	}))

	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StateAbsent,
	}
	result := task.Plan(ctx)
	if result.Error != nil {
		t.Fatalf("Plan returned error: %v", result.Error)
	}

	// Both the cleared key's old value and the surviving key's value are
	// server-probed secrets; they must be registered so every sink masks them.
	registered := masker.Values()
	for _, secret := range []string{"my-secret", "REALSECRET"} {
		found := false
		for _, v := range registered {
			if v == secret {
				found = true
			}
		}
		if !found {
			t.Errorf("probed secret %q not registered with masker: %v", secret, registered)
		}
	}

	// The rendered mutations (unset ... (was "my-secret")) and the rendered
	// command (which re-declares the survivor as --metadata k=REALSECRET) must
	// both mask to *** once the probed values are registered.
	for _, line := range append(append([]string{}, result.Mutations...), result.Commands...) {
		if masked := masker.String(line); strings.Contains(masked, "REALSECRET") || strings.Contains(masked, "my-secret") {
			t.Errorf("plan output leaked a probed secret after masking: %q -> %q", line, masked)
		}
	}
}

func TestSchedulerK3sAutoscalingAuthTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    "invalid",
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestSchedulerK3sAutoscalingAuthTaskMissingApp(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "app is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskGlobalWithAppSet(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Global:   true,
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskMissingTrigger(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Metadata: map[string]string{"awsRegion": "us-east-1"},
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when trigger is empty")
	}
	if !strings.Contains(result.Error.Error(), "trigger is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskPresentWithoutMetadata(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:     "test-app",
		Trigger: "aws-secret-manager",
		State:   StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when present state has no metadata")
	}
	if !strings.Contains(result.Error.Error(), "'metadata' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskAbsentWithoutMetadata(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:     "test-app",
		Trigger: "aws-secret-manager",
		State:   StateAbsent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when absent state has no metadata")
	}
	if !strings.Contains(result.Error.Error(), "'metadata' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskEmptyMetadataKey(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"": "value"},
		State:    StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when a metadata key is empty")
	}
	if !strings.Contains(result.Error.Error(), "metadata keys must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskPresentEmptyValueRejected(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StatePresent,
	}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected error when a present-state metadata value is empty")
	}
	if !strings.Contains(err.Error(), "metadata values must not be empty for state 'present'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestSchedulerK3sAutoscalingAuthTaskAbsentEmptyValueAllowed(t *testing.T) {
	t.Parallel()
	task := SchedulerK3sAutoscalingAuthTask{
		App:      "test-app",
		Trigger:  "aws-secret-manager",
		Metadata: map[string]string{"secretName": ""},
		State:    StateAbsent,
	}
	if err := task.Validate(); err != nil {
		t.Fatalf("absent-state empty value should be allowed (clears the key), got %v", err)
	}
}

// autoscalingAuthReportKey is the fakeDokku key for the probe every
// autoscaling-auth planner runs.
const autoscalingAuthReportKey = "--quiet scheduler-k3s:autoscaling-auth:report node-js-app --format json"

func TestSchedulerK3sAutoscalingAuthSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		autoscalingAuthReportKey: `{"datadog.apiKey":"old","datadog.datadogSite":"gone"}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		App:      "node-js-app",
		Trigger:  "datadog",
		Metadata: map[string]string{"apiKey": "new", "appKey": "added"},
		State:    StateSet,
	}.Plan(ctx)

	if plan.Error != nil {
		t.Fatalf("Plan() error: %v", plan.Error)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	// The whole declared map travels in one --replace call, so the trigger is
	// never left carrying a mixture of the old and new sets.
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:autoscaling-auth:set --replace node-js-app datadog --metadata apiKey=new --metadata appKey=added") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	want := []string{`set apiKey=new (was "old")`, `set appKey=added (new)`, `unset datadogSite (was "gone")`}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestSchedulerK3sAutoscalingAuthSetConvergesWhenReportMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		autoscalingAuthReportKey: `{"datadog.apiKey":"key"}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		App:      "node-js-app",
		Trigger:  "datadog",
		Metadata: map[string]string{"apiKey": "key"},
		State:    StateSet,
	}.Plan(ctx)

	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
}

func TestSchedulerK3sAutoscalingAuthClearWipesTheTrigger(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		autoscalingAuthReportKey: `{"datadog.apiKey":"key","other-trigger.apiKey":"untouched"}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		App:     "node-js-app",
		Trigger: "datadog",
		State:   StateClear,
	}.Plan(ctx)

	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	// There is no :autoscaling-auth:clear subcommand; a bare :set with no
	// --metadata is what dokku reads as "wipe this trigger".
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:autoscaling-auth:set node-js-app datadog") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	// Only this trigger's keys are in scope; another trigger's are not.
	if want := []string{`unset apiKey (was "key")`}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestSchedulerK3sAutoscalingAuthClearIsInSyncWhenTriggerIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		autoscalingAuthReportKey: `{"other-trigger.apiKey":"untouched"}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		App:     "node-js-app",
		Trigger: "datadog",
		State:   StateClear,
	}.Plan(ctx)

	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
}

func TestSchedulerK3sAutoscalingAuthAbsentReplacesInOneCall(t *testing.T) {
	t.Parallel()
	// Before dokku grew --replace this was a wipe followed by a restore, so a
	// failure between the two took every surviving key with it. One call now
	// drops the named keys and re-declares the survivors together.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		autoscalingAuthReportKey: `{"datadog.apiKey":"key","datadog.datadogSite":"site"}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		App:      "node-js-app",
		Trigger:  "datadog",
		Metadata: map[string]string{"datadogSite": ""},
		State:    StateAbsent,
	}.Plan(ctx)

	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:autoscaling-auth:set --replace node-js-app datadog --metadata apiKey=key") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	// Nothing is removed and re-added any more, so there is no restore to report.
	if want := []string{`unset datadogSite (was "site")`}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestSchedulerK3sAutoscalingAuthAbsentWipesWhenNothingSurvives(t *testing.T) {
	t.Parallel()
	// --replace rejects an empty --metadata list, so clearing the last key has
	// to fall back to the bare wipe.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		autoscalingAuthReportKey: `{"datadog.apiKey":"key"}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		App:      "node-js-app",
		Trigger:  "datadog",
		Metadata: map[string]string{"apiKey": ""},
		State:    StateAbsent,
	}.Plan(ctx)

	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:autoscaling-auth:set node-js-app datadog") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
}

func TestSchedulerK3sAutoscalingAuthGlobalSetOmitsGlobalPositional(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet scheduler-k3s:autoscaling-auth:report --global --format json": `{}`,
	}))

	plan := SchedulerK3sAutoscalingAuthTask{
		Global:   true,
		Trigger:  "datadog",
		Metadata: map[string]string{"apiKey": "key"},
		State:    StateSet,
	}.Plan(ctx)

	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:autoscaling-auth:set --replace --global datadog --metadata apiKey=key") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
}

func TestSchedulerK3sAutoscalingAuthValidateStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    SchedulerK3sAutoscalingAuthTask
		wantErr string
	}{
		{
			name:    "set rejects an empty map",
			task:    SchedulerK3sAutoscalingAuthTask{App: "a", Trigger: "datadog", State: StateSet},
			wantErr: "'metadata' must not be empty for state 'set'",
		},
		{
			name:    "clear rejects a map",
			task:    SchedulerK3sAutoscalingAuthTask{App: "a", Trigger: "datadog", Metadata: map[string]string{"k": "v"}, State: StateClear},
			wantErr: "'metadata' must not be set for state 'clear'",
		},
		{
			name: "clear accepts no map",
			task: SchedulerK3sAutoscalingAuthTask{App: "a", Trigger: "datadog", State: StateClear},
		},
		{
			name:    "clear still requires a trigger",
			task:    SchedulerK3sAutoscalingAuthTask{App: "a", State: StateClear},
			wantErr: "trigger is required",
		},
		{
			// Every state carries its keys in a --metadata key=value flag, so
			// unlike annotations and labels the present state needs the check
			// too: dokku splits on the first '=' and would store "bad".
			name:    "present rejects a key carrying an equals sign",
			task:    SchedulerK3sAutoscalingAuthTask{App: "a", Trigger: "datadog", Metadata: map[string]string{"bad=key": "v"}},
			wantErr: "metadata keys must not contain '=' for state 'present'",
		},
		{
			name:    "set rejects a key carrying an equals sign",
			task:    SchedulerK3sAutoscalingAuthTask{App: "a", Trigger: "datadog", Metadata: map[string]string{"bad=key": "v"}, State: StateSet},
			wantErr: "metadata keys must not contain '=' for state 'set'",
		},
		{
			name: "set accepts an empty value",
			task: SchedulerK3sAutoscalingAuthTask{App: "a", Trigger: "datadog", Metadata: map[string]string{"k": ""}, State: StateSet},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			err := tc.task.Validate()
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("Validate() error: %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("error = %v, want it to contain %q", err, tc.wantErr)
			}
		})
	}
}
