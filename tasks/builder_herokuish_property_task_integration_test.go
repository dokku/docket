package tasks

import (
	"testing"
)

func TestIntegrationBuilderHerokuishPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder-herokuish"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"allowed", "false", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-herokuish per-app " + tc.property,
					setTask:   BuilderHerokuishPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: BuilderHerokuishPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := BuilderHerokuishPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder-herokuish global " + tc.property,
					setTask:   BuilderHerokuishPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
