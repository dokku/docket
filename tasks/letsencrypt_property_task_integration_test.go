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

	// dns-provider-* keys are dynamic (per-credential env var names) and
	// have no probe key, so the task always reports Changed=true. Exercise
	// set/unset without the idempotency assertion to confirm the dispatch
	// path through isDynamicProperty works.
	t.Run("dns-provider-CLOUDFLARE_API_TOKEN/dynamic", func(t *testing.T) {
		set := LetsencryptPropertyTask{App: appName, Property: "dns-provider-CLOUDFLARE_API_TOKEN", Value: "token123", State: StatePresent}
		if r := set.Execute(); r.Error != nil {
			t.Fatalf("set dynamic dns-provider key: %v", r.Error)
		}
		unset := LetsencryptPropertyTask{App: appName, Property: "dns-provider-CLOUDFLARE_API_TOKEN", State: StateAbsent}
		if r := unset.Execute(); r.Error != nil {
			t.Fatalf("unset dynamic dns-provider key: %v", r.Error)
		}
	})
}
