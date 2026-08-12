package tasks

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// propertyTableTask is a PropertyTableDocer standing in for a real property
// task, so a test can drive the shared helpers against a synthetic plugin. A
// test exercising a real plugin's table passes the task type itself instead.
type propertyTableTask struct {
	table PropertyTable
}

func (p propertyTableTask) PropertyTable() PropertyTable { return p.table }

// fakePropertyTask builds a propertyTableTask from a subcommand and key map.
func fakePropertyTask(subcommand string, keys map[string]PropertyKeys) propertyTableTask {
	return propertyTableTask{table: PropertyTable{Subcommand: subcommand, Keys: keys}}
}

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

	res := planProperty(fakePropertyTask("myplugin:set", keys), StatePresent, "", true, "secret-prop", "newsecret")
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
	res := planProperty(fakePropertyTask("myplugin:set", keys), StateAbsent, "", true, "secret-prop", "")
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

	res := planProperty(fakePropertyTask("myplugin:set", keys), StatePresent, "", true, "timeout", "90s")
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
	if !traefikPropertyTable.Keys["basic-auth-password"].Sensitive {
		t.Error("traefik basic-auth-password must be marked Sensitive")
	}
	if !schedulerK3sPropertyTable.Keys["token"].Sensitive {
		t.Error("scheduler-k3s token must be marked Sensitive")
	}
}

func TestReadPropertyReportUnparseableReportErrors(t *testing.T) {
	// #329: the exec succeeds (plugin responded) but the payload is not clean
	// JSON - e.g. a deprecation line before the JSON. This is "installed but
	// unreadable" and must surface an error (which export turns into a warning),
	// not be silently dropped.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet nginx:report web --format json": "Deprecated: use something else\n{\"x\":\"y\"}",
	}))()

	if _, err := readPropertyReport("nginx", "web", false); err == nil {
		t.Error("expected an error for an installed-but-unreadable report")
	}
}

func TestReadPropertyReportNotInstalledIsQuietSkip(t *testing.T) {
	// #329: when the report exec fails and the plugin is not installed, the skip
	// is quiet (nil, nil) - no warning.
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "--quiet plugin:list":
			return subprocess.ExecCommandResponse{Stdout: "nginx 1.0.0 enabled nginx"}, nil
		case "--quiet caddy:report web --format json":
			return subprocess.ExecCommandResponse{}, errors.New("caddy:report: command not found")
		}
		return subprocess.ExecCommandResponse{}, nil
	})()

	payload, err := readPropertyReport("caddy", "web", false)
	if err != nil {
		t.Errorf("a not-installed plugin should be a quiet skip, got error: %v", err)
	}
	if payload != nil {
		t.Errorf("expected nil payload for a quiet skip, got %v", payload)
	}
}

func TestReadPropertyReportInstalledExecFailureErrors(t *testing.T) {
	// #329: when the report exec fails but the plugin IS installed, the failure
	// must surface an error rather than a silent drop.
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "--quiet plugin:list":
			return subprocess.ExecCommandResponse{Stdout: "nginx 1.0.0 enabled nginx"}, nil
		case "--quiet nginx:report web --format json":
			return subprocess.ExecCommandResponse{}, errors.New("boom")
		}
		return subprocess.ExecCommandResponse{}, nil
	})()

	if _, err := readPropertyReport("nginx", "web", false); err == nil {
		t.Error("expected an error when an installed plugin's report fails")
	}
}

func TestUnknownPropertyWarningMissingKey(t *testing.T) {
	err := &errUnknownProperty{
		plugin:    "nginx",
		property:  "selecte",
		lookedFor: "selecte",
		validKeys: []string{"bind-address-ipv4", "selected"},
	}
	w, ok := unknownPropertyWarning("nginx", "selecte", err)
	if !ok {
		t.Fatal("expected a warning for a missing report key")
	}
	if w.Reason != WarnReasonUnknownProperty {
		t.Errorf("reason = %q; want %q", w.Reason, WarnReasonUnknownProperty)
	}
	for _, want := range []string{"no key", "nginx", "selecte", "selected"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("message %q missing %q", w.Message, want)
		}
	}
}

func TestUnknownPropertyWarningInvalidFlag(t *testing.T) {
	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr: "Invalid flag passed, valid flags: --letsencrypt-email",
		},
		Err: errors.New("exit status 1"),
	}
	w, ok := unknownPropertyWarning("letsencrypt", "email", execErr)
	if !ok {
		t.Fatal("expected a warning for a rejected probe")
	}
	if w.Reason != WarnReasonProbeRejected {
		t.Errorf("reason = %q; want %q", w.Reason, WarnReasonProbeRejected)
	}
	for _, want := range []string{"rejected probe", "letsencrypt", "Invalid flag passed"} {
		if !strings.Contains(w.Message, want) {
			t.Errorf("message %q missing %q", w.Message, want)
		}
	}
}

// TestUnknownPropertyWarningMasksSensitiveStderr is the core #353 guarantee:
// the rejected-probe branch embeds the server's raw stderr, and a registered
// secret that reaches it must mask at emit time. The message is stored raw
// (like PlanResult.Reason) so the assertion masks it the way the emitter does.
func TestUnknownPropertyWarningMasksSensitiveStderr(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })
	subprocess.AddGlobalSensitive("s3cr3t")

	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr: "Invalid flag passed, valid flags: --token near value s3cr3t",
		},
		Err: errors.New("exit status 1"),
	}
	w, ok := unknownPropertyWarning("registry", "password", execErr)
	if !ok {
		t.Fatal("expected a warning for a rejected probe")
	}
	if !strings.Contains(w.Message, "s3cr3t") {
		t.Fatalf("message should embed raw stderr pre-masking, got %q", w.Message)
	}
	if masked := subprocess.MaskString(w.Message); strings.Contains(masked, "s3cr3t") {
		t.Errorf("masked warning leaked secret: %q -> %q", w.Message, masked)
	}
}

func TestUnknownPropertyWarningIgnoresOtherErrors(t *testing.T) {
	if _, ok := unknownPropertyWarning("nginx", "bind-address-ipv4", nil); ok {
		t.Error("nil error should not warn")
	}
	if _, ok := unknownPropertyWarning("nginx", "bind-address-ipv4", errors.New("plain")); ok {
		t.Error("plain error should not warn")
	}
	execErr := &subprocess.ExecError{
		Response: subprocess.ExecCommandResponse{
			Stderr: "App nonexistent does not exist",
		},
		Err: errors.New("exit status 1"),
	}
	if _, ok := unknownPropertyWarning("nginx", "bind-address-ipv4", execErr); ok {
		t.Error("non-flag exec error should not warn")
	}
}

// TestUnknownPropertyWarningDynamicPropertySkipsWarning uses traefik because
// letsencrypt's dns-provider-* family is probed now (#449) and its missing rows
// never reach this branch; traefik is the family that still does.
func TestUnknownPropertyWarningDynamicPropertySkipsWarning(t *testing.T) {
	err := &errUnknownProperty{
		plugin:    "traefik",
		property:  "dns-provider-CLOUDFLARE_API_TOKEN",
		lookedFor: "dns-provider-CLOUDFLARE_API_TOKEN",
		validKeys: []string{"global-dns-provider", "global-letsencrypt-email"},
	}
	if _, ok := unknownPropertyWarning("traefik", "dns-provider-CLOUDFLARE_API_TOKEN", err); ok {
		t.Error("dynamic property should not warn")
	}
}

// TestPlanPropertyAttachesUnknownKeyWarning drives the whole Plan() path: a
// report payload missing the probed key yields drift plus a PlanWarning the run
// loop can drain. (#353)
func TestPlanPropertyAttachesUnknownKeyWarning(t *testing.T) {
	keys := map[string]PropertyKeys{
		"hsts": {PerApp: "hsts", Global: ""},
	}
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet nginx:report myapp --format json": `{"proxy-read-timeout":"60s"}`,
	}))()

	res := planProperty(fakePropertyTask("nginx:set", keys), StatePresent, "myapp", false, "hsts", "true")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d (%v)", len(res.Warnings), res.Warnings)
	}
	if res.Warnings[0].Reason != WarnReasonUnknownProperty {
		t.Errorf("reason = %q; want %q", res.Warnings[0].Reason, WarnReasonUnknownProperty)
	}
}

// TestPlanPropertyAttachesRejectedProbeWarning drives the older-plugin path:
// `:report --format json` fails with an "Invalid flag" stderr, so the probe
// error becomes drift plus a probe_rejected PlanWarning. (#353)
func TestPlanPropertyAttachesRejectedProbeWarning(t *testing.T) {
	keys := map[string]PropertyKeys{
		"hsts": {PerApp: "hsts", Global: ""},
	}
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		resp := subprocess.ExecCommandResponse{
			Stderr:   "Invalid flag passed, valid flags: --app, --global",
			ExitCode: 1,
		}
		return resp, &subprocess.ExecError{Response: resp, Err: errors.New("exit status 1"), Ran: true}
	})()

	res := planProperty(fakePropertyTask("nginx:set", keys), StatePresent, "myapp", false, "hsts", "true")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if len(res.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %d (%v)", len(res.Warnings), res.Warnings)
	}
	if res.Warnings[0].Reason != WarnReasonProbeRejected {
		t.Errorf("reason = %q; want %q", res.Warnings[0].Reason, WarnReasonProbeRejected)
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

func TestDynamicPropertyKeys(t *testing.T) {
	cases := []struct {
		plugin   string
		property string
		want     PropertyKeys
		wantOK   bool
	}{
		{"letsencrypt", "dns-provider-NAMECHEAP_API_USER", PropertyKeys{
			PerApp:    "dns-provider-NAMECHEAP_API_USER",
			Global:    "global-dns-provider-NAMECHEAP_API_USER",
			Sensitive: true,
		}, true},
		// The mapped provider name is not dynamic, and neither is any other
		// mapped property.
		{"letsencrypt", "dns-provider", PropertyKeys{}, false},
		{"letsencrypt", "email", PropertyKeys{}, false},
		// traefik's family has the same shape but is still absent from
		// traefik:report (#450), so it must stay unprobed.
		{"traefik", "dns-provider-CLOUDFLARE_API_TOKEN", PropertyKeys{}, false},
		{"nginx", "dns-provider-X", PropertyKeys{}, false},
	}
	for _, tc := range cases {
		got, ok := dynamicPropertyKeys(tc.plugin, tc.property)
		if ok != tc.wantOK {
			t.Errorf("dynamicPropertyKeys(%q, %q) ok = %v; want %v", tc.plugin, tc.property, ok, tc.wantOK)
			continue
		}
		if got != tc.want {
			t.Errorf("dynamicPropertyKeys(%q, %q) = %+v; want %+v", tc.plugin, tc.property, got, tc.want)
		}
	}
}

// TestDynamicPropertiesFromReport pins the scope filtering the exporters rely
// on: an app report also carries the global and computed variants of a key, and
// only the bare row is the app's own value.
func TestDynamicPropertiesFromReport(t *testing.T) {
	appPayload := map[string]string{
		"email":                                    "admin@example.com",
		"dns-provider":                             "namecheap",
		"dns-provider-NAMECHEAP_API_USER":          "deploy-bot",
		"global-dns-provider-NAMECHEAP_API_KEY":    "globalkey",
		"computed-dns-provider-NAMECHEAP_API_USER": "deploy-bot",
		"computed-dns-provider-NAMECHEAP_API_KEY":  "globalkey",
	}
	got := dynamicPropertiesFromReport("letsencrypt", appPayload, false)
	if !reflect.DeepEqual(got, []string{"dns-provider-NAMECHEAP_API_USER"}) {
		t.Errorf("app scope = %v; want only the bare app row", got)
	}

	globalPayload := map[string]string{
		"global-email":                           "",
		"global-dns-provider":                    "namecheap",
		"global-dns-provider-NAMECHEAP_API_USER": "deploy-bot",
		"global-dns-provider-NAMECHEAP_API_KEY":  "globalkey",
	}
	got = dynamicPropertiesFromReport("letsencrypt", globalPayload, true)
	want := []string{"dns-provider-NAMECHEAP_API_KEY", "dns-provider-NAMECHEAP_API_USER"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("global scope = %v; want %v", got, want)
	}

	// traefik is not probeable, so its report rows are never lifted.
	if got := dynamicPropertiesFromReport("traefik", map[string]string{"global-dns-provider-CLOUDFLARE_API_TOKEN": "token"}, true); got != nil {
		t.Errorf("traefik = %v; want nil", got)
	}
}

// TestPlanPropertyDynamicLetsencryptInSync is the core of #449: a
// dns-provider-* credential that already matches the recipe plans as in sync
// instead of reporting drift on every run.
func TestPlanPropertyDynamicLetsencryptInSync(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"email":"admin@example.com","dns-provider-CLOUDFLARE_API_TOKEN":"token123"}`,
	}))()

	res := planProperty(LetsencryptPropertyTask{}, StatePresent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !res.InSync {
		t.Errorf("expected in sync, got status %q reason %q", res.Status, res.Reason)
	}
	if res.Status != PlanStatusOK {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusOK)
	}
}

func TestPlanPropertyDynamicLetsencryptGlobalInSync(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet letsencrypt:report --global --format json": `{"global-dns-provider-NAMECHEAP_API_USER":"deploy-bot"}`,
	}))()

	res := planProperty(LetsencryptPropertyTask{}, StatePresent, "", true, "dns-provider-NAMECHEAP_API_USER", "deploy-bot")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !res.InSync {
		t.Errorf("expected in sync from the global row, got status %q reason %q", res.Status, res.Reason)
	}
}

// TestPlanPropertyDynamicLetsencryptDriftMasksProbedValue guards the Sensitive
// mark on the synthesized keys: the probed credential reaches the drift reason
// and must be registered with the masker.
func TestPlanPropertyDynamicLetsencryptDriftMasksProbedValue(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"dns-provider-CLOUDFLARE_API_TOKEN":"oldtoken"}`,
	}))()

	res := planProperty(LetsencryptPropertyTask{}, StatePresent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "newtoken")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusModify {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusModify)
	}
	if !strings.Contains(res.Reason, "drift") {
		t.Errorf("reason = %q; want a drift reason", res.Reason)
	}
	if masked := subprocess.MaskString(res.Reason); strings.Contains(masked, "oldtoken") {
		t.Errorf("drift reason leaked the probed credential: %q -> %q", res.Reason, masked)
	}
	for _, cmd := range res.Commands {
		if masked := subprocess.MaskString(cmd); strings.Contains(masked, "newtoken") {
			t.Errorf("command leaked the desired credential: %q -> %q", cmd, masked)
		}
	}
}

// TestPlanPropertyDynamicLetsencryptMissingRowPlansCreate covers the pre-set
// state: the plugin only emits a row once the property holds a value, so an
// absent row is an unset property and not a probe failure worth warning about.
func TestPlanPropertyDynamicLetsencryptMissingRowPlansCreate(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"email":"admin@example.com"}`,
	}))()

	res := planProperty(LetsencryptPropertyTask{}, StatePresent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusCreate {
		t.Errorf("status = %q; want %q (reason %q)", res.Status, PlanStatusCreate, res.Reason)
	}
	if !strings.Contains(res.Reason, "missing on myapp") {
		t.Errorf("reason = %q; want a missing-on reason", res.Reason)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("an unset dynamic property must not warn, got %v", res.Warnings)
	}
}

func TestPlanPropertyDynamicLetsencryptAbsentMissingRowIsInSync(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"email":"admin@example.com"}`,
	}))()

	res := planProperty(LetsencryptPropertyTask{}, StateAbsent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !res.InSync {
		t.Errorf("an already-unset credential must plan as in sync, got status %q reason %q", res.Status, res.Reason)
	}
}

func TestPlanPropertyDynamicLetsencryptAbsentPlansDestroy(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"dns-provider-CLOUDFLARE_API_TOKEN":"livetoken"}`,
	}))()

	res := planProperty(LetsencryptPropertyTask{}, StateAbsent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusDestroy {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusDestroy)
	}
	if masked := subprocess.MaskString(res.Reason); strings.Contains(masked, "livetoken") {
		t.Errorf("unset reason leaked the probed credential: %q -> %q", res.Reason, masked)
	}
}

// TestPlanPropertyDynamicTraefikStaysUnprobed keeps the traefik half of the
// family on the unprobed path while dokku/dokku#8928 is open (#450): it must not
// even attempt a report read.
func TestPlanPropertyDynamicTraefikStaysUnprobed(t *testing.T) {
	var ran []string
	defer subprocess.SetExecRunner(func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		ran = append(ran, strings.Join(in.Args, " "))
		return subprocess.ExecCommandResponse{}, nil
	})()

	res := planProperty(TraefikPropertyTask{}, StatePresent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !strings.Contains(res.Reason, "(no probe key)") {
		t.Errorf("reason = %q; want the unprobed reason", res.Reason)
	}
	for _, cmd := range ran {
		if strings.Contains(cmd, "traefik:report") {
			t.Errorf("traefik dynamic properties must not be probed, ran %q", cmd)
		}
	}
}

// TestPlanPropertyDynamicTraefikMasksCredential is the core of #457: the
// traefik family cannot be probed, but its values are DNS provider credentials
// all the same, so the desired value must reach the masker before it lands in
// the command echo or the plan mutation line.
func TestPlanPropertyDynamicTraefikMasksCredential(t *testing.T) {
	subprocess.SetGlobalSensitive(nil)
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	defer subprocess.SetExecRunner(func(_ context.Context, _ subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		return subprocess.ExecCommandResponse{}, nil
	})()

	res := planProperty(TraefikPropertyTask{}, StatePresent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "traefiktoken")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !strings.Contains(res.Reason, "(no probe key)") {
		t.Errorf("reason = %q; want the unprobed reason", res.Reason)
	}
	// Commands are masked as they are resolved, so a leak here means the value
	// was never registered; mutations are masked by the emitter instead.
	for _, cmd := range res.Commands {
		if masked := subprocess.MaskString(cmd); strings.Contains(masked, "traefiktoken") {
			t.Errorf("command leaked the credential: %q -> %q", cmd, masked)
		}
	}
	for _, mutation := range res.Mutations {
		if masked := subprocess.MaskString(mutation); strings.Contains(masked, "traefiktoken") {
			t.Errorf("mutation leaked the credential: %q -> %q", mutation, masked)
		}
	}
}

// TestPropertyEntry pins the three arms the export path and planProperty read
// sensitivity through: a mapped property answers for itself, a probeable
// dynamic member is synthesized whole, and an unprobeable one carries the
// family's Sensitive mark with no lookup keys (#457).
func TestPropertyEntry(t *testing.T) {
	cases := []struct {
		name     string
		plugin   string
		property string
		keys     map[string]PropertyKeys
		want     PropertyKeys
	}{
		{
			name:     "mapped entry wins",
			plugin:   "traefik",
			property: "basic-auth-password",
			keys:     traefikPropertyTable.Keys,
			want:     PropertyKeys{Global: "global-basic-auth-password", Sensitive: true},
		},
		{
			name:     "probeable dynamic member is synthesized",
			plugin:   "letsencrypt",
			property: "dns-provider-NAMECHEAP_API_USER",
			keys:     letsencryptPropertyTable.Keys,
			want: PropertyKeys{
				PerApp:    "dns-provider-NAMECHEAP_API_USER",
				Global:    "global-dns-provider-NAMECHEAP_API_USER",
				Sensitive: true,
			},
		},
		{
			name:     "unprobeable dynamic member is still sensitive",
			plugin:   "traefik",
			property: "dns-provider-CLOUDFLARE_API_TOKEN",
			keys:     traefikPropertyTable.Keys,
			want:     PropertyKeys{Sensitive: true},
		},
		{
			name:     "unknown property has no entry",
			plugin:   "traefik",
			property: "wat",
			keys:     traefikPropertyTable.Keys,
			want:     PropertyKeys{},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := propertyEntry(tc.plugin, tc.property, tc.keys); got != tc.want {
				t.Errorf("propertyEntry(%q, %q) = %+v; want %+v", tc.plugin, tc.property, got, tc.want)
			}
		})
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
