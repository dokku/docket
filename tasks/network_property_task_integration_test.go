package tasks

import (
	"testing"
)

func TestIntegrationNetworkPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-network"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"attach-post-create", "example-net", true, true},
		{"attach-post-deploy", "example-net", true, true},
		{"bind-all-interfaces", "true", true, true},
		{"initial-network", "example-net", true, true},
		{"static-web-listener", "1.2.3.4:5000", true, false},
		{"tld", "example.com", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "network per-app " + tc.property,
					setTask:   NetworkPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: NetworkPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := NetworkPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "network global " + tc.property,
					setTask:   NetworkPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
