package tasks

import (
	"testing"
)

func TestIntegrationCronProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-cron"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "cron per-app",
		setTask:   CronPropertyTask{App: appName, Property: "maintenance", Value: "true", State: StatePresent},
		unsetTask: CronPropertyTask{App: appName, Property: "maintenance", State: StateAbsent},
	})
}

func TestIntegrationCronPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := CronPropertyTask{Global: true, Property: "mailto", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "cron global",
		setTask:   CronPropertyTask{Global: true, Property: "mailto", Value: "ops@example.com", State: StatePresent},
		unsetTask: unsetTask,
	})
}
