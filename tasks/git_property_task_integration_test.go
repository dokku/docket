package tasks

import (
	"testing"
)

func TestIntegrationGitProperty(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-git-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "git per-app",
		setTask:   GitPropertyTask{App: appName, Property: "deploy-branch", Value: "main", State: StatePresent},
		unsetTask: GitPropertyTask{App: appName, Property: "deploy-branch", State: StateAbsent},
	})
}

func TestIntegrationGitPropertyGlobal(t *testing.T) {
	skipIfNoDokkuT(t)

	unsetTask := GitPropertyTask{Global: true, Property: "keep-git-dir", State: StateAbsent}
	defer unsetTask.Execute()

	runPropertyIdempotencyTest(t, propertyIdempotencyCase{
		label:     "git global",
		setTask:   GitPropertyTask{Global: true, Property: "keep-git-dir", Value: "true", State: StatePresent},
		unsetTask: unsetTask,
	})
}
