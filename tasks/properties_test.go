package tasks

import (
	"bytes"
	"errors"
	"log"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestGetPropertyArgsPerApp(t *testing.T) {
	got := getPropertyArgs("nginx", "myapp", false)
	want := []string{"--quiet", "nginx:report", "myapp", "--format", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getPropertyArgs(nginx, myapp, false) = %v; want %v", got, want)
	}
}

func TestGetPropertyArgsGlobal(t *testing.T) {
	got := getPropertyArgs("nginx", "", true)
	want := []string{"--quiet", "nginx:report", "--global", "--format", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getPropertyArgs(nginx, \"\", true) = %v; want %v", got, want)
	}
}

func TestPlanPropertyMasksSensitiveDriftValue(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	keys := map[string]PropertyKeys{
		"secret-prop": {PerApp: "", Global: "global-secret-prop", Sensitive: true},
	}
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet myplugin:report --global --format json": `{"global-secret-prop":"oldsecret"}`,
	}))()

	res := planProperty(StatePresent, "", true, "secret-prop", "newsecret", "myplugin:set", keys)
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}

	// The probed old value and the desired new value are both secrets and must
	// be registered so the drift reason and command echo mask them.
	if masked := subprocess.MaskString(res.Reason); strings.Contains(masked, "oldsecret") {
		t.Errorf("drift reason leaked probed secret: %q -> %q", res.Reason, masked)
	}
	if !strings.Contains(res.Reason, "oldsecret") {
		t.Fatalf("expected reason to embed the probed value pre-masking, got %q", res.Reason)
	}
	if masked := subprocess.MaskString(res.Reason); !strings.Contains(masked, "***") {
		t.Errorf("expected mask placeholder in masked reason, got %q", masked)
	}
	for _, cmd := range res.Commands {
		if masked := subprocess.MaskString(cmd); strings.Contains(masked, "newsecret") {
			t.Errorf("command leaked desired secret after masking: %q -> %q", cmd, masked)
		}
	}
}

func TestPlanPropertyAbsentMasksSensitiveOldValue(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	keys := map[string]PropertyKeys{
		"secret-prop": {PerApp: "", Global: "global-secret-prop", Sensitive: true},
	}
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet myplugin:report --global --format json": `{"global-secret-prop":"livesecret"}`,
	}))()

	// The absent path leaks the current server secret even without a sensitive
	// recipe value (the value must be empty for absent).
	res := planProperty(StateAbsent, "", true, "secret-prop", "", "myplugin:set", keys)
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if masked := subprocess.MaskString(res.Reason); strings.Contains(masked, "livesecret") {
		t.Errorf("unset reason leaked server secret: %q -> %q", res.Reason, masked)
	}
}

func TestPlanPropertyDoesNotMaskBenignDriftValue(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	keys := map[string]PropertyKeys{
		"timeout": {PerApp: "", Global: "global-timeout"},
	}
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet myplugin:report --global --format json": `{"global-timeout":"60s"}`,
	}))()

	res := planProperty(StatePresent, "", true, "timeout", "90s", "myplugin:set", keys)
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	// A non-sensitive property keeps its old value visible for a useful diff.
	if masked := subprocess.MaskString(res.Reason); !strings.Contains(masked, "60s") {
		t.Errorf("benign old value should not be masked, got %q", masked)
	}
}

func TestSecretPropertiesAreMarkedSensitive(t *testing.T) {
	// Guard the marks that close #336 so they are not accidentally dropped.
	if !traefikPropertyKeys["basic-auth-password"].Sensitive {
		t.Error("traefik basic-auth-password must be marked Sensitive")
	}
	if !schedulerK3sPropertyKeys["token"].Sensitive {
		t.Error("scheduler-k3s token must be marked Sensitive")
	}
}

func captureLog(t *testing.T) *bytes.Buffer {
	t.Helper()
	buf := &bytes.Buffer{}
	orig := log.Writer()
	flags := log.Flags()
	prefix := log.Prefix()
	log.SetOutput(buf)
	log.SetFlags(0)
	log.SetPrefix("")
	t.Cleanup(func() {
		log.SetOutput(orig)
		log.SetFlags(flags)
		log.SetPrefix(prefix)
	})
	return buf
}

func TestWarnIfUnknownPropertyMissingKey(t *testing.T) {
	buf := captureLog(t)
	err := &errUnknownProperty{
		plugin:    "nginx",
		property:  "selecte",
		lookedFor: "selecte",
		validKeys: []string{"bind-address-ipv4", "selected"},
	}
	warnIfUnknownProperty("nginx", "selecte", err)
	out := buf.String()
	if !strings.Contains(out, "no key") {
		t.Errorf("log output missing 'no key': %q", out)
	}
	if !strings.Contains(out, "nginx") {
		t.Errorf("log output missing plugin name: %q", out)
	}
	if !strings.Contains(out, "selecte") {
		t.Errorf("log output missing property name: %q", out)
	}
	if !strings.Contains(out, "selected") {
		t.Errorf("log output missing available key list: %q", out)
	}
}

func TestWarnIfUnknownPropertyInvalidFlag(t *testing.T) {
	buf := captureLog(t)
	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr: "Invalid flag passed, valid flags: --letsencrypt-email",
		},
		Err: errors.New("exit status 1"),
	}
	warnIfUnknownProperty("letsencrypt", "email", execErr)
	out := buf.String()
	if !strings.Contains(out, "rejected probe") {
		t.Errorf("log output missing 'rejected probe': %q", out)
	}
	if !strings.Contains(out, "letsencrypt") {
		t.Errorf("log output missing plugin name: %q", out)
	}
	if !strings.Contains(out, "Invalid flag passed") {
		t.Errorf("log output missing stderr snippet: %q", out)
	}
}

func TestWarnIfUnknownPropertyIgnoresOtherErrors(t *testing.T) {
	buf := captureLog(t)
	warnIfUnknownProperty("nginx", "bind-address-ipv4", nil)
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for nil error, got %q", buf.String())
	}

	warnIfUnknownProperty("nginx", "bind-address-ipv4", errors.New("plain"))
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for plain error, got %q", buf.String())
	}

	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr: "App nonexistent does not exist",
		},
		Err: errors.New("exit status 1"),
	}
	warnIfUnknownProperty("nginx", "bind-address-ipv4", execErr)
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for non-flag exec error, got %q", buf.String())
	}
}

func TestWarnIfUnknownPropertyDynamicPropertySkipsWarning(t *testing.T) {
	buf := captureLog(t)
	err := &errUnknownProperty{
		plugin:    "letsencrypt",
		property:  "dns-provider-NAMECHEAP_API_USER",
		lookedFor: "dns-provider-NAMECHEAP_API_USER",
		validKeys: []string{"email", "server"},
	}
	warnIfUnknownProperty("letsencrypt", "dns-provider-NAMECHEAP_API_USER", err)
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for dynamic property, got %q", buf.String())
	}
}

func TestIsDynamicProperty(t *testing.T) {
	cases := []struct {
		plugin   string
		property string
		want     bool
	}{
		{"letsencrypt", "dns-provider-NAMECHEAP_API_USER", true},
		{"letsencrypt", "dns-provider-X", true},
		{"letsencrypt", "email", false},
		{"traefik", "dns-provider-CLOUDFLARE_API_TOKEN", true},
		{"traefik", "dns-provider", false},
		// scheduler-k3s chart.* used to be dynamic; it is now handled
		// by the dedicated dokku_scheduler_k3s_chart task and the
		// property task rejects chart.* before reaching here.
		{"scheduler-k3s", "chart.traefik.replicas", false},
		{"scheduler-k3s", "namespace", false},
		{"nginx", "dns-provider-X", false},
	}
	for _, tc := range cases {
		got := isDynamicProperty(tc.plugin, tc.property)
		if got != tc.want {
			t.Errorf("isDynamicProperty(%q, %q) = %v; want %v", tc.plugin, tc.property, got, tc.want)
		}
	}
}

func TestValidateProperty(t *testing.T) {
	keys := map[string]PropertyKeys{
		"both":       {PerApp: "both", Global: "global-both"},
		"app-only":   {PerApp: "app-only", Global: ""},
		"global-only": {PerApp: "", Global: "global-global-only"},
	}

	cases := []struct {
		name     string
		property string
		global   bool
		wantErr  string
	}{
		{"app+global per-app ok", "both", false, ""},
		{"app+global global ok", "both", true, ""},
		{"app-only per-app ok", "app-only", false, ""},
		{"app-only global rejected", "app-only", true, "no global form"},
		{"global-only global ok", "global-only", true, ""},
		{"global-only per-app rejected", "global-only", false, "no per-app form"},
		{"unsupported", "wat", false, "unsupported property"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validateProperty("test", tc.property, tc.global, keys)
			if tc.wantErr == "" {
				if err != nil {
					t.Errorf("got error %v; want nil", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("got nil error; want substring %q", tc.wantErr)
			}
			if !strings.Contains(err.Error(), tc.wantErr) {
				t.Errorf("got error %q; want substring %q", err.Error(), tc.wantErr)
			}
		})
	}
}

func TestValidatePropertyDynamic(t *testing.T) {
	keys := map[string]PropertyKeys{
		"email": {PerApp: "email", Global: "global-email"},
	}
	if err := validateProperty("letsencrypt", "dns-provider-CLOUDFLARE_API_TOKEN", false, keys); err != nil {
		t.Errorf("dynamic property should pass validation, got %v", err)
	}
	if err := validateProperty("letsencrypt", "dns-provider-CLOUDFLARE_API_TOKEN", true, keys); err != nil {
		t.Errorf("dynamic property should pass validation in global scope, got %v", err)
	}
}
