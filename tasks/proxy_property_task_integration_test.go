package tasks

import (
	"testing"
)

func TestIntegrationProxyPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-proxy-prop"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"type", "caddy", true, true},
		{"proxy-port", "8080", true, true},
		{"proxy-ssl-port", "8443", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "proxy per-app " + tc.property,
					setTask:   ProxyPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: ProxyPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := ProxyPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "proxy global " + tc.property,
					setTask:   ProxyPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
