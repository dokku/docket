package tasks

import (
	"testing"
)

func TestIntegrationOpenrestyPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-openresty"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"access-log-format", "combined", true, false},
		{"access-log-path", "/var/log/nginx/test-access.log", true, false},
		{"allowed-letsencrypt-domains-func-base64", "cmV0dXJuIHRydWUK", false, true},
		{"bind-address-ipv4", "0.0.0.0", true, false},
		{"bind-address-ipv6", "::", true, false},
		{"client-body-timeout", "90s", true, false},
		{"client-header-timeout", "90s", true, false},
		{"client-max-body-size", "10m", true, false},
		{"error-log-path", "/var/log/nginx/test-error.log", true, false},
		{"hsts", "true", true, true},
		{"hsts-include-subdomains", "true", true, false},
		{"hsts-max-age", "31536000", true, false},
		{"hsts-preload", "false", true, false},
		{"image", "dokku/openresty-docker-proxy:0.11.0", false, true},
		{"keepalive-timeout", "60s", true, false},
		{"letsencrypt-email", "admin@example.com", false, true},
		{"letsencrypt-server", "https://acme-staging-v02.api.letsencrypt.org/directory", false, true},
		{"lingering-timeout", "10s", true, false},
		{"log-level", "DEBUG", false, true},
		{"proxy-buffer-size", "8k", true, false},
		{"proxy-buffering", "on", true, false},
		{"proxy-buffers", "16 8k", true, false},
		{"proxy-busy-buffers-size", "16k", true, false},
		{"proxy-connect-timeout", "60s", true, false},
		{"proxy-read-timeout", "120s", true, false},
		{"proxy-send-timeout", "60s", true, false},
		{"send-timeout", "60s", true, false},
		{"underscore-in-headers", "on", true, false},
		{"x-forwarded-for-value", "$proxy_add_x_forwarded_for", true, false},
		{"x-forwarded-port-value", "$server_port", true, false},
		{"x-forwarded-proto-value", "$scheme", true, false},
		{"x-forwarded-ssl", "on", true, false},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "openresty per-app " + tc.property,
					setTask:   OpenrestyPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: OpenrestyPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := OpenrestyPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "openresty global " + tc.property,
					setTask:   OpenrestyPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
