package tasks

import (
	"strings"
	"testing"
)

func TestIntegrationServiceCreateAndDestroy(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceName := "docket-test-service"
	serviceType := "redis"

	// ensure clean state
	destroyService(testCtx(), serviceType, serviceName)

	// create the service
	task := ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to create service: %v", result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for new service creation")
	}

	// creating again should be idempotent
	result = task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent create failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for existing service")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// destroy the service
	destroyTask := ServiceCreateTask{Service: serviceType, Name: serviceName, State: StateAbsent}
	result = destroyTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to destroy service: %v", result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	if !result.Changed {
		t.Error("expected changed=true for service destruction")
	}

	// destroying again should be idempotent
	result = destroyTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("idempotent destroy failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false for nonexistent service")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
}

// TestIntegrationServiceCreatePinnedImage proves the create-time options reach
// dokku: it pins an image and tag, then reads the running container back with
// `<service>:info --version` - the same command docket export uses, so this
// covers the round trip in both directions. A second apply must stay in sync,
// since the image fields are create-time only and must not manufacture drift.
func TestIntegrationServiceCreatePinnedImage(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceName := "docket-test-pinned"
	serviceType := "redis"
	image := "redis"
	imageVersion := "7.2.5"

	destroyService(testCtx(), serviceType, serviceName)
	t.Cleanup(func() { destroyService(testCtx(), serviceType, serviceName) })

	task := ServiceCreateTask{
		Service:      serviceType,
		Name:         serviceName,
		Image:        image,
		ImageVersion: imageVersion,
		State:        StatePresent,
	}
	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to create pinned service: %v\n  commands: %v\n  stderr: %s", result.Error, result.Commands, result.Stderr)
	}
	if !result.Changed || result.State != StatePresent {
		t.Fatalf("expected a changed, present service, got changed=%v state=%q", result.Changed, result.State)
	}

	gotImage, gotVersion, err := serviceImage(testCtx(), serviceType, serviceName)
	if err != nil {
		t.Fatalf("serviceImage: %v", err)
	}
	if gotImage != image || gotVersion != imageVersion {
		t.Errorf("service is running %q:%q, want %q:%q", gotImage, gotVersion, image, imageVersion)
	}

	if plan := task.Plan(testCtx()); !plan.InSync {
		t.Errorf("expected the pinned service to be in sync on re-plan, got %+v", plan)
	}
}

// The image pair the drift tests move a service between. 7.2.5 is the tag
// TestIntegrationServiceCreatePinnedImage and the documented examples already
// pull, so the suite adds at most one image to what CI fetches anyway.
const (
	driftImage   = "redis"
	driftOldTag  = "7.2.4"
	driftNewTag  = "7.2.5"
	driftService = "redis"
)

// createDriftService stands up a redis service on the given tag and registers
// its teardown. Each test uses its own name so a sharded CI run cannot have two
// of them fighting over one service.
func createDriftService(t *testing.T, name, tag string) {
	t.Helper()
	destroyService(testCtx(), driftService, name)
	t.Cleanup(func() { destroyService(testCtx(), driftService, name) })

	task := ServiceCreateTask{
		Service:      driftService,
		Name:         name,
		Image:        driftImage,
		ImageVersion: tag,
		State:        StatePresent,
	}
	if result := task.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to create %s:%s: %v\n  commands: %v\n  stderr: %s", driftImage, tag, result.Error, result.Commands, result.Stderr)
	}
}

// TestIntegrationServiceCreateImageDriftWarns pins the default: a service on
// the wrong image is reported and left alone, so apply stays idempotent.
func TestIntegrationServiceCreateImageDriftWarns(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceName := "docket-test-drift-warn"
	createDriftService(t, serviceName, driftNewTag)

	task := ServiceCreateTask{
		Service:      driftService,
		Name:         serviceName,
		Image:        driftImage,
		ImageVersion: driftOldTag,
		State:        StatePresent,
	}
	plan := task.Plan(testCtx())
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Errorf("warn mode must stay in sync, got InSync=%v status=%q", plan.InSync, plan.Status)
	}
	if len(plan.Warnings) != 1 || plan.Warnings[0].Reason != WarnReasonServiceImageDrift {
		t.Fatalf("expected one %s warning, got %v", WarnReasonServiceImageDrift, plan.Warnings)
	}
	if result := task.Execute(testCtx()); result.Error != nil || result.Changed {
		t.Errorf("warn mode must not change the server, got changed=%v err=%v", result.Changed, result.Error)
	}
}

// TestIntegrationServiceCreateImageDriftErrors covers the CI-gate mode.
func TestIntegrationServiceCreateImageDriftErrors(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceName := "docket-test-drift-error"
	createDriftService(t, serviceName, driftNewTag)

	task := ServiceCreateTask{
		Service:      driftService,
		Name:         serviceName,
		Image:        driftImage,
		ImageVersion: driftOldTag,
		ImageDrift:   imageDriftError,
		State:        StatePresent,
	}
	plan := task.Plan(testCtx())
	if plan.Error == nil || plan.Status != PlanStatusError {
		t.Fatalf("expected a plan error, got status=%q err=%v", plan.Status, plan.Error)
	}
	if !strings.Contains(plan.Error.Error(), driftNewTag) || !strings.Contains(plan.Error.Error(), driftOldTag) {
		t.Errorf("error %q should name both the running and the pinned reference", plan.Error.Error())
	}
}

// TestIntegrationServiceCreateImageDriftUpgrades is the convergence test. An
// upgrade that leaves the task reporting drift on the next run would recreate
// the container on every apply, so asserting the re-plan is in sync and a
// second apply changes nothing is the part that matters most here.
func TestIntegrationServiceCreateImageDriftUpgrades(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceName := "docket-test-drift-upgrade"
	createDriftService(t, serviceName, driftOldTag)

	task := ServiceCreateTask{
		Service:      driftService,
		Name:         serviceName,
		Image:        driftImage,
		ImageVersion: driftNewTag,
		ImageDrift:   imageDriftUpgrade,
		State:        StatePresent,
	}

	plan := task.Plan(testCtx())
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("expected drift, got InSync=%v status=%q", plan.InSync, plan.Status)
	}

	result := task.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to upgrade: %v\n  commands: %v\n  stderr: %s", result.Error, result.Commands, result.Stderr)
	}
	if !result.Changed || result.State != StatePresent {
		t.Fatalf("expected a changed, present service, got changed=%v state=%q", result.Changed, result.State)
	}

	image, version, err := serviceImage(testCtx(), driftService, serviceName)
	if err != nil {
		t.Fatalf("serviceImage: %v", err)
	}
	if image != driftImage || version != driftNewTag {
		t.Errorf("service is running %q:%q, want %q:%q", image, version, driftImage, driftNewTag)
	}

	if plan := task.Plan(testCtx()); !plan.InSync || len(plan.Warnings) != 0 {
		t.Errorf("the upgraded service must re-plan in sync and silent, got %+v", plan)
	}
	if second := task.Execute(testCtx()); second.Error != nil || second.Changed {
		t.Errorf("a second apply must be a no-op, got changed=%v err=%v", second.Changed, second.Error)
	}
}
