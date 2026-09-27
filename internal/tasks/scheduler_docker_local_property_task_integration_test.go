package tasks

import (
	"testing"
)

func TestIntegrationSchedulerDockerLocalPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-scheduler-docker-local"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

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
		t.Run(tc.property+"/global", func(t *testing.T) {
			unsetTask := SchedulerDockerLocalPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
			defer unsetTask.Execute(testCtx())
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "scheduler-docker-local global " + tc.property,
				setTask:   SchedulerDockerLocalPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
				unsetTask: unsetTask,
			})
		})
	}
}
