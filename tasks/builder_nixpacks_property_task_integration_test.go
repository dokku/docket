package tasks

import (
	"testing"
)

func TestIntegrationBuilderNixpacksProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-nixpacks"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-nixpacks per-app",
		setTask:   BuilderNixpacksPropertyTask{App: appName, Property: "nixpackstoml-path", Value: "config/nixpacks.toml", State: StatePresent},
		unsetTask: BuilderNixpacksPropertyTask{App: appName, Property: "nixpackstoml-path", State: StateAbsent},
	})
}

func TestIntegrationBuilderNixpacksPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderNixpacksPropertyTask{Global: true, Property: "nixpackstoml-path", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-nixpacks global",
		setTask:   BuilderNixpacksPropertyTask{Global: true, Property: "nixpackstoml-path", Value: "config/nixpacks.toml", State: StatePresent},
		unsetTask: unsetTask,
	})
}
