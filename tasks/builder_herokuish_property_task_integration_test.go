package tasks

import (
	"testing"
)

func TestIntegrationBuilderHerokuishProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-herokuish"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-herokuish per-app",
		setTask:   BuilderHerokuishPropertyTask{App: appName, Property: "allowed", Value: "true", State: StatePresent},
		unsetTask: BuilderHerokuishPropertyTask{App: appName, Property: "allowed", State: StateAbsent},
	})
}

func TestIntegrationBuilderHerokuishPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderHerokuishPropertyTask{Global: true, Property: "allowed", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-herokuish global",
		setTask:   BuilderHerokuishPropertyTask{Global: true, Property: "allowed", Value: "true", State: StatePresent},
		unsetTask: unsetTask,
	})
}
