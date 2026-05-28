package tasks

import (
	"testing"
)

func TestIntegrationPsPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-ps-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"dockerfile-start-cmd", "./entrypoint.sh", true, false},
		{"procfile-path", "Procfile.web", true, true},
		{"restart-policy", "on-failure:5", true, false},
		{"skip-deploy", "true", true, true},
		{"start-cmd", "npm start", true, false},
		{"stop-timeout-seconds", "45", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "ps per-app " + tc.property,
					setTask:   PsPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: PsPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := PsPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "ps global " + tc.property,
					setTask:   PsPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
