package tasks

import (
	"testing"
)

func TestIntegrationBuilderDockerfileProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-dockerfile"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-dockerfile per-app",
		setTask:   BuilderDockerfilePropertyTask{App: appName, Property: "dockerfile-path", Value: "Dockerfile.production", State: StatePresent},
		unsetTask: BuilderDockerfilePropertyTask{App: appName, Property: "dockerfile-path", State: StateAbsent},
	})
}

func TestIntegrationBuilderDockerfilePropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := BuilderDockerfilePropertyTask{Global: true, Property: "dockerfile-path", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "builder-dockerfile global",
		setTask:   BuilderDockerfilePropertyTask{Global: true, Property: "dockerfile-path", Value: "Dockerfile.production", State: StatePresent},
		unsetTask: unsetTask,
	})
}
