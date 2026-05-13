package tasks

import (
	"testing"
)

// SchedulerDockerLocalPropertyTask has no Global field, so only per-app
// coverage applies.
func TestIntegrationSchedulerDockerLocalProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-docker-local"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "scheduler-docker-local per-app",
		setTask:   SchedulerDockerLocalPropertyTask{App: appName, Property: "init-process", Value: "true", State: StatePresent},
		unsetTask: SchedulerDockerLocalPropertyTask{App: appName, Property: "init-process", State: StateAbsent},
	})
}
