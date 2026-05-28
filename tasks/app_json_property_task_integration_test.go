package tasks

import (
	"testing"
)

func TestIntegrationAppJsonPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-app-json-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"appjson-path", "apps/web/app.json", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "app-json per-app " + tc.property,
					setTask:   AppJsonPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: AppJsonPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := AppJsonPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "app-json global " + tc.property,
					setTask:   AppJsonPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
