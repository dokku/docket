package tasks

import (
	"testing"
)

// traefik has no per-app properties as of dokku 0.38.3 (dokku/dokku#8597);
// every key returns "can only be set globally". Only global coverage applies.
func TestIntegrationTraefikProperty(t *testing.T) {
	t.Skip("traefik has no per-app properties post dokku 0.38.3")
}

// Global tests use presentOnly because traefik's :report exposes
// global-<property> as a computed-style key that returns the default after
// unset (filed as dokku/dokku#8631), so absent re-apply still reports
// Changed=true until the upstream fix lands.
func TestIntegrationTraefikPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := TraefikPropertyTask{Global: true, Property: "image", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:       "traefik global",
		setTask:     TraefikPropertyTask{Global: true, Property: "image", Value: "traefik:v3.2", State: StatePresent},
		unsetTask:   unsetTask,
		presentOnly: true,
	})
}
