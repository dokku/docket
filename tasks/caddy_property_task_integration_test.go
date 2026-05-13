package tasks

import (
	"testing"
)

func TestIntegrationCaddyProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-caddy"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "caddy per-app",
		setTask:   CaddyPropertyTask{App: appName, Property: "tls-internal", Value: "true", State: StatePresent},
		unsetTask: CaddyPropertyTask{App: appName, Property: "tls-internal", State: StateAbsent},
	})
}

func TestIntegrationCaddyPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := CaddyPropertyTask{Global: true, Property: "tls-internal", State: StateAbsent}
	defer unsetTask.Execute()

	// caddy's global-<property> key returns the default value after unset
	// rather than empty (filed as dokku/dokku#8631), so the absent re-apply
	// would observe drift. Assert present re-apply only.
	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:       "caddy global",
		setTask:     CaddyPropertyTask{Global: true, Property: "tls-internal", Value: "true", State: StatePresent},
		unsetTask:   unsetTask,
		presentOnly: true,
	})
}
