package tasks

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestPsScaleTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 1}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestPsScaleTaskEmptyScale(t *testing.T) {
	t.Parallel()
	task := PsScaleTask{App: "test-app", Scale: map[string]int{}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty scale and state=present should return an error")
	}
}

func TestPsScaleTaskNilScale(t *testing.T) {
	t.Parallel()
	task := PsScaleTask{App: "test-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with nil scale and state=present should return an error")
	}
}

func TestPsScaleCommandDeterministicOrder(t *testing.T) {
	t.Parallel()
	// web and worker both drift, so the ps:scale command lists both (appended
	// after the app) and both mutations are reported. Sorting the process types
	// must yield byte-identical output on every run (issue #341).
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "web: 1\nworker: 1",
	}))

	task := PsScaleTask{
		App:   "test-app",
		Scale: map[string]int{"worker": 3, "web": 2},
		State: StatePresent,
	}

	wantCommands := []string{"dokku ps:scale test-app web=2 worker=3"}
	wantMutations := []string{"scale web=2 (was 1)", "scale worker=3 (was 1)"}

	// Repeat so a reintroduced map-order bug is caught reliably rather than
	// passing by chance on a lucky iteration.
	for i := 0; i < 20; i++ {
		plan := task.Plan(ctx)
		if plan.Error != nil {
			t.Fatalf("iteration %d: unexpected plan error: %v", i, plan.Error)
		}
		if !reflect.DeepEqual(plan.Commands, wantCommands) {
			t.Fatalf("iteration %d commands = %v, want %v", i, plan.Commands, wantCommands)
		}
		if !reflect.DeepEqual(plan.Mutations, wantMutations) {
			t.Fatalf("iteration %d mutations = %v, want %v", i, plan.Mutations, wantMutations)
		}
	}
}

func TestGetTasksPsScaleTaskParsedCorrectly(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: scale processes
      dokku_ps_scale:
        app: test-app
        scale:
          web: 2
          worker: 1
        skip_deploy: true
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("scale processes")
	if task == nil {
		t.Fatal("task 'scale processes' not found")
	}

	psTask, ok := task.(*PsScaleTask)
	if !ok {
		pt, ok2 := task.(PsScaleTask)
		if !ok2 {
			t.Fatalf("task is not a PsScaleTask (type is %T)", task)
		}
		psTask = &pt
	}

	if psTask.App != "test-app" {
		t.Errorf("App = %q, want %q", psTask.App, "test-app")
	}
	if len(psTask.Scale) != 2 {
		t.Fatalf("expected 2 scale entries, got %d", len(psTask.Scale))
	}
	if psTask.Scale["web"] != 2 {
		t.Errorf("Scale[web] = %d, want %d", psTask.Scale["web"], 2)
	}
	if psTask.Scale["worker"] != 1 {
		t.Errorf("Scale[worker] = %d, want %d", psTask.Scale["worker"], 1)
	}
	// SkipDeploy is a *bool, so an explicit skip_deploy: true survives decoding.
	if psTask.SkipDeploy == nil || !*psTask.SkipDeploy {
		t.Error("SkipDeploy = false, want true (YAML value should be preserved)")
	}
}

func TestPsScaleTaskSetEmptyScale(t *testing.T) {
	t.Parallel()
	task := PsScaleTask{App: "test-app", Scale: map[string]int{}, State: StateSet}
	if err := task.Validate(); err == nil {
		t.Fatal("Validate with empty scale and state=set should return an error")
	} else if !strings.Contains(err.Error(), "'scale' must not be empty for state 'set'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestPsScaleTaskRejectsANegativeQuantity(t *testing.T) {
	t.Parallel()
	// dokku parses the tuple with strconv.Atoi and stores whatever comes back,
	// so -1 round-trips through the formation and reaches the scheduler
	// (dokku/dokku#9048). docket refuses it without contacting a server.
	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": -1}, State: StatePresent}
	err := task.Validate()
	if err == nil {
		t.Fatal("Validate with a negative quantity should return an error")
	}
	if !strings.Contains(err.Error(), "must not be negative") || !strings.Contains(err.Error(), `scale["web"]`) {
		t.Errorf("expected the error to name the offending entry, got: %v", err)
	}
}

func TestPsScaleTaskRejectsAnUnreadableProcessType(t *testing.T) {
	t.Parallel()
	// Every rejected spelling is one getPsScale cannot read back out of the
	// ps:scale report, so the task would plan the same change on every run:
	// the report collapses whitespace and splits each line on its first colon,
	// and dokku splits each tuple on its first '='.
	for _, proctype := range []string{"", "web=1", "web worker", "web\tworker", "a:b"} {
		t.Run(fmt.Sprintf("%q", proctype), func(t *testing.T) {
			t.Parallel()
			task := PsScaleTask{App: "test-app", Scale: map[string]int{proctype: 1}, State: StatePresent}
			if err := task.Validate(); err == nil {
				t.Fatalf("Validate with process type %q should return an error", proctype)
			}
		})
	}
	// Control: the spellings a Procfile actually uses stay valid.
	for _, proctype := range []string{"web", "worker", "release", "release-phase", "release_phase"} {
		t.Run(proctype, func(t *testing.T) {
			t.Parallel()
			task := PsScaleTask{App: "test-app", Scale: map[string]int{proctype: 1}, State: StatePresent}
			if err := task.Validate(); err != nil {
				t.Errorf("Validate with process type %q returned an error: %v", proctype, err)
			}
		})
	}
}

func TestPsScaleSetPlansTheWholeFormation(t *testing.T) {
	t.Parallel()
	// worker runs but the recipe does not name it, so state 'set' zeroes it in
	// the same authoritative call that scales web.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "web: 1\nworker: 3",
	}))

	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 2}, State: StateSet}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when an undeclared process type is running")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusModify)
	}
	wantCommands := []string{"dokku ps:scale --replace test-app web=2"}
	if !reflect.DeepEqual(plan.Commands, wantCommands) {
		t.Errorf("commands = %v, want %v", plan.Commands, wantCommands)
	}
	wantMutations := []string{"scale web=2 (was 1)", "scale worker=0 (was 3, undeclared)"}
	if !reflect.DeepEqual(plan.Mutations, wantMutations) {
		t.Errorf("mutations = %v, want %v", plan.Mutations, wantMutations)
	}
}

func TestPsScaleSetSendsDeclaredTypesThatAlreadyMatch(t *testing.T) {
	t.Parallel()
	// Only the undeclared type drifts, but ps:scale --replace rebuilds the
	// formation out of the tuples it is handed, so omitting the in-sync ones
	// would zero them.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "release: 1\nweb: 2\nworker: 3",
	}))

	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 2, "release": 1}, State: StateSet}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	wantCommands := []string{"dokku ps:scale --replace test-app release=1 web=2"}
	if !reflect.DeepEqual(plan.Commands, wantCommands) {
		t.Errorf("commands = %v, want %v", plan.Commands, wantCommands)
	}
	wantMutations := []string{"scale worker=0 (was 3, undeclared)"}
	if !reflect.DeepEqual(plan.Mutations, wantMutations) {
		t.Errorf("mutations = %v, want %v", plan.Mutations, wantMutations)
	}
}

func TestPsScaleSetIgnoresAZeroedProcessType(t *testing.T) {
	t.Parallel()
	// dokku keeps a zeroed process type visible in ps:scale - one the Procfile
	// names stays in the formation at zero, and one it does not sits in the
	// scale.old property until a deploy clears it, which never happens under
	// skip_deploy or on an app that was never deployed. Counting it as drift
	// would leave state 'set' planning the same change forever.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "web: 2\nworker: 0",
	}))

	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 2}, State: StateSet}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in sync, got mutations %v", plan.Mutations)
	}
	if plan.Status != PlanStatusOK {
		t.Errorf("Status = %v, want %v", plan.Status, PlanStatusOK)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no commands, got %v", plan.Commands)
	}
}

func TestPsScaleSetTreatsAnUndeclaredZeroAsInSync(t *testing.T) {
	t.Parallel()
	// The same rule read from the desired side: declaring worker: 0 on an app
	// that has never run a worker is already true.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "web: 2",
	}))

	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 2, "worker": 0}, State: StateSet}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in sync, got mutations %v", plan.Mutations)
	}
}

func TestPsScalePresentTreatsAnUnreportedZeroAsInSync(t *testing.T) {
	t.Parallel()
	// The present branch shares the rule. Without it, `worker: 0` on an app
	// whose report does not mention worker drifts on every run: applying it
	// files worker under scale.old, and the deploy that follows deletes that
	// property, so the next probe is back where it started.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "web: 2",
	}))

	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 2, "worker": 0}, State: StatePresent}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in sync, got mutations %v", plan.Mutations)
	}
}

func TestPsScaleSetCommandDeterministicOrder(t *testing.T) {
	t.Parallel()
	// The undeclared process types come out of a map, so without the sort the
	// mutation list would shuffle between runs (issue #341).
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "clock: 1\nweb: 1\nworker: 2\nrelease: 3",
	}))

	task := PsScaleTask{App: "test-app", Scale: map[string]int{"web": 2, "beat": 1}, State: StateSet}

	wantCommands := []string{"dokku ps:scale --replace test-app beat=1 web=2"}
	wantMutations := []string{
		"scale beat=1 (new)",
		"scale web=2 (was 1)",
		"scale clock=0 (was 1, undeclared)",
		"scale release=0 (was 3, undeclared)",
		"scale worker=0 (was 2, undeclared)",
	}

	for i := 0; i < 20; i++ {
		plan := task.Plan(ctx)
		if plan.Error != nil {
			t.Fatalf("iteration %d: unexpected plan error: %v", i, plan.Error)
		}
		if !reflect.DeepEqual(plan.Commands, wantCommands) {
			t.Fatalf("iteration %d commands = %v, want %v", i, plan.Commands, wantCommands)
		}
		if !reflect.DeepEqual(plan.Mutations, wantMutations) {
			t.Fatalf("iteration %d mutations = %v, want %v", i, plan.Mutations, wantMutations)
		}
	}
}

func TestPsScaleSetSkipDeployFlagsPrecedeTheApp(t *testing.T) {
	t.Parallel()
	// ps:scale parses with Go's flag package, which stops at the first
	// positional, so a flag after the app name would be read as a process tuple.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet ps:scale test-app": "web: 1",
	}))

	task := PsScaleTask{
		App:        "test-app",
		Scale:      map[string]int{"web": 2},
		SkipDeploy: boolPtr(true),
		State:      StateSet,
	}
	plan := task.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	wantCommands := []string{"dokku ps:scale --replace --skip-deploy test-app web=2"}
	if !reflect.DeepEqual(plan.Commands, wantCommands) {
		t.Errorf("commands = %v, want %v", plan.Commands, wantCommands)
	}
}
