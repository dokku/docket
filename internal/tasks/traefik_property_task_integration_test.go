package tasks

import (
	"testing"
)

// All traefik properties are global-only.
func TestIntegrationTraefikPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	cases := []struct {
		property string
		value    string
	}{
		{"api-enabled", "true"},
		{"api-entry-point", "traefik"},
		{"api-entry-point-address", ":8080"},
		{"api-vhost", "traefik.dokku.me"},
		{"basic-auth-password", "secret"},
		{"basic-auth-username", "admin"},
		{"challenge-mode", "tls"},
		{"dashboard-enabled", "true"},
		{"dns-provider", "cloudflare"},
		// A dns-provider-<ENV> credential has no map entry - its global key is
		// synthesized from the property name - so it exercises the dynamic
		// probe path added in #450. dokku 0.38.27+ reports it, so it converges
		// like any mapped property.
		{"dns-provider-CLOUDFLARE_API_TOKEN", "token123"},
		{"http-entry-point", "http"},
		{"https-entry-point", "https"},
		{"image", "traefik:v3.7.1"},
		{"letsencrypt-email", "admin@example.com"},
		{"letsencrypt-server", "https://acme-staging-v02.api.letsencrypt.org/directory"},
		{"log-level", "INFO"},
	}
	for _, tc := range cases {
		t.Run(tc.property+"/global", func(t *testing.T) {
			unsetTask := TraefikPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
			defer unsetTask.Execute(testCtx())
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "traefik global " + tc.property,
				setTask:   TraefikPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
				unsetTask: unsetTask,
			})
		})
	}
}
