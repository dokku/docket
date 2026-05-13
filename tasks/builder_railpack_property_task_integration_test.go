package tasks

import (
	"testing"
)

func TestIntegrationBuilderRailpackProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-railpack"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-railpack per-app",
		setTask:   BuilderRailpackPropertyTask{App: appName, Property: "railpackjson-path", Value: "config/railpack.json", State: StatePresent},
		unsetTask: BuilderRailpackPropertyTask{App: appName, Property: "railpackjson-path", State: StateAbsent},
	})
}

func TestIntegrationBuilderRailpackPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderRailpackPropertyTask{Global: true, Property: "railpackjson-path", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-railpack global",
		setTask:   BuilderRailpackPropertyTask{Global: true, Property: "railpackjson-path", Value: "config/railpack.json", State: StatePresent},
		unsetTask: unsetTask,
	})
}
