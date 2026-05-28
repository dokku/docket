package tasks

import (
	"testing"
)

func TestIntegrationGitPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-git-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"archive-max-files", "5000", false, true},
		{"archive-max-size", "100000000", false, true},
		{"deploy-branch", "main", true, true},
		{"keep-git-dir", "true", true, true},
		{"rev-env-var", "COMMIT_SHA", true, false},
		{"source-image", "dokku/source:latest", true, false},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "git per-app " + tc.property,
					setTask:   GitPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: GitPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := GitPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "git global " + tc.property,
					setTask:   GitPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
