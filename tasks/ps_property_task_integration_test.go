package tasks

import (
	"testing"
)

func TestIntegrationPsProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-ps-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// procfile-path goes through dokku's generic property setter and
	// supports unset via the absent state. restart-policy is special-cased
	// on the dokku side and rejects empty values, so it cannot be cleared
	// via `ps:set <app> restart-policy` (no value).
	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "ps per-app",
		setTask:   PsPropertyTask{App: appName, Property: "procfile-path", Value: "Procfile.custom", State: StatePresent},
		unsetTask: PsPropertyTask{App: appName, Property: "procfile-path", State: StateAbsent},
	})
}
