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
			defer unsetTask.Execute()
			runPropertyIdempotencyTest(t, propertyIdempotencyCase{
				label:     "traefik global " + tc.property,
				setTask:   TraefikPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
				unsetTask: unsetTask,
			})
		})
	}

	// dns-provider-<ENV> are dynamic; exercise one to confirm the
	// isDynamicProperty fallback path.
	t.Run("dns-provider-CLOUDFLARE_API_TOKEN/dynamic", func(t *testing.T) {
		set := TraefikPropertyTask{Global: true, Property: "dns-provider-CLOUDFLARE_API_TOKEN", Value: "token123", State: StatePresent}
		if r := set.Execute(); r.Error != nil {
			t.Fatalf("set dynamic dns-provider key: %v", r.Error)
		}
		unset := TraefikPropertyTask{Global: true, Property: "dns-provider-CLOUDFLARE_API_TOKEN", State: StateAbsent}
		if r := unset.Execute(); r.Error != nil {
			t.Fatalf("unset dynamic dns-provider key: %v", r.Error)
		}
	})
}
