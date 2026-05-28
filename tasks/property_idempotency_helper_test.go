package tasks

import (
	"testing"
)

// propertyTask is the minimal Task interface used by the idempotency helper.
type propertyTask interface {
	Execute() TaskOutputState
}

// propertyIdempotencyCase describes the inputs to runPropertyIdempotencyTest.
type propertyIdempotencyCase struct {
	label     string
	setTask   propertyTask
	unsetTask propertyTask
}

// runPropertyIdempotencyTest exercises set/re-set/unset/re-unset for a
// property task and asserts Changed=false on the re-runs.
func runPropertyIdempotencyTest(t *testing.T, c propertyIdempotencyCase) {
	t.Helper()

	result := c.setTask.Execute()
	if result.Error != nil {
		t.Fatalf("%s: failed to set: %v", c.label, result.Error)
	}
	if result.State != StatePresent {
		t.Errorf("%s: expected state 'present', got '%s'", c.label, result.State)
	}

	result = c.setTask.Execute()
	if result.Error != nil {
		t.Fatalf("%s: failed to re-apply set: %v", c.label, result.Error)
	}
	if result.Changed {
		t.Errorf("%s: expected re-apply to report Changed=false", c.label)
	}

	result = c.unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("%s: failed to unset: %v", c.label, result.Error)
	}
	if result.State != StateAbsent {
		t.Errorf("%s: expected state 'absent', got '%s'", c.label, result.State)
	}

	result = c.unsetTask.Execute()
	if result.Error != nil {
		t.Fatalf("%s: failed to re-apply unset: %v", c.label, result.Error)
	}
	if result.Changed {
		t.Errorf("%s: expected re-apply absent to report Changed=false", c.label)
	}
}
