package tasks

import (
	"testing"
)

func TestIntegrationNginxPropertyAll(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-nginx"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	cases := []struct {
		property string
		value    string
		perApp   bool
		global   bool
	}{
		{"access-log-format", "combined", true, true},
		{"access-log-path", "/var/log/nginx/test-access.log", true, true},
		{"bind-address-ipv4", "0.0.0.0", true, true},
		{"bind-address-ipv6", "::", true, true},
		{"client-body-timeout", "90s", true, true},
		{"client-header-timeout", "90s", true, true},
		{"client-max-body-size", "10m", true, true},
		{"disable-custom-config", "true", true, true},
		{"error-log-path", "/var/log/nginx/test-error.log", true, true},
		{"hsts", "true", true, true},
		{"hsts-include-subdomains", "true", true, true},
		{"hsts-max-age", "31536000", true, true},
		{"hsts-preload", "false", true, true},
		{"keepalive-timeout", "60s", true, true},
		{"lingering-timeout", "10s", true, true},
		{"nginx-conf-sigil-path", "nginx.conf.sigil", true, true},
		{"nginx-service-command", "/usr/sbin/nginx -g 'daemon off;'", true, true},
		{"proxy-buffer-size", "8k", true, true},
		{"proxy-buffering", "on", true, true},
		{"proxy-buffers", "16 8k", true, true},
		{"proxy-busy-buffers-size", "16k", true, true},
		{"proxy-connect-timeout", "60s", true, true},
		{"proxy-keepalive", "100", true, true},
		{"proxy-read-timeout", "120s", true, true},
		{"proxy-send-timeout", "60s", true, true},
		{"send-timeout", "60s", true, true},
		{"underscore-in-headers", "on", true, true},
		{"x-forwarded-for-value", "$proxy_add_x_forwarded_for", true, true},
		{"x-forwarded-port-value", "$server_port", true, true},
		{"x-forwarded-proto-value", "$scheme", true, true},
		{"x-forwarded-ssl", "on", true, true},
	}
	for _, tc := range cases {
		if tc.perApp {
			t.Run(tc.property+"/per-app", func(t *testing.T) {
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "nginx per-app " + tc.property,
					setTask:   NginxPropertyTask{App: appName, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: NginxPropertyTask{App: appName, Property: tc.property, State: StateAbsent},
				})
			})
		}
		if tc.global {
			t.Run(tc.property+"/global", func(t *testing.T) {
				unsetTask := NginxPropertyTask{Global: true, Property: tc.property, State: StateAbsent}
				defer unsetTask.Execute()
				runPropertyIdempotencyTest(t, propertyIdempotencyCase{
					label:     "nginx global " + tc.property,
					setTask:   NginxPropertyTask{Global: true, Property: tc.property, Value: tc.value, State: StatePresent},
					unsetTask: unsetTask,
				})
			})
		}
	}
}
