package tasks

import (
	"testing"
)

func TestIntegrationBuilderDockerfilePropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-dockerfile"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"dockerfile-path", "Dockerfile.prod", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-dockerfile per-app " + tc.property,
					setTask:   BuilderDockerfilePropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: BuilderDockerfilePropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := BuilderDockerfilePropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-dockerfile global " + tc.property,
					setTask:   BuilderDockerfilePropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
