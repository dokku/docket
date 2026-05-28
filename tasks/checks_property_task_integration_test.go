package tasks

import (
	"testing"
)

func TestIntegrationChecksPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-checks-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"wait-to-retire", "90", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "checks per-app " + tc.property,
					setTask:   ChecksPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: ChecksPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := ChecksPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "checks global " + tc.property,
					setTask:   ChecksPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
