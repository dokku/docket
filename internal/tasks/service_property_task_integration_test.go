package tasks

import "testing"

// TestIntegrationServiceProperty drives real service properties through set,
// an in-sync re-plan and a clear, including a backup credential that info
// reports only as a fingerprint.
func TestIntegrationServiceProperty(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-property"

	destroyService(testCtx(), serviceType, serviceName)
	t.Cleanup(func() { destroyService(testCtx(), serviceType, serviceName) })
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create service: %v", r.Error)
	}

	converge := func(t *testing.T, task ServicePropertyTask) {
		t.Helper()
		result := task.Execute(testCtx())
		if result.Error != nil {
			t.Fatalf("failed to apply %s (%s): %v\n  commands: %v\n  stderr: %s", task.Property, task.State, result.Error, result.Commands, result.Stderr)
		}
		if !result.Changed {
			t.Errorf("expected %s (%s) to change the service", task.Property, task.State)
		}
		if plan := task.Plan(testCtx()); !plan.InSync || len(plan.Warnings) != 0 {
			t.Errorf("expected %s (%s) to re-plan in sync and silent, got %+v", task.Property, task.State, plan)
		}
		if second := task.Execute(testCtx()); second.Error != nil || second.Changed {
			t.Errorf("a second apply of %s (%s) must be a no-op, got changed=%v err=%v", task.Property, task.State, second.Changed, second.Error)
		}
	}

	t.Run("restart-policy", func(t *testing.T) {
		converge(t, ServicePropertyTask{Service: serviceType, Name: serviceName, Property: "restart-policy", Value: "always", State: StatePresent})
		converge(t, ServicePropertyTask{Service: serviceType, Name: serviceName, Property: "restart-policy", State: StateAbsent})
	})

	t.Run("backup-access-key-id", func(t *testing.T) {
		converge(t, ServicePropertyTask{Service: serviceType, Name: serviceName, Property: "backup-access-key-id", Value: "AKIADOCKETTEST1", State: StatePresent})

		other := ServicePropertyTask{Service: serviceType, Name: serviceName, Property: "backup-access-key-id", Value: "AKIADOCKETTEST2", State: StatePresent}
		if plan := other.Plan(testCtx()); plan.Error != nil || plan.InSync || plan.Status != PlanStatusModify {
			t.Errorf("a different key id must drift, got InSync=%v status=%q err=%v", plan.InSync, plan.Status, plan.Error)
		}

		converge(t, ServicePropertyTask{Service: serviceType, Name: serviceName, Property: "backup-access-key-id", State: StateAbsent})
	})
}
