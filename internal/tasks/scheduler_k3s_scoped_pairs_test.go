package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// annotationsReportKey is the fakeDokku key for the probe every annotations
// planner runs against an app's default (global) process type. The
// `--process-type --global` pair is the stored-scope sentinel, not an
// accidental repetition of the --global scope flag.
const annotationsReportKey = "--quiet scheduler-k3s:annotations:report node-js-app --resource-type deployment --process-type --global --format json"

func TestSchedulerK3sAnnotationsSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{"global.deployment.keep":"old","global.deployment.stale":"gone"}`,
	}))

	plan := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"keep": "new", "added": "yes"},
		State:        StateSet,
	}.Plan(ctx)

	if plan.Error != nil {
		t.Fatalf("Plan() error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift")
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	// The command carries the complete desired map, sorted, not the per-key
	// delta, and the stale key is removed by its absence from that map.
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:annotations:set --replace --resource-type deployment --process-type --global node-js-app added=yes keep=new") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	want := []string{`set added=yes (new)`, `set keep=new (was "old")`, `unset stale (was "gone")`}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusModify)
	}
}

func TestSchedulerK3sAnnotationsSetRemovesAnUndeclaredKeyAlone(t *testing.T) {
	t.Parallel()
	// Nothing drifts among the declared keys, so only the server-only key is
	// work. The additive present state reports in sync here, which is the gap
	// issue #527 is about.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{"global.deployment.keep":"yes","global.deployment.stale":"gone"}`,
	}))
	task := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"keep": "yes"},
	}

	task.State = StateSet
	plan := task.Plan(ctx)
	if plan.InSync {
		t.Fatal("state 'set' should report the undeclared key as drift")
	}
	if want := []string{`unset stale (was "gone")`}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}

	task.State = StatePresent
	if plan := task.Plan(ctx); !plan.InSync {
		t.Errorf("state 'present' is additive and should be in sync, got %v", plan.Mutations)
	}
}

func TestSchedulerK3sAnnotationsSetConvergesWhenReportMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{"global.deployment.keep":"yes"}`,
	}))

	plan := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"keep": "yes"},
		State:        StateSet,
	}.Plan(ctx)

	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
	if plan.Status != PlanStatusOK {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusOK)
	}
}

func TestSchedulerK3sAnnotationsSetStoresAnEmptyValue(t *testing.T) {
	t.Parallel()
	// dokku's :set --replace stores `key=` as an empty value and the report
	// reads it back, which the per-key form cannot express. Once stored, the
	// same recipe converges.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{"global.deployment.blank":""}`,
	}))
	task := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"blank": ""},
		State:        StateSet,
	}

	if err := task.Validate(); err != nil {
		t.Fatalf("state 'set' should accept an empty value: %v", err)
	}
	if plan := task.Plan(ctx); !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
}

func TestSchedulerK3sAnnotationsSetOnEmptyScopeIsACreate(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{}`,
	}))

	plan := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		Annotations:  map[string]string{"keep": "yes"},
		State:        StateSet,
	}.Plan(ctx)

	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusCreate)
	}
}

func TestSchedulerK3sAnnotationsClearScopesToOneProcessType(t *testing.T) {
	t.Parallel()
	// The regression guard for the failure mode that makes state 'clear'
	// dangerous: dokku reads an omitted --process-type on :clear as "no
	// filter" and would empty every process type's map for the resource type,
	// not just this task's scope.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{"global.deployment.keep":"yes"}`,
	}))

	plan := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		State:        StateClear,
	}.Plan(ctx)

	if plan.Error != nil {
		t.Fatalf("Plan() error: %v", plan.Error)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:annotations:clear --resource-type deployment --process-type --global node-js-app") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusDestroy)
	}
	if want := []string{`unset keep (was "yes")`}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestSchedulerK3sAnnotationsClearCarriesAnExplicitProcessType(t *testing.T) {
	t.Parallel()
	const key = "--quiet scheduler-k3s:annotations:report node-js-app --resource-type deployment --process-type web --format json"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		key: `{"web.deployment.keep":"yes"}`,
	}))

	plan := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ProcessType:  "web",
		ResourceType: "deployment",
		State:        StateClear,
	}.Plan(ctx)

	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "scheduler-k3s:annotations:clear --resource-type deployment --process-type web node-js-app") {
		t.Errorf("unexpected command: %q", plan.Commands[0])
	}
}

func TestSchedulerK3sAnnotationsClearIsInSyncWhenScopeIsEmpty(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		annotationsReportKey: `{}`,
	}))

	plan := SchedulerK3sAnnotationsTask{
		App:          "node-js-app",
		ResourceType: "deployment",
		State:        StateClear,
	}.Plan(ctx)

	if !plan.InSync {
		t.Errorf("expected in sync, got %v", plan.Mutations)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no commands, got %v", plan.Commands)
	}
}

func TestSchedulerK3sLabelsGlobalSetAndClearOmitGlobalPositional(t *testing.T) {
	t.Parallel()
	// Issue #309: --global is a flag on these subcommands, never a positional,
	// or dokku would read it as the app name.
	const key = "--quiet scheduler-k3s:labels:report --global --resource-type deployment --process-type --global --format json"
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		key: `{"global.deployment.tier":"edge"}`,
	}))

	setPlan := SchedulerK3sLabelsTask{
		Global:       true,
		ResourceType: "deployment",
		Labels:       map[string]string{"tier": "core"},
		State:        StateSet,
	}.Plan(ctx)
	if !strings.HasSuffix(setPlan.Commands[0], "scheduler-k3s:labels:set --replace --resource-type deployment --process-type --global --global tier=core") {
		t.Errorf("unexpected set command: %q", setPlan.Commands[0])
	}

	clearPlan := SchedulerK3sLabelsTask{
		Global:       true,
		ResourceType: "deployment",
		State:        StateClear,
	}.Plan(ctx)
	if !strings.HasSuffix(clearPlan.Commands[0], "scheduler-k3s:labels:clear --resource-type deployment --process-type --global --global") {
		t.Errorf("unexpected clear command: %q", clearPlan.Commands[0])
	}
}

func TestSchedulerK3sScopedPairsValidateStates(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		task    SchedulerK3sAnnotationsTask
		wantErr string
	}{
		{
			name:    "set rejects an empty map",
			task:    SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", State: StateSet},
			wantErr: "'annotations' must not be empty for state 'set'",
		},
		{
			name:    "clear rejects a map",
			task:    SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", Annotations: map[string]string{"k": "v"}, State: StateClear},
			wantErr: "'annotations' must not be set for state 'clear'",
		},
		{
			name: "clear accepts no map",
			task: SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", State: StateClear},
		},
		{
			name:    "set rejects a key carrying an equals sign",
			task:    SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", Annotations: map[string]string{"bad=key": "v"}, State: StateSet},
			wantErr: "annotation keys must not contain '=' for state 'set'",
		},
		{
			name: "present accepts a key carrying an equals sign",
			task: SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", Annotations: map[string]string{"odd=key": "v"}},
		},
		{
			name: "set accepts an empty value",
			task: SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", Annotations: map[string]string{"k": ""}, State: StateSet},
		},
		{
			name:    "present rejects an empty value",
			task:    SchedulerK3sAnnotationsTask{App: "a", ResourceType: "deployment", Annotations: map[string]string{"k": ""}},
			wantErr: "annotation values must not be empty for state 'present'",
		},
		{
			name:    "clear still requires a resource type",
			task:    SchedulerK3sAnnotationsTask{App: "a", State: StateClear},
			wantErr: "resource_type is required",
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
