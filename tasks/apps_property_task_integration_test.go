package tasks

import (
	"testing"
)

func TestIntegrationAppsPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-apps-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"deploy-source", "git", true, false},
		{"deploy-source-metadata", "https://example.com/repo", true, false},
		{"disable-autocreation", "true", false, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "apps per-app " + tc.property,
					setTask:   AppsPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: AppsPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := AppsPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "apps global " + tc.property,
					setTask:   AppsPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
