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

func TestResolvePropertyKeyPerApp(t *testing.T) {
	cases := []struct {
		name     string
		payload  map[string]string
		plugin   string
		property string
		want     string
	}{
		{
			name:     "nginx bare key",
			payload:  map[string]string{"bind-address-ipv4": "1.2.3.4", "computed-bind-address-ipv4": "1.2.3.4"},
			plugin:   "nginx",
			property: "bind-address-ipv4",
			want:     "1.2.3.4",
		},
		{
			name:     "app-json prefixed key",
			payload:  map[string]string{"app-json-appjson-path": "app.json", "app-json-computed-appjson-path": "app.json"},
			plugin:   "app-json",
			property: "appjson-path",
			want:     "app.json",
		},
		{
			name:     "network prefixed key",
			payload:  map[string]string{"network-bind-all-interfaces": "true"},
			plugin:   "network",
			property: "bind-all-interfaces",
			want:     "true",
		},
		{
			name:     "ps prefixed key restart-policy",
			payload:  map[string]string{"ps-restart-policy": "always"},
			plugin:   "ps",
			property: "restart-policy",
			want:     "always",
		},
		{
			name:     "ps bare key stop-timeout-seconds",
			payload:  map[string]string{"stop-timeout-seconds": "60"},
			plugin:   "ps",
			property: "stop-timeout-seconds",
			want:     "60",
		},
		{
			name:     "letsencrypt bare key",
			payload:  map[string]string{"email": "x@example.com"},
			plugin:   "letsencrypt",
			property: "email",
			want:     "x@example.com",
		},
		{
			name:     "scheduler-docker-local bare key",
			payload:  map[string]string{"init-process": "true", "computed-init-process": "true"},
			plugin:   "scheduler-docker-local",
			property: "init-process",
			want:     "true",
		},
		{
			name:     "caddy bare key tls-internal",
			payload:  map[string]string{"tls-internal": "true", "computed-tls-internal": "true"},
			plugin:   "caddy",
			property: "tls-internal",
			want:     "true",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolvePropertyKey(tc.payload, tc.plugin, tc.property, false)
			if !ok {
				t.Fatalf("resolvePropertyKey returned !ok; payload=%v", tc.payload)
			}
			if got != tc.want {
				t.Errorf("resolvePropertyKey = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePropertyKeyGlobal(t *testing.T) {
	cases := []struct {
		name     string
		payload  map[string]string
		plugin   string
		property string
		want     string
	}{
		{
			name:     "nginx global- prefix",
			payload:  map[string]string{"global-bind-address-ipv4": "1.2.3.4"},
			plugin:   "nginx",
			property: "bind-address-ipv4",
			want:     "1.2.3.4",
		},
		{
			name:     "app-json plugin-global- prefix",
			payload:  map[string]string{"app-json-global-appjson-path": "app.json"},
			plugin:   "app-json",
			property: "appjson-path",
			want:     "app.json",
		},
		{
			name:     "network plugin-global- prefix",
			payload:  map[string]string{"network-global-bind-all-interfaces": "true"},
			plugin:   "network",
			property: "bind-all-interfaces",
			want:     "true",
		},
		{
			name:     "letsencrypt global- prefix",
			payload:  map[string]string{"global-email": "x@example.com"},
			plugin:   "letsencrypt",
			property: "email",
			want:     "x@example.com",
		},
		{
			name:     "scheduler-docker-local global- prefix",
			payload:  map[string]string{"global-init-process": "true"},
			plugin:   "scheduler-docker-local",
			property: "init-process",
			want:     "true",
		},
		{
			name:     "logs grouped subsystem vector-image",
			payload:  map[string]string{"logs-vector-global-image": "timberio/vector:0.55.0"},
			plugin:   "logs",
			property: "vector-image",
			want:     "timberio/vector:0.55.0",
		},
		{
			name:     "logs grouped subsystem vector-networks",
			payload:  map[string]string{"logs-vector-global-networks": "net1,net2"},
			plugin:   "logs",
			property: "vector-networks",
			want:     "net1,net2",
		},
		{
			name:     "caddy global-tls-internal",
			payload:  map[string]string{"global-tls-internal": "true"},
			plugin:   "caddy",
			property: "tls-internal",
			want:     "true",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := resolvePropertyKey(tc.payload, tc.plugin, tc.property, true)
			if !ok {
				t.Fatalf("resolvePropertyKey returned !ok; payload=%v", tc.payload)
			}
			if got != tc.want {
				t.Errorf("resolvePropertyKey = %q; want %q", got, tc.want)
			}
		})
	}
}

func TestResolvePropertyKeyMissing(t *testing.T) {
	payload := map[string]string{"unrelated": "value"}
	if _, ok := resolvePropertyKey(payload, "nginx", "bind-address-ipv4", false); ok {
		t.Errorf("resolvePropertyKey should return false when no candidate matches")
	}
	if _, ok := resolvePropertyKey(payload, "nginx", "bind-address-ipv4", true); ok {
		t.Errorf("resolvePropertyKey should return false when no candidate matches (global)")
	}
}

func TestCandidateKeysPerApp(t *testing.T) {
	got := candidateKeys("nginx", "bind-address-ipv4", false)
	want := []string{"bind-address-ipv4", "nginx-bind-address-ipv4"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidateKeys per-app = %v; want %v", got, want)
	}
}

func TestCandidateKeysGlobalGroupedSubsystem(t *testing.T) {
	got := candidateKeys("logs", "vector-image", true)
	want := []string{
		"global-vector-image",
		"logs-global-vector-image",
		"vector-image",
		"logs-vector-image",
		"logs-vector-global-image",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidateKeys global grouped = %v; want %v", got, want)
	}
}

func TestCandidateKeysGlobalNoDash(t *testing.T) {
	got := candidateKeys("nginx", "image", true)
	want := []string{
		"global-image",
		"nginx-global-image",
		"image",
		"nginx-image",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("candidateKeys global no-dash = %v; want %v", got, want)
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
		global:    false,
		validKeys: []string{"bind-address-ipv4", "selected"},
	}
	warnIfUnknownProperty("nginx", "selecte", false, err)
	out := buf.String()
	if !strings.Contains(out, "no key for property") {
		t.Errorf("log output missing 'no key for property': %q", out)
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
	warnIfUnknownProperty("letsencrypt", "email", false, execErr)
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
	warnIfUnknownProperty("nginx", "bind-address-ipv4", false, nil)
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for nil error, got %q", buf.String())
	}

	warnIfUnknownProperty("nginx", "bind-address-ipv4", false, errors.New("plain"))
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for plain error, got %q", buf.String())
	}

	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr: "App nonexistent does not exist",
		},
		Err: errors.New("exit status 1"),
	}
	warnIfUnknownProperty("nginx", "bind-address-ipv4", false, execErr)
	if buf.Len() != 0 {
		t.Errorf("log output should be empty for non-flag exec error, got %q", buf.String())
	}
}

func TestWarnIfUnknownPropertyDynamicPropertySkipsWarning(t *testing.T) {
	buf := captureLog(t)
	err := &errUnknownProperty{
		plugin:    "letsencrypt",
		property:  "dns-provider-NAMECHEAP_API_USER",
		global:    false,
		validKeys: []string{"email", "server"},
	}
	warnIfUnknownProperty("letsencrypt", "dns-provider-NAMECHEAP_API_USER", false, err)
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
		{"nginx", "dns-provider-X", false},
	}
	for _, tc := range cases {
		got := isDynamicProperty(tc.plugin, tc.property)
		if got != tc.want {
			t.Errorf("isDynamicProperty(%q, %q) = %v; want %v", tc.plugin, tc.property, got, tc.want)
		}
	}
}
