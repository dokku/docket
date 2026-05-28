package tasks

import (
	"testing"
)

func TestIntegrationSchedulerPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"selected", "docker-local", true, true},
		{"shell", "bash", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "scheduler per-app " + tc.property,
					setTask:   SchedulerPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: SchedulerPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := SchedulerPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "scheduler global " + tc.property,
					setTask:   SchedulerPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
