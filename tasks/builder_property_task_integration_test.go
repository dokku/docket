package tasks

import (
	"testing"
)

func TestIntegrationBuilderProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder per-app",
		setTask:   BuilderPropertyTask{App: appName, Property: "selected", Value: "dockerfile", State: StatePresent},
		unsetTask: BuilderPropertyTask{App: appName, Property: "selected", State: StateAbsent},
	})
}

func TestIntegrationBuilderPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderPropertyTask{Global: true, Property: "selected", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder global",
		setTask:   BuilderPropertyTask{Global: true, Property: "selected", Value: "herokuish", State: StatePresent},
		unsetTask: unsetTask,
	})
}
