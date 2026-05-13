package tasks

import (
	"testing"
)

func TestIntegrationRegistryProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-registry"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "registry per-app",
		setTask:   RegistryPropertyTask{App: appName, Property: "image-repo", Value: "registry.example.com/" + appName, State: StatePresent},
		unsetTask: RegistryPropertyTask{App: appName, Property: "image-repo", State: StateAbsent},
	})
}

func TestIntegrationRegistryPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := RegistryPropertyTask{Global: true, Property: "server", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "registry global",
		setTask:   RegistryPropertyTask{Global: true, Property: "server", Value: "registry.example.com", State: StatePresent},
		unsetTask: unsetTask,
	})
}
