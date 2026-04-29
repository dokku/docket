package tasks

import (
	"strings"
	"testing"
)

// TestIntegrationPlanDetectsMissingApp asserts Plan() reports drift for a
// dokku_app task when the target app does not exist.
func TestIntegrationPlanDetectsMissingApp(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-plan-detect"
	destroyApp(appName)
	defer destroyApp(appName)

	task := AppTask{App: appName, State: StatePresent}

	plan := task.Plan()
	if plan.Error != nil {
		t.Fatalf("Plan() errored: %v", plan.Error)
	}
	if plan.InSync {
		t.Errorf("expected drift on missing app, got InSync=true")
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected status=%q, got %q", PlanStatusCreate, plan.Status)
	}
}

// TestIntegrationPlanInSyncAfterApply applies a single dokku_app task then
// verifies Plan() reports InSync. This is the round-trip property: every
// apply followed by an immediate plan must report clean.
func TestIntegrationPlanInSyncAfterApply(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-plan-roundtrip"
	destroyApp(appName)
	defer destroyApp(appName)

	task := AppTask{App: appName, State: StatePresent}
	if state := task.Execute(); state.Error != nil {
		t.Fatalf("apply errored: %v", state.Error)
	}

	plan := task.Plan()
	if plan.Error != nil {
		t.Fatalf("plan errored after apply: %v", plan.Error)
	}
	if !plan.InSync {
		t.Errorf("expected InSync after apply, got %+v", plan)
	}
}

// TestIntegrationPlanDoesNotMutate is the safety contract for plan: it must
// never create, modify, or destroy server state.
func TestIntegrationPlanDoesNotMutate(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-plan-no-mutate"
	destroyApp(appName)
	defer destroyApp(appName)

	task := AppTask{App: appName, State: StatePresent}
	_ = task.Plan()

	exists, _ := appExists(appName)
	if exists {
		t.Errorf("Plan() unexpectedly created %s", appName)
	}
}

// TestIntegrationPlanConfigItemizes asserts the per-key Mutations contract
// for multi-mutation tasks against a real Dokku.
func TestIntegrationPlanConfigItemizes(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-plan-config"
	destroyApp(appName)
	defer destroyApp(appName)

	if state := (AppTask{App: appName, State: StatePresent}).Execute(); state.Error != nil {
		t.Fatalf("setup apps:create failed: %v", state.Error)
	}

	task := ConfigTask{
		App:     appName,
		Restart: false,
		Config:  map[string]string{"DOCKET_TEST_KEY_A": "1", "DOCKET_TEST_KEY_B": "2"},
		State:   StatePresent,
	}

	plan := task.Plan()
	if plan.Error != nil {
		t.Fatalf("plan errored: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift, got InSync=true")
	}
	if len(plan.Mutations) != 2 {
		t.Errorf("expected 2 mutations (one per key), got %d: %v", len(plan.Mutations), plan.Mutations)
	}
	if len(plan.Commands) == 0 {
		t.Error("expected Commands to be populated for drift; got empty slice")
	}
}

// TestIntegrationPlanCommandsPopulatedOnDrift asserts the per-task contract
// that whenever Plan() reports drift (Status `+`/`~`/`-`), Commands carries
// at least one resolved dokku command line. The matching strings are what
// `docket plan --json` emits in the `commands` array.
func TestIntegrationPlanCommandsPopulatedOnDrift(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-plan-commands"
	destroyApp(appName)
	defer destroyApp(appName)

	plan := (AppTask{App: appName, State: StatePresent}).Plan()
	if plan.Error != nil {
		t.Fatalf("plan errored: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift on missing app")
	}
	if len(plan.Commands) == 0 {
		t.Fatal("expected Commands to be populated for drift")
	}
	if !strings.Contains(plan.Commands[0], "apps:create") {
		t.Errorf("expected first command to mention apps:create, got %q", plan.Commands[0])
	}
}

// TestIntegrationExecuteIdempotent asserts the property the new design
// guarantees: a second Execute() on a task that just ran reports
// Changed=false (because the apply closure short-circuits inside Plan via
// the InSync check). This is the user-visible payoff of the refactor.
func TestIntegrationExecuteIdempotent(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-plan-idempotent"
	destroyApp(appName)
	defer destroyApp(appName)

	task := AppTask{App: appName, State: StatePresent}

	first := task.Execute()
	if first.Error != nil {
		t.Fatalf("first apply errored: %v", first.Error)
	}
	if !first.Changed {
		t.Error("first apply should report Changed=true (app was missing)")
	}

	second := task.Execute()
	if second.Error != nil {
		t.Fatalf("second apply errored: %v", second.Error)
	}
	if second.Changed {
		t.Error("second apply should report Changed=false (app already present)")
	}
}
