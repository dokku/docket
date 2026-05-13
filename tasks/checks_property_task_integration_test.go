package tasks

import (
	"testing"
)

func TestIntegrationChecksProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-checks-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "checks per-app",
		setTask:   ChecksPropertyTask{App: appName, Property: "wait-to-retire", Value: "60", State: StatePresent},
		unsetTask: ChecksPropertyTask{App: appName, Property: "wait-to-retire", State: StateAbsent},
	})
}

func TestIntegrationChecksPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := ChecksPropertyTask{Global: true, Property: "wait-to-retire", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "checks global",
		setTask:   ChecksPropertyTask{Global: true, Property: "wait-to-retire", Value: "60", State: StatePresent},
		unsetTask: unsetTask,
	})
}
