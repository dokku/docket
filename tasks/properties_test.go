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
	t.Parallel()
	got := getPropertyArgs("nginx", "myapp", false)
	want := []string{"--quiet", "nginx:report", "myapp", "--format", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getPropertyArgs(nginx, myapp, false) = %v; want %v", got, want)
	}
}

func TestGetPropertyArgsGlobal(t *testing.T) {
	t.Parallel()
	got := getPropertyArgs("nginx", "", true)
	want := []string{"--quiet", "nginx:report", "--global", "--format", "json"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("getPropertyArgs(nginx, \"\", true) = %v; want %v", got, want)
	}
}

func TestPlanPropertyMasksSensitiveDriftValue(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	keys := map[string]PropertyKeys{
		"secret-prop": {PerApp: "", Global: "global-secret-prop", Sensitive: true},
	}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet myplugin:report --global --format json": `{"global-secret-prop":"oldsecret"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), fakePropertyTask("myplugin:set", keys), StatePresent, "", true, "secret-prop", "newsecret")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}

	// The probed old value and the desired new value are both secrets and must
	// be registered so the drift reason and command echo mask them.
	if masked := masker.String(res.Reason); strings.Contains(masked, "oldsecret") {
		t.Errorf("drift reason leaked probed secret: %q -> %q", res.Reason, masked)
	}
	if !strings.Contains(res.Reason, "oldsecret") {
		t.Fatalf("expected reason to embed the probed value pre-masking, got %q", res.Reason)
	}
	if masked := masker.String(res.Reason); !strings.Contains(masked, "***") {
		t.Errorf("expected mask placeholder in masked reason, got %q", masked)
	}
	for _, cmd := range res.Commands {
		if masked := masker.String(cmd); strings.Contains(masked, "newsecret") {
			t.Errorf("command leaked desired secret after masking: %q -> %q", cmd, masked)
		}
	}
}

func TestPlanPropertyAbsentMasksSensitiveOldValue(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	keys := map[string]PropertyKeys{
		"secret-prop": {PerApp: "", Global: "global-secret-prop", Sensitive: true},
	}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet myplugin:report --global --format json": `{"global-secret-prop":"livesecret"}`,
	}))

	// The absent path leaks the current server secret even without a sensitive
	// recipe value (the value must be empty for absent).
	res := planProperty(subprocess.ContextWithMasker(ctx, masker), fakePropertyTask("myplugin:set", keys), StateAbsent, "", true, "secret-prop", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if masked := masker.String(res.Reason); strings.Contains(masked, "livesecret") {
		t.Errorf("unset reason leaked server secret: %q -> %q", res.Reason, masked)
	}
}

func TestPlanPropertyDoesNotMaskBenignDriftValue(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	keys := map[string]PropertyKeys{
		"timeout": {PerApp: "", Global: "global-timeout"},
	}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet myplugin:report --global --format json": `{"global-timeout":"60s"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), fakePropertyTask("myplugin:set", keys), StatePresent, "", true, "timeout", "90s")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	// A non-sensitive property keeps its old value visible for a useful diff.
	if masked := masker.String(res.Reason); !strings.Contains(masked, "60s") {
		t.Errorf("benign old value should not be masked, got %q", masked)
	}
}

func TestSecretPropertiesAreMarkedSensitive(t *testing.T) {
	t.Parallel()
	// Guard the marks that close #336 so they are not accidentally dropped.
	if !traefikPropertyTable.Keys["basic-auth-password"].Sensitive {
		t.Error("traefik basic-auth-password must be marked Sensitive")
	}
	if !schedulerK3sPropertyTable.Keys["token"].Sensitive {
		t.Error("scheduler-k3s token must be marked Sensitive")
	}
}

func TestReadPropertyReportUnparseableReportErrors(t *testing.T) {
	t.Parallel()
	// #329: the exec succeeds (plugin responded) but the payload is not clean
	// JSON - e.g. a deprecation line before the JSON. This is "installed but
	// unreadable" and must surface an error (which export turns into a warning),
	// not be silently dropped.
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet nginx:report web --format json": "Deprecated: use something else\n{\"x\":\"y\"}",
	}))

	if _, err := readPropertyReport(ctx, "nginx", "web", false); err == nil {
		t.Error("expected an error for an installed-but-unreadable report")
	}
}

func TestReadPropertyReportNotInstalledIsQuietSkip(t *testing.T) {
	t.Parallel()
	// #329: when the report exec fails and the plugin is not installed, the skip
	// is quiet (nil, nil) - no warning.
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "--quiet plugin:list":
			return subprocess.ExecCommandResponse{Stdout: "nginx 1.0.0 enabled nginx"}, nil
		case "--quiet caddy:report web --format json":
			return subprocess.ExecCommandResponse{}, errors.New("caddy:report: command not found")
		}
		return subprocess.ExecCommandResponse{}, nil
	})

	payload, err := readPropertyReport(ctx, "caddy", "web", false)
	if err != nil {
		t.Errorf("a not-installed plugin should be a quiet skip, got error: %v", err)
	}
	if payload != nil {
		t.Errorf("expected nil payload for a quiet skip, got %v", payload)
	}
}

func TestReadPropertyReportInstalledExecFailureErrors(t *testing.T) {
	t.Parallel()
	// #329: when the report exec fails but the plugin IS installed, the failure
	// must surface an error rather than a silent drop.
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		switch strings.Join(in.Args, " ") {
		case "--quiet plugin:list":
			return subprocess.ExecCommandResponse{Stdout: "nginx 1.0.0 enabled nginx"}, nil
		case "--quiet nginx:report web --format json":
			return subprocess.ExecCommandResponse{}, errors.New("boom")
		}
		return subprocess.ExecCommandResponse{}, nil
	})

	if _, err := readPropertyReport(ctx, "nginx", "web", false); err == nil {
		t.Error("expected an error when an installed plugin's report fails")
	}
}

func TestUnknownPropertyWarningMissingKey(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
	masker := subprocess.NewMasker()
	masker.Add("s3cr3t")

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
	if masked := masker.String(w.Message); strings.Contains(masked, "s3cr3t") {
		t.Errorf("masked warning leaked secret: %q -> %q", w.Message, masked)
	}
}

func TestUnknownPropertyWarningIgnoresOtherErrors(t *testing.T) {
	t.Parallel()
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

// TestUnknownPropertyWarningDynamicPropertySkipsWarning pins the helper
// directly. Every declared family is probed now (#449, #450), so getProperty
// reads a missing row as unset and no family reaches this branch through
// planProperty any more - it is the guard a plugin that stops reporting a family
// would fall back on, and a missing row must read as "not set yet", not as a
// typo the user should be warned about.
func TestUnknownPropertyWarningDynamicPropertySkipsWarning(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	masker := subprocess.NewMasker()
	keys := map[string]PropertyKeys{
		"hsts": {PerApp: "hsts", Global: ""},
	}
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet nginx:report myapp --format json": `{"proxy-read-timeout":"60s"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), fakePropertyTask("nginx:set", keys), StatePresent, "myapp", false, "hsts", "true")
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
	t.Parallel()
	masker := subprocess.NewMasker()
	keys := map[string]PropertyKeys{
		"hsts": {PerApp: "hsts", Global: ""},
	}
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		resp := subprocess.ExecCommandResponse{
			Stderr:   "Invalid flag passed, valid flags: --app, --global",
			ExitCode: 1,
		}
		return resp, &subprocess.ExecError{Response: resp, Err: errors.New("exit status 1"), Ran: true}
	})

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), fakePropertyTask("nginx:set", keys), StatePresent, "myapp", false, "hsts", "true")
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
	t.Parallel()
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
	t.Parallel()
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
		// traefik's family reports the same way as of dokku 0.38.27, but
		// `traefik:set` refuses it outside --global, so only the global half
		// of the entry is synthesized (#450).
		{"traefik", "dns-provider-CLOUDFLARE_API_TOKEN", PropertyKeys{
			Global:    "global-dns-provider-CLOUDFLARE_API_TOKEN",
			Sensitive: true,
		}, true},
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
	t.Parallel()
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

	// traefik reports the same family, global-only. Its global rows lift, and
	// an app report - which carries the same global- rows, since the traefik
	// report is global state whichever scope it is asked for - lifts nothing,
	// because the family synthesizes no per-app key to round-trip through.
	traefikPayload := map[string]string{
		"global-dns-provider-CLOUDFLARE_API_TOKEN": "token",
		"global-dns-provider":                      "cloudflare",
	}
	got = dynamicPropertiesFromReport("traefik", traefikPayload, true)
	if !reflect.DeepEqual(got, []string{"dns-provider-CLOUDFLARE_API_TOKEN"}) {
		t.Errorf("traefik global scope = %v; want the one credential row", got)
	}
	if got := dynamicPropertiesFromReport("traefik", traefikPayload, false); got != nil {
		t.Errorf("traefik app scope = %v; want nil for a global-only family", got)
	}
}

// TestPlanPropertyDynamicLetsencryptInSync is the core of #449: a
// dns-provider-* credential that already matches the recipe plans as in sync
// instead of reporting drift on every run.
func TestPlanPropertyDynamicLetsencryptInSync(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"email":"admin@example.com","dns-provider-CLOUDFLARE_API_TOKEN":"token123"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), LetsencryptPropertyTask{}, StatePresent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
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
	t.Parallel()
	masker := subprocess.NewMasker()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report --global --format json": `{"global-dns-provider-NAMECHEAP_API_USER":"deploy-bot"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), LetsencryptPropertyTask{}, StatePresent, "", true, "dns-provider-NAMECHEAP_API_USER", "deploy-bot")
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
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"dns-provider-CLOUDFLARE_API_TOKEN":"oldtoken"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), LetsencryptPropertyTask{}, StatePresent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "newtoken")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusModify {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusModify)
	}
	if !strings.Contains(res.Reason, "drift") {
		t.Errorf("reason = %q; want a drift reason", res.Reason)
	}
	if masked := masker.String(res.Reason); strings.Contains(masked, "oldtoken") {
		t.Errorf("drift reason leaked the probed credential: %q -> %q", res.Reason, masked)
	}
	for _, cmd := range res.Commands {
		if masked := masker.String(cmd); strings.Contains(masked, "newtoken") {
			t.Errorf("command leaked the desired credential: %q -> %q", cmd, masked)
		}
	}
}

// TestPlanPropertyDynamicLetsencryptMissingRowPlansCreate covers the pre-set
// state: the plugin only emits a row once the property holds a value, so an
// absent row is an unset property and not a probe failure worth warning about.
func TestPlanPropertyDynamicLetsencryptMissingRowPlansCreate(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"email":"admin@example.com"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), LetsencryptPropertyTask{}, StatePresent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
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
	t.Parallel()
	masker := subprocess.NewMasker()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"email":"admin@example.com"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), LetsencryptPropertyTask{}, StateAbsent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !res.InSync {
		t.Errorf("an already-unset credential must plan as in sync, got status %q reason %q", res.Status, res.Reason)
	}
}

func TestPlanPropertyDynamicLetsencryptAbsentPlansDestroy(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet letsencrypt:report myapp --format json": `{"dns-provider-CLOUDFLARE_API_TOKEN":"livetoken"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), LetsencryptPropertyTask{}, StateAbsent, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusDestroy {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusDestroy)
	}
	if masked := masker.String(res.Reason); strings.Contains(masked, "livetoken") {
		t.Errorf("unset reason leaked the probed credential: %q -> %q", res.Reason, masked)
	}
}

// TestPlanPropertyDynamicTraefikGlobalInSync is the core of #450: dokku 0.38.27
// reports every set `dns-provider-<KEY>` credential as a `global-` row, so one
// that already matches the recipe plans as in sync instead of reporting drift on
// every run.
func TestPlanPropertyDynamicTraefikGlobalInSync(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{"global-log-level":"INFO","global-dns-provider-CLOUDFLARE_API_TOKEN":"token123"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, StatePresent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
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

// TestPlanPropertyDynamicTraefikDriftMasksProbedValue covers the drift half: the
// value read back is a DNS provider credential and must not reach the
// `(was %q)` reason in the clear (#457).
func TestPlanPropertyDynamicTraefikDriftMasksProbedValue(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{"global-dns-provider-CLOUDFLARE_API_TOKEN":"livetoken"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, StatePresent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "newtoken")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusModify {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusModify)
	}
	if masked := masker.String(res.Reason); strings.Contains(masked, "livetoken") {
		t.Errorf("drift reason leaked the probed credential: %q -> %q", res.Reason, masked)
	}
}

// TestPlanPropertyDynamicTraefikMissingRowPlansCreate pins the pre-set state: a
// credential the server has never been given has no report row, which reads as
// unset rather than as a stale key map, so it plans as a create with no
// unknown_property warning.
func TestPlanPropertyDynamicTraefikMissingRowPlansCreate(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{"global-log-level":"INFO"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, StatePresent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "token123")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusCreate {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusCreate)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("an unset dynamic property is not an unknown key: %v", res.Warnings)
	}
}

// TestPlanPropertyDynamicTraefikAbsentMissingRowIsInSync completes the pair:
// `state: absent` on a credential that was never set converges instead of
// running an unset on every apply.
func TestPlanPropertyDynamicTraefikAbsentMissingRowIsInSync(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{"global-log-level":"INFO"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, StateAbsent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if !res.InSync {
		t.Errorf("expected in sync, got status %q reason %q", res.Status, res.Reason)
	}
}

func TestPlanPropertyDynamicTraefikAbsentPlansDestroy(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{"global-dns-provider-CLOUDFLARE_API_TOKEN":"livetoken"}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, StateAbsent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	if res.Status != PlanStatusDestroy {
		t.Errorf("status = %q; want %q", res.Status, PlanStatusDestroy)
	}
	if masked := masker.String(res.Reason); strings.Contains(masked, "livetoken") {
		t.Errorf("unset reason leaked the probed credential: %q -> %q", res.Reason, masked)
	}
}

// TestPlanPropertyDynamicTraefikAppScopeIsRejected pins the other half of #450.
// `traefik:set` refuses a `dns-provider-*` key outside --global, and the family
// says so, so the app scope is turned away before any probe. Without it the
// empty per-app lookup would read as unset: `state: present` would plan create
// forever and `state: absent` would report in sync while never unsetting a live
// credential.
func TestPlanPropertyDynamicTraefikAppScopeIsRejected(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()
	var ran []string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		ran = append(ran, strings.Join(in.Args, " "))
		return subprocess.ExecCommandResponse{}, nil
	})

	for _, tc := range []struct {
		state State
		value string
	}{
		{StatePresent, "token123"},
		{StateAbsent, ""},
	} {
		res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, tc.state, "myapp", false, "dns-provider-CLOUDFLARE_API_TOKEN", tc.value)
		if res.Error == nil {
			t.Fatalf("state %q: expected an error, got status %q reason %q", tc.state, res.Status, res.Reason)
		}
		if !strings.Contains(res.Error.Error(), "no per-app form") {
			t.Errorf("state %q: error = %v; want the no-per-app-form rejection", tc.state, res.Error)
		}
	}
	if len(ran) != 0 {
		t.Errorf("a rejected scope must not reach the server, ran %v", ran)
	}
}

// TestPlanPropertyDynamicTraefikMasksCredential is the core of #457: the desired
// value is a DNS provider credential and must reach the masker before it lands
// in the command echo or the plan mutation line. It stayed true when the family
// could not be probed and has to stay true now that it can.
func TestPlanPropertyDynamicTraefikMasksCredential(t *testing.T) {
	t.Parallel()
	masker := subprocess.NewMasker()

	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet traefik:report --global --format json": `{}`,
	}))

	res := planProperty(subprocess.ContextWithMasker(ctx, masker), TraefikPropertyTask{}, StatePresent, "", true, "dns-provider-CLOUDFLARE_API_TOKEN", "traefiktoken")
	if res.Error != nil {
		t.Fatalf("planProperty error: %v", res.Error)
	}
	// Commands are masked as they are resolved, so a leak here means the value
	// was never registered; mutations are masked by the emitter instead.
	for _, cmd := range res.Commands {
		if masked := masker.String(cmd); strings.Contains(masked, "traefiktoken") {
			t.Errorf("command leaked the credential: %q -> %q", cmd, masked)
		}
	}
	for _, mutation := range res.Mutations {
		if masked := masker.String(mutation); strings.Contains(masked, "traefiktoken") {
			t.Errorf("mutation leaked the credential: %q -> %q", mutation, masked)
		}
	}
}

// TestRunUnprobedPlansMutateUnconditionally covers the half of the Probeable
// contract no declared family reaches any more: a family its plugin does not
// report skips the probe and runs the mutation on every apply. The helpers are
// kept for the next such plugin, so they are exercised directly rather than
// through a family that no longer exists (#450).
func TestRunUnprobedPlansMutateUnconditionally(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithMasker(testCtx(), subprocess.NewMasker())

	set := runUnprobedSet(ctx, "traefik:set", "--global", "secret-TOKEN", "value")
	if set.InSync || set.Status != PlanStatusModify {
		t.Errorf("set = {InSync:%v Status:%q}; want a modify that never converges", set.InSync, set.Status)
	}
	if !strings.Contains(set.Reason, "(no probe key)") {
		t.Errorf("set reason = %q; want the unprobed reason", set.Reason)
	}

	unset := runUnprobedUnset(ctx, "traefik:set", "--global", "secret-TOKEN")
	if unset.InSync || unset.Status != PlanStatusDestroy {
		t.Errorf("unset = {InSync:%v Status:%q}; want a destroy that never converges", unset.InSync, unset.Status)
	}
	if !strings.Contains(unset.Reason, "(no probe key)") {
		t.Errorf("unset reason = %q; want the unprobed reason", unset.Reason)
	}
}

// TestPropertyEntry pins the arms the export path and planProperty read
// sensitivity through: a mapped property answers for itself, and a dynamic
// member is synthesized from its family, in the scopes that family declares.
// The unprobeable arm has no live family left to use, so it is pinned against a
// synthetic one in TestDynamicFamilySensitivityIsIndependentOfProbing (#457).
func TestPropertyEntry(t *testing.T) {
	t.Parallel()
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
			name:     "global-only dynamic member synthesizes only its global key",
			plugin:   "traefik",
			property: "dns-provider-CLOUDFLARE_API_TOKEN",
			keys:     traefikPropertyTable.Keys,
			want: PropertyKeys{
				Global:    "global-dns-provider-CLOUDFLARE_API_TOKEN",
				Sensitive: true,
			},
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
	t.Parallel()
	keys := map[string]PropertyKeys{
		"both":        {PerApp: "both", Global: "global-both"},
		"app-only":    {PerApp: "app-only", Global: ""},
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

// rejectedFamilyTable is a synthetic table that refuses one family, so the
// shared behaviour can be driven without leaning on scheduler-k3s's real one.
func rejectedFamilyTable() propertyTableTask {
	return propertyTableTask{table: PropertyTable{
		Subcommand: "test:set",
		Rejected: []RejectedPropertyFamily{
			{Prefix: "chart.", Replacement: "dokku_other_task", Reason: "another task owns it"},
		},
		Keys: map[string]PropertyKeys{
			"supported": {PerApp: "supported", Global: "global-supported"},
		},
	}}
}

func TestRejectedFamilyFor(t *testing.T) {
	t.Parallel()
	table := rejectedFamilyTable().table
	cases := []struct {
		property string
		want     bool
	}{
		{"chart.traefik.replicas", true},
		{"chart.", true},
		{"chartreuse", false},
		{"supported", false},
		{"", false},
	}
	for _, tc := range cases {
		t.Run(tc.property, func(t *testing.T) {
			family, ok := table.rejectedFamilyFor(tc.property)
			if ok != tc.want {
				t.Fatalf("rejectedFamilyFor(%q) = %v; want %v", tc.property, ok, tc.want)
			}
			if ok && family.Replacement != "dokku_other_task" {
				t.Errorf("rejectedFamilyFor(%q) replacement = %q; want dokku_other_task", tc.property, family.Replacement)
			}
		})
	}
}

// TestValidatePropertyInputRejectedFamily pins the ordering inside
// validatePropertyInput. The rejected family is checked before scoping and
// before the key map, so a member is always answered with the task that owns
// it - never with the supported-name list, and never with a scoping error the
// user would fix only to hit the refusal anyway (#458).
func TestValidatePropertyInputRejectedFamily(t *testing.T) {
	t.Parallel()
	task := rejectedFamilyTable()
	cases := []struct {
		name     string
		app      string
		global   bool
		property string
		value    string
		state    State
		wantErr  string
	}{
		{"member rejected", "some-app", false, "chart.traefik.replicas", "3", StatePresent, "chart.* properties are managed by dokku_other_task; another task owns it"},
		{"outranks missing app", "", false, "chart.traefik.replicas", "3", StatePresent, "managed by dokku_other_task"},
		{"outranks app with global", "some-app", true, "chart.traefik.replicas", "3", StatePresent, "managed by dokku_other_task"},
		{"outranks missing value", "some-app", false, "chart.traefik.replicas", "", StatePresent, "managed by dokku_other_task"},
		{"near miss falls through", "some-app", false, "chartreuse", "3", StatePresent, "unsupported property"},
		{"supported name unaffected", "some-app", false, "supported", "3", StatePresent, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := validatePropertyInput(task, tc.state, tc.app, tc.global, tc.property, tc.value)
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("got error %v; want nil", err)
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

// TestRejectedPropertyFamiliesSorted checks the catalog projection: the
// published order is by prefix regardless of declaration order, so two runs of
// the same binary agree and a reordered declaration is not a wire change.
func TestRejectedPropertyFamiliesSorted(t *testing.T) {
	t.Parallel()
	table := PropertyTable{
		Subcommand: "test:set",
		Rejected: []RejectedPropertyFamily{
			{Prefix: "zeta.", Replacement: "dokku_zeta", Reason: "z"},
			{Prefix: "alpha.", Replacement: "dokku_alpha", Reason: "a"},
		},
	}
	got := RejectedPropertyFamilies(table)
	want := []RejectedPropertySchema{
		{Prefix: "alpha.", Replacement: "dokku_alpha", Reason: "a"},
		{Prefix: "zeta.", Replacement: "dokku_zeta", Reason: "z"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("RejectedPropertyFamilies = %+v; want %+v", got, want)
	}
	if families := RejectedPropertyFamilies(PropertyTable{Subcommand: "test:set"}); families != nil {
		t.Errorf("a table with no rejected families = %+v; want nil", families)
	}
}

func TestValidatePropertyDynamic(t *testing.T) {
	t.Parallel()
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
