package tasks

import (
	"testing"
)

func TestIntegrationCaddyPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-caddy"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"image", "lucaslorentz/caddy-docker-proxy:2.12", false, true},
		{"letsencrypt-email", "admin@example.com", false, true},
		{"letsencrypt-server", "https://acme-staging-v02.api.letsencrypt.org/directory", false, true},
		{"log-level", "INFO", false, true},
		{"polling-interval", "10s", false, true},
		{"tls-internal", "true", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "caddy per-app " + tc.property,
					setTask:   CaddyPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: CaddyPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := CaddyPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "caddy global " + tc.property,
					setTask:   CaddyPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
