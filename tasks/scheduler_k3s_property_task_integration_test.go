package tasks

import (
	"testing"
)

func TestIntegrationSchedulerK3sProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-k3s"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "scheduler-k3s per-app",
		setTask:   SchedulerK3sPropertyTask{App: appName, Property: "deploy-timeout", Value: "300s", State: StatePresent},
		unsetTask: SchedulerK3sPropertyTask{App: appName, Property: "deploy-timeout", State: StateAbsent},
	})
}
