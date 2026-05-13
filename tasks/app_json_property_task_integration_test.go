package tasks

import (
	"testing"
)

func TestIntegrationAppJsonProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-app-json-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "app-json per-app",
		setTask:   AppJsonPropertyTask{App: appName, Property: "appjson-path", Value: "app.json", State: StatePresent},
		unsetTask: AppJsonPropertyTask{App: appName, Property: "appjson-path", State: StateAbsent},
	})
}

func TestIntegrationAppJsonPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := AppJsonPropertyTask{Global: true, Property: "appjson-path", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "app-json global",
		setTask:   AppJsonPropertyTask{Global: true, Property: "appjson-path", Value: "app.json", State: StatePresent},
		unsetTask: unsetTask,
	})
}
