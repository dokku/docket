package tasks

import "testing"

// TestIntegrationServiceExposeIdempotent drives a real dokku service through
// expose, an idempotent re-expose, and unexpose. Redis exposes a single
// container port so a positional reorder cannot be reproduced here (that path
// is covered by the unit tests); this guards that the ordered-slice probe still
// converges end-to-end.
func TestIntegrationServiceExposeIdempotent(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-expose-svc"

	destroyService(serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(); r.Error != nil {
		t.Fatalf("failed to create service: %v", r.Error)
	}
	defer destroyService(serviceType, serviceName)

	exposeTask := ServiceExposeTask{Service: serviceType, Name: serviceName, Ports: []string{"11111"}, State: StatePresent}

	result := exposeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to expose service: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for a new expose")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got %q", result.State)
	}

	result = exposeTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent expose failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false when the service is already exposed on the same ports")
	}

	ports, err := serviceExposedPortList(serviceType, serviceName)
	if err != nil {
		t.Fatalf("failed to read exposed ports: %v", err)
	}
	if len(ports) != 1 || ports[0] != "11111" {
		t.Errorf("expected exposed ports [11111], got %v", ports)
	}

	unexposeTask := ServiceExposeTask{Service: serviceType, Name: serviceName, State: StateAbsent}
	result = unexposeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to unexpose service: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected changed=true for unexpose")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got %q", result.State)
	}

	result = unexposeTask.Execute()
	if result.Error != nil {
		t.Fatalf("idempotent unexpose failed: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected changed=false when the service is already unexposed")
	}
}
