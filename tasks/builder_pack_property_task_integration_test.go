package tasks

import (
	"testing"
)

func TestIntegrationBuilderPackProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-pack"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-pack per-app",
		setTask:   BuilderPackPropertyTask{App: appName, Property: "projecttoml-path", Value: "config/project.toml", State: StatePresent},
		unsetTask: BuilderPackPropertyTask{App: appName, Property: "projecttoml-path", State: StateAbsent},
	})
}

func TestIntegrationBuilderPackPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderPackPropertyTask{Global: true, Property: "projecttoml-path", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-pack global",
		setTask:   BuilderPackPropertyTask{Global: true, Property: "projecttoml-path", Value: "config/project.toml", State: StatePresent},
		unsetTask: unsetTask,
	})
}
