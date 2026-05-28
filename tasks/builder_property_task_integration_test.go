package tasks

import (
	"testing"
)

func TestIntegrationBuilderPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-builder"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"build-dir", "backend", true, true},
		{"selected", "dockerfile", true, true},
		{"skip-cleanup", "true", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder per-app " + tc.property,
					setTask:   BuilderPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: BuilderPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := BuilderPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "builder global " + tc.property,
					setTask:   BuilderPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
