package tasks

import (
	"testing"
)

// SchedulerDockerLocalPropertyTask has no Global field, so only per-app
// coverage applies.
func TestIntegrationSchedulerDockerLocalPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-docker-local"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
	}{
		{"init-process", "false"},
		{"parallel-schedule-count", "5"},
	}
	for _, tc := range cases {
		t.Run(tc.property+"/per-app", func(t *testing.T) {
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-docker-local per-app " + tc.property,
				setTask:   SchedulerDockerLocalPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
				unsetTask: SchedulerDockerLocalPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
			})
		})
	}
}
