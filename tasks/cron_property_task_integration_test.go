package tasks

import (
	"testing"
)

func TestIntegrationCronPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-cron"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"maintenance", "true", true, true},
		{"mailfrom", "cron@example.com", false, true},
		{"mailto", "ops@example.com", false, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "cron per-app " + tc.property,
					setTask:   CronPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: CronPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := CronPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "cron global " + tc.property,
					setTask:   CronPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
