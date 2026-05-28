package tasks

import (
	"strings"
	"testing"
)

// propertyKeysCase declares the expected (PerApp, Global) values for one
// property in a per-plugin property-keys map.
type propertyKeysCase struct {
	property string
	perApp   string
	global   string
}

func checkPropertyKeys(t *testing.T, plugin string, keys map[string]PropertyKeys, cases []propertyKeysCase) {
	t.Helper()
	for _, tc := range cases {
		got, ok := keys[tc.property]
		if !ok {
			t.Errorf("%s: missing property %q in map", plugin, tc.property)
			continue
		}
		if got.PerApp != tc.perApp {
			t.Errorf("%s[%q].PerApp = %q; want %q", plugin, tc.property, got.PerApp, tc.perApp)
		}
		if got.Global != tc.global {
			t.Errorf("%s[%q].Global = %q; want %q", plugin, tc.property, got.Global, tc.global)
		}
	}
	if len(keys) != len(cases) {
		t.Errorf("%s: map has %d entries; want %d (extra entries in the map are not asserted)", plugin, len(keys), len(cases))
	}
}

// checkUnsupportedProperty verifies validateProperty rejects a property not
// in the plugin's map.
func checkUnsupportedProperty(t *testing.T, plugin string, keys map[string]PropertyKeys) {
	t.Helper()
	err := validateProperty(plugin, "definitely-not-a-real-property", false, keys)
	if err == nil {
		t.Errorf("%s: validateProperty should reject unsupported property", plugin)
		return
	}
	if !strings.Contains(err.Error(), "unsupported property") {
		t.Errorf("%s: unexpected error: %v", plugin, err)
	}
}

// checkScopeMismatch verifies validateProperty returns the right errors when
// the user asks for a scope the property doesn't support.
func checkScopeMismatch(t *testing.T, plugin string, keys map[string]PropertyKeys, perAppOnly, globalOnly string) {
	t.Helper()
	if perAppOnly != "" {
		err := validateProperty(plugin, perAppOnly, true, keys)
		if err == nil || !strings.Contains(err.Error(), "no global form") {
			t.Errorf("%s: expected 'no global form' for %q in global scope, got %v", plugin, perAppOnly, err)
		}
	}
	if globalOnly != "" {
		err := validateProperty(plugin, globalOnly, false, keys)
		if err == nil || !strings.Contains(err.Error(), "no per-app form") {
			t.Errorf("%s: expected 'no per-app form' for %q in per-app scope, got %v", plugin, globalOnly, err)
		}
	}
}

func TestAppJsonPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "app-json", appJsonPropertyKeys, []propertyKeysCase{
		{"appjson-path", "appjson-path", "global-appjson-path"},
	})
	checkUnsupportedProperty(t, "app-json", appJsonPropertyKeys)
}

func TestBuilderPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder", builderPropertyKeys, []propertyKeysCase{
		{"build-dir", "build-dir", "global-build-dir"},
		{"selected", "selected", "global-selected"},
		{"skip-cleanup", "skip-cleanup", "global-skip-cleanup"},
	})
	checkUnsupportedProperty(t, "builder", builderPropertyKeys)
}

func TestBuilderDockerfilePropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder-dockerfile", builderDockerfilePropertyKeys, []propertyKeysCase{
		{"dockerfile-path", "dockerfile-path", "global-dockerfile-path"},
	})
	checkUnsupportedProperty(t, "builder-dockerfile", builderDockerfilePropertyKeys)
}

func TestBuilderHerokuishPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder-herokuish", builderHerokuishPropertyKeys, []propertyKeysCase{
		{"allowed", "allowed", "global-allowed"},
	})
	checkUnsupportedProperty(t, "builder-herokuish", builderHerokuishPropertyKeys)
}

func TestBuilderLambdaPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder-lambda", builderLambdaPropertyKeys, []propertyKeysCase{
		{"lambdayml-path", "lambdayml-path", "global-lambdayml-path"},
	})
	checkUnsupportedProperty(t, "builder-lambda", builderLambdaPropertyKeys)
}

func TestBuilderNixpacksPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder-nixpacks", builderNixpacksPropertyKeys, []propertyKeysCase{
		{"nixpackstoml-path", "nixpackstoml-path", "global-nixpackstoml-path"},
	})
	checkUnsupportedProperty(t, "builder-nixpacks", builderNixpacksPropertyKeys)
}

func TestBuilderPackPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder-pack", builderPackPropertyKeys, []propertyKeysCase{
		{"projecttoml-path", "projecttoml-path", "global-projecttoml-path"},
	})
	checkUnsupportedProperty(t, "builder-pack", builderPackPropertyKeys)
}

func TestBuilderRailpackPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builder-railpack", builderRailpackPropertyKeys, []propertyKeysCase{
		{"railpackjson-path", "railpackjson-path", "global-railpackjson-path"},
	})
	checkUnsupportedProperty(t, "builder-railpack", builderRailpackPropertyKeys)
}

func TestBuildpacksPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "buildpacks", buildpacksPropertyKeys, []propertyKeysCase{
		{"stack", "stack", "global-stack"},
	})
	checkUnsupportedProperty(t, "buildpacks", buildpacksPropertyKeys)
}

func TestBuildsPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "builds", buildsPropertyKeys, []propertyKeysCase{
		{"retention", "retention", "global-retention"},
	})
	checkUnsupportedProperty(t, "builds", buildsPropertyKeys)
}

func TestCaddyPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "caddy", caddyPropertyKeys, []propertyKeysCase{
		{"image", "", "global-image"},
		{"letsencrypt-email", "", "global-letsencrypt-email"},
		{"letsencrypt-server", "", "global-letsencrypt-server"},
		{"log-level", "", "global-log-level"},
		{"polling-interval", "", "global-polling-interval"},
		{"tls-internal", "tls-internal", "global-tls-internal"},
	})
	checkUnsupportedProperty(t, "caddy", caddyPropertyKeys)
	checkScopeMismatch(t, "caddy", caddyPropertyKeys, "", "image")
}

func TestChecksPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "checks", checksPropertyKeys, []propertyKeysCase{
		{"wait-to-retire", "wait-to-retire", "global-wait-to-retire"},
	})
	checkUnsupportedProperty(t, "checks", checksPropertyKeys)
}

func TestCronPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "cron", cronPropertyKeys, []propertyKeysCase{
		{"maintenance", "maintenance", "global-maintenance"},
		{"mailfrom", "", "global-mailfrom"},
		{"mailto", "", "global-mailto"},
	})
	checkUnsupportedProperty(t, "cron", cronPropertyKeys)
	checkScopeMismatch(t, "cron", cronPropertyKeys, "", "mailto")
}

func TestGitPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "git", gitPropertyKeys, []propertyKeysCase{
		{"archive-max-files", "", "global-archive-max-files"},
		{"archive-max-size", "", "global-archive-max-size"},
		{"deploy-branch", "deploy-branch", "global-deploy-branch"},
		{"keep-git-dir", "keep-git-dir", "global-keep-git-dir"},
		{"rev-env-var", "rev-env-var", ""},
		{"source-image", "source-image", ""},
	})
	checkUnsupportedProperty(t, "git", gitPropertyKeys)
	// archive-max-files is global-only; rev-env-var is per-app-only.
	checkScopeMismatch(t, "git", gitPropertyKeys, "rev-env-var", "archive-max-files")
}

func TestHaproxyPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "haproxy", haproxyPropertyKeys, []propertyKeysCase{
		{"image", "", "global-image"},
		{"letsencrypt-email", "", "global-letsencrypt-email"},
		{"letsencrypt-server", "", "global-letsencrypt-server"},
		{"log-level", "", "global-log-level"},
		{"refresh-conf", "", "global-refresh-conf"},
	})
	checkUnsupportedProperty(t, "haproxy", haproxyPropertyKeys)
	checkScopeMismatch(t, "haproxy", haproxyPropertyKeys, "", "image")
}

func TestLetsencryptPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "letsencrypt", letsencryptPropertyKeys, []propertyKeysCase{
		{"dns-provider", "dns-provider", "global-dns-provider"},
		{"email", "email", "global-email"},
		{"graceperiod", "graceperiod", "global-graceperiod"},
		{"lego-args", "lego-args", "global-lego-args"},
		{"lego-docker-options", "lego-docker-options", "global-lego-docker-options"},
		{"server", "server", "global-server"},
	})
	checkUnsupportedProperty(t, "letsencrypt", letsencryptPropertyKeys)
	// dns-provider-* are dynamic and should bypass map validation.
	if err := validateProperty("letsencrypt", "dns-provider-X", false, letsencryptPropertyKeys); err != nil {
		t.Errorf("dynamic property should pass validation, got %v", err)
	}
}

func TestLogsPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "logs", logsPropertyKeys, []propertyKeysCase{
		{"app-label-alias", "app-label-alias", "global-app-label-alias"},
		{"max-size", "max-size", "global-max-size"},
		{"vector-image", "", "global-vector-image"},
		{"vector-networks", "", "global-vector-networks"},
		{"vector-sink", "vector-sink", "global-vector-sink"},
	})
	checkUnsupportedProperty(t, "logs", logsPropertyKeys)
	checkScopeMismatch(t, "logs", logsPropertyKeys, "", "vector-image")
}

func TestNetworkPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "network", networkPropertyKeys, []propertyKeysCase{
		{"attach-post-create", "attach-post-create", "global-attach-post-create"},
		{"attach-post-deploy", "attach-post-deploy", "global-attach-post-deploy"},
		{"bind-all-interfaces", "bind-all-interfaces", "global-bind-all-interfaces"},
		{"initial-network", "initial-network", "global-initial-network"},
		{"static-web-listener", "static-web-listener", ""},
		{"tld", "tld", "global-tld"},
	})
	checkUnsupportedProperty(t, "network", networkPropertyKeys)
	checkScopeMismatch(t, "network", networkPropertyKeys, "static-web-listener", "")
}

func TestNginxPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "nginx", nginxPropertyKeys, []propertyKeysCase{
		{"access-log-format", "access-log-format", "global-access-log-format"},
		{"access-log-path", "access-log-path", "global-access-log-path"},
		{"bind-address-ipv4", "bind-address-ipv4", "global-bind-address-ipv4"},
		{"bind-address-ipv6", "bind-address-ipv6", "global-bind-address-ipv6"},
		{"client-body-timeout", "client-body-timeout", "global-client-body-timeout"},
		{"client-header-timeout", "client-header-timeout", "global-client-header-timeout"},
		{"client-max-body-size", "client-max-body-size", "global-client-max-body-size"},
		{"disable-custom-config", "disable-custom-config", "global-disable-custom-config"},
		{"error-log-path", "error-log-path", "global-error-log-path"},
		{"hsts", "hsts", "global-hsts"},
		{"hsts-include-subdomains", "hsts-include-subdomains", "global-hsts-include-subdomains"},
		{"hsts-max-age", "hsts-max-age", "global-hsts-max-age"},
		{"hsts-preload", "hsts-preload", "global-hsts-preload"},
		{"keepalive-timeout", "keepalive-timeout", "global-keepalive-timeout"},
		{"lingering-timeout", "lingering-timeout", "global-lingering-timeout"},
		{"nginx-conf-sigil-path", "nginx-conf-sigil-path", "global-nginx-conf-sigil-path"},
		{"nginx-service-command", "nginx-service-command", "global-nginx-service-command"},
		{"proxy-buffer-size", "proxy-buffer-size", "global-proxy-buffer-size"},
		{"proxy-buffering", "proxy-buffering", "global-proxy-buffering"},
		{"proxy-buffers", "proxy-buffers", "global-proxy-buffers"},
		{"proxy-busy-buffers-size", "proxy-busy-buffers-size", "global-proxy-busy-buffers-size"},
		{"proxy-connect-timeout", "proxy-connect-timeout", "global-proxy-connect-timeout"},
		{"proxy-keepalive", "proxy-keepalive", "global-proxy-keepalive"},
		{"proxy-read-timeout", "proxy-read-timeout", "global-proxy-read-timeout"},
		{"proxy-send-timeout", "proxy-send-timeout", "global-proxy-send-timeout"},
		{"send-timeout", "send-timeout", "global-send-timeout"},
		{"underscore-in-headers", "underscore-in-headers", "global-underscore-in-headers"},
		{"x-forwarded-for-value", "x-forwarded-for-value", "global-x-forwarded-for-value"},
		{"x-forwarded-port-value", "x-forwarded-port-value", "global-x-forwarded-port-value"},
		{"x-forwarded-proto-value", "x-forwarded-proto-value", "global-x-forwarded-proto-value"},
		{"x-forwarded-ssl", "x-forwarded-ssl", "global-x-forwarded-ssl"},
	})
	checkUnsupportedProperty(t, "nginx", nginxPropertyKeys)
}

func TestOpenrestyPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "openresty", openrestyPropertyKeys, []propertyKeysCase{
		{"access-log-format", "access-log-format", "global-access-log-format"},
		{"access-log-path", "access-log-path", "global-access-log-path"},
		{"allowed-letsencrypt-domains-func-base64", "", "global-allowed-letsencrypt-domains-func-base64"},
		{"bind-address-ipv4", "bind-address-ipv4", "global-bind-address-ipv4"},
		{"bind-address-ipv6", "bind-address-ipv6", "global-bind-address-ipv6"},
		{"client-body-timeout", "client-body-timeout", "global-client-body-timeout"},
		{"client-header-timeout", "client-header-timeout", "global-client-header-timeout"},
		{"client-max-body-size", "client-max-body-size", "global-client-max-body-size"},
		{"error-log-path", "error-log-path", "global-error-log-path"},
		{"hsts", "hsts", "global-hsts"},
		{"hsts-include-subdomains", "hsts-include-subdomains", "global-hsts-include-subdomains"},
		{"hsts-max-age", "hsts-max-age", "global-hsts-max-age"},
		{"hsts-preload", "hsts-preload", "global-hsts-preload"},
		{"image", "", "global-image"},
		{"keepalive-timeout", "keepalive-timeout", "global-keepalive-timeout"},
		{"letsencrypt-email", "", "global-letsencrypt-email"},
		{"letsencrypt-server", "", "global-letsencrypt-server"},
		{"lingering-timeout", "lingering-timeout", "global-lingering-timeout"},
		{"log-level", "", "global-log-level"},
		{"proxy-buffer-size", "proxy-buffer-size", "global-proxy-buffer-size"},
		{"proxy-buffering", "proxy-buffering", "global-proxy-buffering"},
		{"proxy-buffers", "proxy-buffers", "global-proxy-buffers"},
		{"proxy-busy-buffers-size", "proxy-busy-buffers-size", "global-proxy-busy-buffers-size"},
		{"proxy-connect-timeout", "proxy-connect-timeout", "global-proxy-connect-timeout"},
		{"proxy-read-timeout", "proxy-read-timeout", "global-proxy-read-timeout"},
		{"proxy-send-timeout", "proxy-send-timeout", "global-proxy-send-timeout"},
		{"send-timeout", "send-timeout", "global-send-timeout"},
		{"underscore-in-headers", "underscore-in-headers", "global-underscore-in-headers"},
		{"x-forwarded-for-value", "x-forwarded-for-value", "global-x-forwarded-for-value"},
		{"x-forwarded-port-value", "x-forwarded-port-value", "global-x-forwarded-port-value"},
		{"x-forwarded-proto-value", "x-forwarded-proto-value", "global-x-forwarded-proto-value"},
		{"x-forwarded-ssl", "x-forwarded-ssl", "global-x-forwarded-ssl"},
	})
	checkUnsupportedProperty(t, "openresty", openrestyPropertyKeys)
	checkScopeMismatch(t, "openresty", openrestyPropertyKeys, "", "image")
}

func TestProxyPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "proxy", proxyPropertyKeys, []propertyKeysCase{
		{"type", "type", "global-type"},
		{"proxy-port", "proxy-port", "global-proxy-port"},
		{"proxy-ssl-port", "proxy-ssl-port", "global-proxy-ssl-port"},
	})
	checkUnsupportedProperty(t, "proxy", proxyPropertyKeys)
}

func TestPsPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "ps", psPropertyKeys, []propertyKeysCase{
		{"dockerfile-start-cmd", "dockerfile-start-cmd", ""},
		{"procfile-path", "procfile-path", "global-procfile-path"},
		{"restart-policy", "restart-policy", "global-restart-policy"},
		{"skip-deploy", "skip-deploy", "global-skip-deploy"},
		{"start-cmd", "start-cmd", ""},
		{"stop-timeout-seconds", "stop-timeout-seconds", "global-stop-timeout-seconds"},
	})
	checkUnsupportedProperty(t, "ps", psPropertyKeys)
	checkScopeMismatch(t, "ps", psPropertyKeys, "dockerfile-start-cmd", "")
}

func TestRegistryPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "registry", registryPropertyKeys, []propertyKeysCase{
		{"image-repo", "image-repo", ""},
		{"image-repo-template", "image-repo-template", "global-image-repo-template"},
		{"push-extra-tags", "push-extra-tags", "global-push-extra-tags"},
		{"push-on-release", "push-on-release", "global-push-on-release"},
		{"server", "server", "global-server"},
	})
	checkUnsupportedProperty(t, "registry", registryPropertyKeys)
	checkScopeMismatch(t, "registry", registryPropertyKeys, "image-repo", "")
}

func TestSchedulerPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "scheduler", schedulerPropertyKeys, []propertyKeysCase{
		{"selected", "selected", "global-selected"},
		{"shell", "shell", "global-shell"},
	})
	checkUnsupportedProperty(t, "scheduler", schedulerPropertyKeys)
}

func TestSchedulerDockerLocalPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "scheduler-docker-local", schedulerDockerLocalPropertyKeys, []propertyKeysCase{
		{"init-process", "init-process", ""},
		{"parallel-schedule-count", "parallel-schedule-count", ""},
	})
	checkUnsupportedProperty(t, "scheduler-docker-local", schedulerDockerLocalPropertyKeys)
}

func TestSchedulerK3sPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "scheduler-k3s", schedulerK3sPropertyKeys, []propertyKeysCase{
		{"deploy-timeout", "deploy-timeout", "global-deploy-timeout"},
		{"image-pull-secrets", "image-pull-secrets", "global-image-pull-secrets"},
		{"ingress-class", "", "global-ingress-class"},
		{"kube-context", "", "global-kube-context"},
		{"kubeconfig-path", "", "global-kubeconfig-path"},
		{"kustomize-root-path", "kustomize-root-path", "global-kustomize-root-path"},
		{"letsencrypt-email-prod", "", "global-letsencrypt-email-prod"},
		{"letsencrypt-email-stag", "", "global-letsencrypt-email-stag"},
		{"letsencrypt-server", "letsencrypt-server", "global-letsencrypt-server"},
		{"namespace", "namespace", "global-namespace"},
		{"network-interface", "", "global-network-interface"},
		{"rollback-on-failure", "rollback-on-failure", "global-rollback-on-failure"},
		{"shm-size", "shm-size", "global-shm-size"},
		{"token", "", "global-token"},
	})
	checkUnsupportedProperty(t, "scheduler-k3s", schedulerK3sPropertyKeys)
	checkScopeMismatch(t, "scheduler-k3s", schedulerK3sPropertyKeys, "", "token")
	// chart.*.* are dynamic and should bypass map validation.
	if err := validateProperty("scheduler-k3s", "chart.traefik.replicas", false, schedulerK3sPropertyKeys); err != nil {
		t.Errorf("dynamic property should pass validation, got %v", err)
	}
}

func TestTraefikPropertyKeys(t *testing.T) {
	checkPropertyKeys(t, "traefik", traefikPropertyKeys, []propertyKeysCase{
		{"api-enabled", "", "global-api-enabled"},
		{"api-entry-point", "", "global-api-entry-point"},
		{"api-entry-point-address", "", "global-api-entry-point-address"},
		{"api-vhost", "", "global-api-vhost"},
		{"basic-auth-password", "", "global-basic-auth-password"},
		{"basic-auth-username", "", "global-basic-auth-username"},
		{"challenge-mode", "", "global-challenge-mode"},
		{"dashboard-enabled", "", "global-dashboard-enabled"},
		{"dns-provider", "", "global-dns-provider"},
		{"http-entry-point", "", "global-http-entry-point"},
		{"https-entry-point", "", "global-https-entry-point"},
		{"image", "", "global-image"},
		{"letsencrypt-email", "", "global-letsencrypt-email"},
		{"letsencrypt-server", "", "global-letsencrypt-server"},
		{"log-level", "", "global-log-level"},
	})
	checkUnsupportedProperty(t, "traefik", traefikPropertyKeys)
	checkScopeMismatch(t, "traefik", traefikPropertyKeys, "", "image")
	// dns-provider-* are dynamic and should bypass map validation.
	if err := validateProperty("traefik", "dns-provider-CLOUDFLARE_API_TOKEN", true, traefikPropertyKeys); err != nil {
		t.Errorf("dynamic property should pass validation, got %v", err)
	}
}
