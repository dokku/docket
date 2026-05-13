package tasks

import (
	"testing"
)

func TestIntegrationSchedulerProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "scheduler per-app",
		setTask:   SchedulerPropertyTask{App: appName, Property: "selected", Value: "docker-local", State: StatePresent},
		unsetTask: SchedulerPropertyTask{App: appName, Property: "selected", State: StateAbsent},
	})
}

func TestIntegrationSchedulerPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := SchedulerPropertyTask{Global: true, Property: "selected", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "scheduler global",
		setTask:   SchedulerPropertyTask{Global: true, Property: "selected", Value: "docker-local", State: StatePresent},
		unsetTask: unsetTask,
	})
}
