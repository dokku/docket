package tasks

import (
	"testing"
)

func TestIntegrationHaproxyProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	// As of dokku 0.38.3 (dokku/dokku#8597), haproxy:set <app> rejects every
	// property because none of them are stored per-app. Coverage lives in
	// TestIntegrationHaproxyPropertyGlobal.
	t.Skip("haproxy has no per-app properties post dokku 0.38.3")
}

func TestIntegrationHaproxyPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := HaproxyPropertyTask{Global: true, Property: "log-level", State: StateAbsent}
	defer unsetTask.Execute()

	// haproxy's global-<property> key returns the default value after unset
	// rather than empty (filed as dokku/dokku#8631), so the absent re-apply
	// would observe drift. Assert present re-apply only.
	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:       "haproxy global",
		setTask:     HaproxyPropertyTask{Global: true, Property: "log-level", Value: "INFO", State: StatePresent},
		unsetTask:   unsetTask,
		presentOnly: true,
	})
}
