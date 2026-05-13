package tasks

import (
	"testing"
)

func TestIntegrationBuildpacksProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-buildpacks-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "buildpacks per-app",
		setTask:   BuildpacksPropertyTask{App: appName, Property: "stack", Value: "gliderlabs/herokuish:latest", State: StatePresent},
		unsetTask: BuildpacksPropertyTask{App: appName, Property: "stack", State: StateAbsent},
	})
}

func TestIntegrationBuildpacksPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuildpacksPropertyTask{Global: true, Property: "stack", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "buildpacks global",
		setTask:   BuildpacksPropertyTask{Global: true, Property: "stack", Value: "gliderlabs/herokuish:latest", State: StatePresent},
		unsetTask: unsetTask,
	})
}
