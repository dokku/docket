package tasks

import (
	"testing"
)

func TestIntegrationNetworkProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-network"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "network per-app",
		setTask:   NetworkPropertyTask{App: appName, Property: "bind-all-interfaces", Value: "true", State: StatePresent},
		unsetTask: NetworkPropertyTask{App: appName, Property: "bind-all-interfaces", State: StateAbsent},
	})
}

func TestIntegrationNetworkPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := NetworkPropertyTask{Global: true, Property: "bind-all-interfaces", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "network global",
		setTask:   NetworkPropertyTask{Global: true, Property: "bind-all-interfaces", Value: "true", State: StatePresent},
		unsetTask: unsetTask,
	})
}
