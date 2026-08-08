package tasks

import (
	"testing"
)

func TestIntegrationServiceCreateAndDestroy(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceName := "docket-test-service"
	serviceType := "redis"

	// ensure clean state
	destroyService(serviceType, serviceName)

	// create the service
	task := ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}
	result := task.Execute()
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
	result = task.Execute()
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
	result = destroyTask.Execute()
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
	result = destroyTask.Execute()
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

	destroyService(serviceType, serviceName)
	t.Cleanup(func() { destroyService(serviceType, serviceName) })

	task := ServiceCreateTask{
		Service:      serviceType,
		Name:         serviceName,
		Image:        image,
		ImageVersion: imageVersion,
		State:        StatePresent,
	}
	result := task.Execute()
	if result.Error != nil {
		t.Fatalf("failed to create pinned service: %v\n  commands: %v\n  stderr: %s", result.Error, result.Commands, result.Stderr)
	}
	if !result.Changed || result.State != StatePresent {
		t.Fatalf("expected a changed, present service, got changed=%v state=%q", result.Changed, result.State)
	}

	gotImage, gotVersion, err := serviceImage(serviceType, serviceName)
	if err != nil {
		t.Fatalf("serviceImage: %v", err)
	}
	if gotImage != image || gotVersion != imageVersion {
		t.Errorf("service is running %q:%q, want %q:%q", gotImage, gotVersion, image, imageVersion)
	}

	if plan := task.Plan(); !plan.InSync {
		t.Errorf("expected the pinned service to be in sync on re-plan, got %+v", plan)
	}
}
