package tasks

import (
	"testing"
)

func TestIntegrationLetsencryptPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "letsencrypt")

	appName := "docket-test-letsencrypt"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"dns-provider", "cloudflare", true, true},
		// A dns-provider-<ENV> credential has no map entry - its keys are
		// synthesized from the property name - so it exercises the dynamic
		// probe path added in #449. dokku-letsencrypt 0.25.0+ reports it, so
		// it converges like any mapped property.
		{"dns-provider-CLOUDFLARE_API_TOKEN", "token123", true, true},
		{"email", "admin@example.com", true, true},
		{"graceperiod", "2592000", true, true},
		{"lego-args", "--key=value", true, true},
		{"lego-docker-options", "--cpus=1", true, true},
		{"server", "staging", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "letsencrypt per-app " + tc.property,
					setTask:   LetsencryptPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: LetsencryptPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := LetsencryptPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "letsencrypt global " + tc.property,
					setTask:   LetsencryptPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
