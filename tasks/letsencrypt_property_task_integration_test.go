package tasks

import (
	"testing"
)

func TestIntegrationLetsencryptProperty(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "letsencrypt")

	appName := "docket-test-letsencrypt"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "letsencrypt per-app",
		setTask:   LetsencryptPropertyTask{App: appName, Property: "email", Value: "admin@example.com", State: StatePresent},
		unsetTask: LetsencryptPropertyTask{App: appName, Property: "email", State: StateAbsent},
	})
}

func TestIntegrationLetsencryptPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "letsencrypt")

	unsetTask := LetsencryptPropertyTask{Global: true, Property: "email", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "letsencrypt global",
		setTask:   LetsencryptPropertyTask{Global: true, Property: "email", Value: "admin@example.com", State: StatePresent},
		unsetTask: unsetTask,
	})
}
