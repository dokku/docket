package tasks

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

func TestCertsTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", Key: "/tmp/key", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestCertsTaskMissingApp(t *testing.T) {
	t.Parallel()
	task := CertsTask{Cert: "/tmp/cert", Key: "/tmp/key", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskGlobalWithApp(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", Global: true, Cert: "/tmp/cert", Key: "/tmp/key", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskPresentMissingCert(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", Key: "/tmp/key", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without cert should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' (or 'cert_content') and 'key' (or 'key_content') are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskPresentMissingKey(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without key should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' (or 'cert_content') and 'key' (or 'key_content') are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskInlineMissingKeyContent(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", CertContent: "cert-pem", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with cert_content but no key should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' (or 'cert_content') and 'key' (or 'key_content') are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskInlineMixedSources(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", KeyContent: "key-pem", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with cert + key_content should return a validation error")
	}
	if !strings.Contains(result.Error.Error(), "cannot be mixed") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskInlineBothCertForms(t *testing.T) {
	t.Parallel()
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", CertContent: "cert-pem", Key: "/tmp/key", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with both cert and cert_content should return a validation error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' and 'cert_content' are mutually exclusive") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuildCertTarball(t *testing.T) {
	t.Parallel()
	certPEM := "-----BEGIN CERTIFICATE-----\nfake-cert\n-----END CERTIFICATE-----\n"
	keyPEM := "-----BEGIN PRIVATE KEY-----\nfake-key\n-----END PRIVATE KEY-----\n"

	out, err := buildCertTarball(certPEM, keyPEM)
	if err != nil {
		t.Fatalf("buildCertTarball failed: %v", err)
	}

	tr := tar.NewReader(bytes.NewReader(out))
	got := map[string]string{}
	modes := map[string]int64{}
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("tar read failed: %v", err)
		}
		body, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("tar entry body read failed: %v", err)
		}
		got[hdr.Name] = string(body)
		modes[hdr.Name] = hdr.Mode
	}
	if got["server.crt"] != certPEM {
		t.Errorf("server.crt body = %q, want %q", got["server.crt"], certPEM)
	}
	if got["server.key"] != keyPEM {
		t.Errorf("server.key body = %q, want %q", got["server.key"], keyPEM)
	}
	if modes["server.crt"] != 0o600 {
		t.Errorf("server.crt mode = %o, want 0600", modes["server.crt"])
	}
	if modes["server.key"] != 0o600 {
		t.Errorf("server.key mode = %o, want 0600", modes["server.key"])
	}
}

func TestGetTasksCertsTaskInlineParsedCorrectly(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: install cert
      dokku_certs:
        app: test-app
        cert_content: |
          -----BEGIN CERTIFICATE-----
          fake
          -----END CERTIFICATE-----
        key_content: |
          -----BEGIN PRIVATE KEY-----
          fake
          -----END PRIVATE KEY-----
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("install cert")
	if task == nil {
		t.Fatal("task 'install cert' not found")
	}

	certsTask, ok := task.(*CertsTask)
	if !ok {
		t.Fatalf("task is not a CertsTask (type is %T)", task)
	}
	if certsTask.App != "test-app" {
		t.Errorf("App = %q, want %q", certsTask.App, "test-app")
	}
	if !strings.Contains(certsTask.CertContent, "BEGIN CERTIFICATE") {
		t.Errorf("CertContent missing PEM marker: %q", certsTask.CertContent)
	}
	if !strings.Contains(certsTask.KeyContent, "BEGIN PRIVATE KEY") {
		t.Errorf("KeyContent missing PEM marker: %q", certsTask.KeyContent)
	}
	if certsTask.Cert != "" || certsTask.Key != "" {
		t.Errorf("expected path fields empty, got cert=%q key=%q", certsTask.Cert, certsTask.Key)
	}
}

func TestGetTasksCertsTaskParsedCorrectly(t *testing.T) {
	t.Parallel()
	data := []byte(`---
- tasks:
    - name: install cert
      dokku_certs:
        app: test-app
        cert: /etc/ssl/test-app.crt
        key: /etc/ssl/test-app.key
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("install cert")
	if task == nil {
		t.Fatal("task 'install cert' not found")
	}

	certsTask, ok := task.(*CertsTask)
	if !ok {
		t.Fatalf("task is not a CertsTask (type is %T)", task)
	}
	if certsTask.App != "test-app" {
		t.Errorf("App = %q, want %q", certsTask.App, "test-app")
	}
	if certsTask.Cert != "/etc/ssl/test-app.crt" {
		t.Errorf("Cert = %q, want %q", certsTask.Cert, "/etc/ssl/test-app.crt")
	}
	if certsTask.Key != "/etc/ssl/test-app.key" {
		t.Errorf("Key = %q, want %q", certsTask.Key, "/etc/ssl/test-app.key")
	}
	if certsTask.State != StatePresent {
		t.Errorf("State = %q, want %q", certsTask.State, StatePresent)
	}
}

// TestCertsEnabledGlobalUsesGlobalScope locks the global certsEnabled probe to
// the `--global` report scope. dokku-global-cert standardized
// global-cert:report so a bare `--global-cert-enabled` flag now reports
// per-app; only `--global` targets the global certificate itself.
func TestCertsEnabledGlobalUsesGlobalScope(t *testing.T) {
	t.Parallel()
	var gotArgs []string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		gotArgs = in.Args
		return subprocess.ExecCommandResponse{Stdout: "true"}, nil
	})

	enabled, err := certsEnabled(ctx, CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled: %v", err)
	}
	if !enabled {
		t.Errorf("expected enabled=true when report returns \"true\"")
	}
	want := "--quiet global-cert:report --global --global-cert-enabled"
	if got := strings.Join(gotArgs, " "); got != want {
		t.Errorf("global certsEnabled args = %q, want %q", got, want)
	}
}

// certPEM renders a PEM block whose DER payload is the given marker, which is
// all samePEM and the plan comparison read - neither parses X.509. Building the
// fixture with encoding/pem rather than pasting a literal keeps the tests fast
// and makes the line wrapping the real thing.
func certPEM(body string) string {
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte(body)}))
}

// fingerprintOf renders the colon-separated uppercase SHA-256 of the first PEM
// block's DER in text, which is the shape dokku's certs:report reports. It walks
// the document itself rather than calling desiredCertFingerprint so a fixture
// stating what the server holds is not derived from the code under test.
func fingerprintOf(pemText string) string {
	block, _ := pem.Decode([]byte(pemText))
	if block == nil {
		return ""
	}
	sum := sha256.Sum256(block.Bytes)
	upper := strings.ToUpper(hex.EncodeToString(sum[:]))
	pairs := make([]string, 0, len(upper)/2)
	for i := 0; i < len(upper); i += 2 {
		pairs = append(pairs, upper[i:i+2])
	}
	return strings.Join(pairs, ":")
}

// certsReportJSON renders a certs:report --format json payload. The certs plugin
// strips the `ssl-` prefix from JSON keys, so the digest lands under
// `fingerprint`.
func certsReportJSON(fingerprint string) string {
	return fmt.Sprintf(`{"enabled":"true","fingerprint":%q}`, fingerprint)
}

// certsAppFixture answers the reads the app-scope present branch makes: a
// certificate is installed, letsencrypt does not manage it, certs:report carries
// the installed certificate's fingerprint, and certs:show hands back installed
// for anything the fingerprint cannot settle.
func certsAppFixture(app, installed string) map[string]string {
	fixture := certsAppFixtureNoFingerprint(app, installed)
	fixture["--quiet certs:report "+app+" --format json"] = certsReportJSON(fingerprintOf(installed))
	return fixture
}

// certsAppFixtureNoFingerprint is certsAppFixture against a dokku whose
// certs:report carries no fingerprint key, which is the fall-back to certs:show.
func certsAppFixtureNoFingerprint(app, installed string) map[string]string {
	return map[string]string{
		"--quiet certs:report " + app + " --ssl-enabled": "true",
		"--quiet letsencrypt:active " + app:              "false",
		"--quiet certs:report " + app + " --format json": `{"enabled":"true"}`,
		"--quiet certs:show " + app + " crt":             installed,
	}
}

// globalCertReportJSON renders a global-cert:report --global --format json
// payload. The plugin strips the `--global-cert-` prefix from JSON keys, so its
// digest lands under `fingerprint`, the same key core's certs:report uses.
func globalCertReportJSON(fingerprint string) string {
	return fmt.Sprintf(`{"dir":"/home/dokku/tls","enabled":"true","fingerprint":%q,"serial":"322844AD8CD6D4FF"}`, fingerprint)
}

// certsGlobalFixture answers the reads the global-scope present branch makes: a
// global certificate is installed, global-cert:report carries its fingerprint,
// and global-cert:show hands back installed for anything the fingerprint cannot
// settle. There is no letsencrypt entry: a global plan must not ask.
func certsGlobalFixture(installed string) map[string]string {
	fixture := certsGlobalFixtureNoFingerprint(installed)
	fixture["--quiet global-cert:report --global --format json"] = globalCertReportJSON(fingerprintOf(installed))
	return fixture
}

// certsGlobalFixtureNoFingerprint is certsGlobalFixture against a
// dokku-global-cert older than 0.7.0, whose report carries no fingerprint key,
// which is the fall-back to global-cert:show.
func certsGlobalFixtureNoFingerprint(installed string) map[string]string {
	return map[string]string{
		"--quiet global-cert:report --global --global-cert-enabled": "true",
		"--quiet global-cert:report --global --format json":         `{"dir":"/home/dokku/tls","enabled":"true"}`,
		"--quiet global-cert:show crt":                              installed,
	}
}

// globalCertTask is the recipe the global-scope plan tests exercise: a pinned
// certificate and its key, inline so the material is readable on every
// transport.
func globalCertTask(desired string) CertsTask {
	return CertsTask{
		Global:      true,
		CertContent: desired,
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nglobal-key\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}
}

// assertNoCertMaterial fails when any plan-visible string carries PEM material.
// The cert and key are sensitive and ride to dokku on stdin, so neither the
// rendered commands, the itemized mutations nor the reason may name them.
func assertNoCertMaterial(t *testing.T, plan PlanResult) {
	t.Helper()
	for _, s := range append(append([]string{plan.Reason}, plan.Mutations...), plan.Commands...) {
		if strings.Contains(s, "BEGIN CERTIFICATE") || strings.Contains(s, "BEGIN PRIVATE KEY") {
			t.Errorf("certificate material leaked into plan output: %q", s)
		}
	}
}

func TestCertsTaskPlanInlineMatchingCertInSync(t *testing.T) {
	t.Parallel()
	installed := certPEM("cert-a")
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsAppFixture("test-app", installed), &calls))

	plan := CertsTask{
		App:         "test-app",
		CertContent: installed,
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-a\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	// The certificate answers the question on its own; reading the key back
	// would move private material off the server for nothing.
	for _, call := range calls {
		if strings.HasSuffix(call, "certs:show test-app key") {
			t.Errorf("plan read the private key back: %q", call)
		}
	}
}

func TestCertsTaskPlanInlineRotatedCertPlansModify(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(certsAppFixture("test-app", certPEM("cert-old"))))

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("cert-renewed"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-renewed\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	if plan.Reason != "certificate material drift" {
		t.Errorf("Reason = %q, want %q", plan.Reason, "certificate material drift")
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"replace certificate for test-app"}) {
		t.Errorf("Mutations = %v, want [replace certificate for test-app]", plan.Mutations)
	}
	// certs:add is also certs:update in dokku, so replacing needs no second command.
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "certs:add test-app") {
		t.Errorf("Commands = %v, want one command ending in certs:add test-app", plan.Commands)
	}
	assertNoCertMaterial(t, plan)
}

// TestCertsTaskPlanNormalizesPEM locks the comparison to the decoded block. The
// digest is taken over the DER a PEM block decodes to, never over the text
// around it, so an inline cert_content that ends in a newline - which every PEM
// file does - does not plan as drift forever against the certificate it just
// installed. The same holds on the certs:show fall-back, where samePEM compares
// decoded blocks for the same reason.
func TestCertsTaskPlanNormalizesPEM(t *testing.T) {
	t.Parallel()
	installed := strings.TrimSpace(certPEM("cert-a"))

	tests := []struct {
		name    string
		desired string
	}{
		{name: "trailing newline", desired: installed + "\n"},
		{name: "trailing blank lines", desired: installed + "\n\n\n"},
		{name: "crlf line endings", desired: strings.ReplaceAll(installed, "\n", "\r\n") + "\r\n"},
		{name: "leading text dump", desired: "Certificate:\n    Serial Number: 1\n" + installed + "\n"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(certsAppFixture("test-app", installed)))

			plan := CertsTask{
				App:         "test-app",
				CertContent: tc.desired,
				KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-a\n-----END PRIVATE KEY-----\n",
				State:       StatePresent,
			}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if !plan.InSync {
				t.Errorf("plan reported drift for whitespace-only differences (status %q)", plan.Status)
			}
		})
	}
}

// TestCertsTaskPlanPathFormComparesLocalFile covers the `cert:` form on a local
// run, where the path dokku resolves is this machine's path too, so the file is
// the desired material.
func TestCertsTaskPlanPathFormComparesLocalFile(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	keyPath := filepath.Join(dir, "server.key")
	if err := os.WriteFile(certPath, []byte(certPEM("cert-on-disk")), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}
	if err := os.WriteFile(keyPath, []byte("-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n"), 0o600); err != nil {
		t.Fatalf("write key: %v", err)
	}

	tests := []struct {
		name       string
		installed  string
		wantInSync bool
		wantStatus PlanStatus
	}{
		{name: "same certificate", installed: certPEM("cert-on-disk"), wantInSync: true, wantStatus: PlanStatusOK},
		{name: "renewed certificate", installed: certPEM("cert-superseded"), wantStatus: PlanStatusModify},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(certsAppFixture("test-app", tc.installed)))

			plan := CertsTask{App: "test-app", Cert: certPath, Key: keyPath, State: StatePresent}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync != tc.wantInSync || plan.Status != tc.wantStatus {
				t.Fatalf("plan = {InSync:%v Status:%q}, want {InSync:%v Status:%q}",
					plan.InSync, plan.Status, tc.wantInSync, tc.wantStatus)
			}
		})
	}
}

// TestCertsTaskPlanPathFormRemoteSkipsComparison locks the one case the probe
// must not guess at: `cert:` names a file on the dokku host, so under --host the
// desired material is not readable here and a same-named local file is a
// different file. The task keeps its coarse "installed means in sync" answer
// rather than planning drift against a server that may well match.
func TestCertsTaskPlanPathFormRemoteSkipsComparison(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	certPath := filepath.Join(dir, "server.crt")
	if err := os.WriteFile(certPath, []byte(certPEM("a-different-local-file")), 0o600); err != nil {
		t.Fatalf("write cert: %v", err)
	}

	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsAppFixture("test-app", certPEM("cert-on-server")), &calls))
	ctx = subprocess.ContextWithTarget(ctx, subprocess.Target{Host: "dokku.example.com"})

	plan := CertsTask{
		App:   "test-app",
		Cert:  certPath,
		Key:   filepath.Join(dir, "server.key"),
		State: StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	for _, call := range calls {
		if strings.Contains(call, "certs:show") || strings.Contains(call, "--format json") {
			t.Errorf("plan probed the certificate with nothing to compare it to: %q", call)
		}
	}
}

// TestCertsTaskPlanLetsencryptManagedSkipsComparison mirrors the export skip
// (#337): a letsencrypt certificate is re-issued on renewal, so comparing it
// against a pinned one would have docket overwrite a fresh certificate with a
// stale one on every run.
func TestCertsTaskPlanLetsencryptManagedSkipsComparison(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(map[string]string{
		"--quiet certs:report test-app --ssl-enabled": "true",
		"--quiet letsencrypt:active test-app":         "true",
		"--quiet certs:show test-app crt":             certPEM("letsencrypt-issued"),
	}, &calls))

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("pinned"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	for _, call := range calls {
		if strings.Contains(call, "certs:show") || strings.Contains(call, "--format json") {
			t.Errorf("plan compared a letsencrypt-managed certificate: %q", call)
		}
	}
}

func TestCertsTaskPlanNotInstalledPlansCreate(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(map[string]string{
		"--quiet certs:report test-app --ssl-enabled": "false",
	}, &calls))

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("cert-a"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-a\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusCreate {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusCreate)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"install certificate for test-app"}) {
		t.Errorf("Mutations = %v, want [install certificate for test-app]", plan.Mutations)
	}
	// Nothing is installed, so there is nothing to read back.
	for _, call := range calls {
		if strings.Contains(call, "certs:show") || strings.Contains(call, "letsencrypt:active") {
			t.Errorf("plan probed material for an app with no certificate: %q", call)
		}
	}
	assertNoCertMaterial(t, plan)
}

// TestCertsTaskPlanShowFailureIsProbeError keeps a failed read-back an error
// rather than a silent "in sync", matching how the same branch already treats a
// certs:report failure. The report reports no fingerprint, which is what puts
// the branch on certs:show in the first place.
func TestCertsTaskPlanShowFailureIsProbeError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if strings.Contains(joined, "certs:show") {
			return subprocess.ExecCommandResponse{ExitCode: 1}, errors.New("test-app doesn't have an SSL endpoint defined")
		}
		if strings.Contains(joined, "--format json") {
			return subprocess.ExecCommandResponse{Stdout: `{"enabled":"true"}`}, nil
		}
		if strings.Contains(joined, "certs:report") {
			return subprocess.ExecCommandResponse{Stdout: "true"}, nil
		}
		return subprocess.ExecCommandResponse{Stdout: "false"}, nil
	})

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("cert-a"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-a\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error == nil {
		t.Fatal("expected a probe error when certs:show fails")
	}
	if plan.Status != PlanStatusError {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusError)
	}
}

func TestSamePEM(t *testing.T) {
	t.Parallel()
	certA := certPEM("cert-a")
	certB := certPEM("cert-b")

	tests := []struct {
		name string
		a    string
		b    string
		want bool
	}{
		{name: "identical", a: certA, b: certA, want: true},
		{name: "trailing newline", a: certA, b: strings.TrimSpace(certA), want: true},
		{name: "crlf endings", a: certA, b: strings.ReplaceAll(certA, "\n", "\r\n"), want: true},
		{name: "text outside the block", a: certA, b: "subject=/CN=example.com\n" + certA, want: true},
		{name: "different certificate", a: certA, b: certB},
		{name: "chain against leaf", a: certA, b: certA + certB},
		{name: "chain order", a: certA + certB, b: certB + certA},
		{name: "different block type", a: certA, b: string(pem.EncodeToMemory(&pem.Block{Type: "TRUSTED CERTIFICATE", Bytes: []byte("cert-a")}))},
		{name: "neither is pem", a: "not a certificate\n", b: "not a certificate", want: true},
		{name: "one side is not pem", a: certA, b: "not a certificate"},
		{name: "both empty", a: "", b: "", want: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := samePEM(tc.a, tc.b); got != tc.want {
				t.Errorf("samePEM = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCertsTaskPlanFingerprintSettlesWithoutReadingTheCertificate is #529 in one
// assertion: an app whose installed certificate matches the pinned one is
// settled by the digest certs:report carries, so no certificate material leaves
// the server at plan time.
func TestCertsTaskPlanFingerprintSettlesWithoutReadingTheCertificate(t *testing.T) {
	t.Parallel()
	installed := certPEM("cert-a")
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsAppFixture("test-app", installed), &calls))

	plan := CertsTask{
		App:         "test-app",
		CertContent: installed,
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-a\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	assertNoCertsShow(t, calls)
}

// TestCertsTaskPlanFingerprintMismatchPlansModify covers the other half: a
// renewed certificate is caught by the digest alone, still without a read-back.
func TestCertsTaskPlanFingerprintMismatchPlansModify(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsAppFixture("test-app", certPEM("cert-old")), &calls))

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("cert-renewed"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey-renewed\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	if plan.Reason != "certificate material drift" {
		t.Errorf("Reason = %q, want %q", plan.Reason, "certificate material drift")
	}
	assertNoCertsShow(t, calls)
	assertNoCertMaterial(t, plan)
}

// TestCertsTaskPlanMissingFingerprintFallsBackToShow covers a dokku whose
// certs:report carries no fingerprint key at all: the comparison is the exact
// one #525 shipped, so the verdict is unchanged in both directions.
func TestCertsTaskPlanMissingFingerprintFallsBackToShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		desired    string
		wantInSync bool
		wantStatus PlanStatus
	}{
		{name: "same certificate", desired: certPEM("cert-a"), wantInSync: true, wantStatus: PlanStatusOK},
		{name: "renewed certificate", desired: certPEM("cert-renewed"), wantStatus: PlanStatusModify},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(),
				recordingDokku(certsAppFixtureNoFingerprint("test-app", certPEM("cert-a")), &calls))

			plan := CertsTask{
				App:         "test-app",
				CertContent: tc.desired,
				KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
				State:       StatePresent,
			}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync != tc.wantInSync || plan.Status != tc.wantStatus {
				t.Fatalf("plan = {InSync:%v Status:%q}, want {InSync:%v Status:%q}",
					plan.InSync, plan.Status, tc.wantInSync, tc.wantStatus)
			}
			assertCertsShowRan(t, calls)
		})
	}
}

// TestCertsTaskPlanUnusableFingerprintFallsBackToShow keeps a reported value
// that is not a SHA-256 digest from being compared. Comparing one could never
// match, so every run would plan drift and every apply would reinstall the same
// certificate; reading it back instead both answers correctly and terminates.
func TestCertsTaskPlanUnusableFingerprintFallsBackToShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		reported string
	}{
		{name: "empty, as openssl failing to read server.crt reports", reported: ""},
		{name: "sha1 length", reported: "B7:DF:D5:84:C6:2E:27:BF:12:34:56:78:9A:BC:DE:F0:12:34:56:78"},
		{name: "not hex", reported: strings.Repeat("z", 64)},
		{name: "still labelled", reported: "sha256 Fingerprint=" + fingerprintOf(certPEM("cert-a"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			installed := certPEM("cert-a")
			fixture := certsAppFixture("test-app", installed)
			fixture["--quiet certs:report test-app --format json"] = certsReportJSON(tc.reported)

			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(fixture, &calls))

			plan := CertsTask{
				App:         "test-app",
				CertContent: installed,
				KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
				State:       StatePresent,
			}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if !plan.InSync || plan.Status != PlanStatusOK {
				t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
			}
			assertCertsShowRan(t, calls)
		})
	}
}

// TestCertsTaskPlanReportFailureFallsBackToShow keeps a dokku-level report
// failure out of the plan. The read-back answers the question exactly, so there
// is nothing to surface - and nothing to leak, since the probe's stderr never
// reaches the plan.
func TestCertsTaskPlanReportFailureFallsBackToShow(t *testing.T) {
	t.Parallel()
	installed := certPEM("cert-a")
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "--format json"):
			response := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "Invalid flag passed, valid flags: --ssl-dir, --ssl-enabled"}
			return response, &subprocess.ExecError{Response: response, Err: errors.New(response.Stderr), Ran: true}
		case strings.Contains(joined, "certs:show"):
			return subprocess.ExecCommandResponse{Stdout: installed}, nil
		case strings.Contains(joined, "--ssl-enabled"):
			return subprocess.ExecCommandResponse{Stdout: "true"}, nil
		}
		return subprocess.ExecCommandResponse{Stdout: "false"}, nil
	})

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("cert-renewed"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	if strings.Contains(plan.Reason, "Invalid flag") {
		t.Errorf("Reason carried the probe's stderr: %q", plan.Reason)
	}
	assertCertsShowRan(t, calls)
}

// TestCertsTaskPlanReportSSHErrorIsProbeError keeps a transport failure an
// error. A server docket cannot reach is not a server that is in sync, and
// falling back would only reach for the same unreachable host again.
func TestCertsTaskPlanReportSSHErrorIsProbeError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if strings.Contains(joined, "--format json") {
			return subprocess.ExecCommandResponse{ExitCode: 255}, &subprocess.SSHError{
				Host:   "dokku@unreachable",
				Stderr: "ssh: connect to host unreachable port 22: Connection refused",
			}
		}
		if strings.Contains(joined, "--ssl-enabled") {
			return subprocess.ExecCommandResponse{Stdout: "true"}, nil
		}
		return subprocess.ExecCommandResponse{Stdout: "false"}, nil
	})

	plan := CertsTask{
		App:         "test-app",
		CertContent: certPEM("cert-a"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Status != PlanStatusError {
		t.Fatalf("Status = %q, want %q", plan.Status, PlanStatusError)
	}
	var sshErr *subprocess.SSHError
	if !errors.As(plan.Error, &sshErr) {
		t.Errorf("plan.Error = %v, want an *subprocess.SSHError", plan.Error)
	}
}

// TestCertsTaskPlanChainRecipeComparesWholePEM keeps a recipe pinning a chain on
// the exact comparison. dokku digests the first certificate in server.crt and
// ignores the rest, so settling a chain by fingerprint would stop reporting a
// changed intermediate - a narrowing the recipe never asked for.
func TestCertsTaskPlanChainRecipeComparesWholePEM(t *testing.T) {
	t.Parallel()
	leaf := certPEM("leaf")
	installedChain := leaf + certPEM("intermediate-old")

	tests := []struct {
		name       string
		desired    string
		wantInSync bool
		wantStatus PlanStatus
	}{
		{name: "same chain", desired: installedChain, wantInSync: true, wantStatus: PlanStatusOK},
		{name: "same leaf, rotated intermediate", desired: leaf + certPEM("intermediate-new"), wantStatus: PlanStatusModify},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsAppFixture("test-app", installedChain), &calls))

			plan := CertsTask{
				App:         "test-app",
				CertContent: tc.desired,
				KeyContent:  "-----BEGIN PRIVATE KEY-----\nkey\n-----END PRIVATE KEY-----\n",
				State:       StatePresent,
			}.Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync != tc.wantInSync || plan.Status != tc.wantStatus {
				t.Fatalf("plan = {InSync:%v Status:%q}, want {InSync:%v Status:%q}",
					plan.InSync, plan.Status, tc.wantInSync, tc.wantStatus)
			}
			// The report is not even asked: a leaf digest cannot answer for a chain.
			assertNoFingerprintReport(t, calls)
			assertCertsShowRan(t, calls)
		})
	}
}

// TestCertsReportFingerprintReadsTheScopesReport locks the command each scope's
// fingerprint read issues. The global form has to name the `--global` scope for
// the reason certsEnabled does: dokku-global-cert standardized its report, so a
// bare info flag reports per-app and only `--global` targets the global
// certificate itself.
func TestCertsReportFingerprintReadsTheScopesReport(t *testing.T) {
	t.Parallel()
	installed := certPEM("cert-a")

	tests := []struct {
		name    string
		task    CertsTask
		payload string
		want    string
	}{
		{
			name:    "app",
			task:    CertsTask{App: "test-app"},
			payload: certsReportJSON(fingerprintOf(installed)),
			want:    "--quiet certs:report test-app --format json",
		},
		{
			name:    "global",
			task:    CertsTask{Global: true},
			payload: globalCertReportJSON(fingerprintOf(installed)),
			want:    "--quiet global-cert:report --global --format json",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var gotArgs []string
			ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
				gotArgs = in.Args
				return subprocess.ExecCommandResponse{Stdout: tc.payload}, nil
			})

			reported, ok, err := certsReportFingerprint(ctx, tc.task)
			if err != nil {
				t.Fatalf("certsReportFingerprint: %v", err)
			}
			if !ok {
				t.Fatal("expected the report to carry a fingerprint")
			}
			if want := fingerprintOf(installed); reported != want {
				t.Errorf("reported fingerprint = %q, want %q", reported, want)
			}
			if got := strings.Join(gotArgs, " "); got != tc.want {
				t.Errorf("certsReportFingerprint args = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestCertsTaskPlanGlobalFingerprintSettlesWithoutReadingTheCertificate is #548
// in one assertion: the global certificate is settled by the digest
// global-cert:report carries (dokku-global-cert 0.7.0), so it stays on the
// server at plan time - on a command whose other argument hands back the
// private key.
func TestCertsTaskPlanGlobalFingerprintSettlesWithoutReadingTheCertificate(t *testing.T) {
	t.Parallel()
	installed := certPEM("global-a")
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsGlobalFixture(installed), &calls))

	plan := globalCertTask(installed).Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync || plan.Status != PlanStatusOK {
		t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
	}
	assertNoCertsShow(t, calls)
	assertNoLetsencryptProbe(t, calls)
}

// TestCertsTaskPlanGlobalFingerprintMismatchPlansModify covers the other half:
// a renewed global certificate is caught by the digest alone, still without a
// read-back.
func TestCertsTaskPlanGlobalFingerprintMismatchPlansModify(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsGlobalFixture(certPEM("global-old")), &calls))

	plan := globalCertTask(certPEM("global-renewed")).Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	if plan.Reason != "certificate material drift" {
		t.Errorf("Reason = %q, want %q", plan.Reason, "certificate material drift")
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"replace certificate for (global)"}) {
		t.Errorf("Mutations = %v, want [replace certificate for (global)]", plan.Mutations)
	}
	// global-cert:set overwrites what is there, so replacing needs no second command.
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "global-cert:set") {
		t.Errorf("Commands = %v, want one command ending in global-cert:set", plan.Commands)
	}
	assertNoCertsShow(t, calls)
	assertNoLetsencryptProbe(t, calls)
	assertNoCertMaterial(t, plan)
}

// TestCertsTaskPlanGlobalMissingFingerprintFallsBackToShow covers a
// dokku-global-cert older than 0.7.0, whose report carries no fingerprint key at
// all: the comparison is the exact global-cert:show one the global scope always
// made, so the verdict is unchanged in both directions.
func TestCertsTaskPlanGlobalMissingFingerprintFallsBackToShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name       string
		desired    string
		wantInSync bool
		wantStatus PlanStatus
	}{
		{name: "same certificate", desired: certPEM("global-a"), wantInSync: true, wantStatus: PlanStatusOK},
		{name: "renewed certificate", desired: certPEM("global-renewed"), wantStatus: PlanStatusModify},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(),
				recordingDokku(certsGlobalFixtureNoFingerprint(certPEM("global-a")), &calls))

			plan := globalCertTask(tc.desired).Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync != tc.wantInSync || plan.Status != tc.wantStatus {
				t.Fatalf("plan = {InSync:%v Status:%q}, want {InSync:%v Status:%q}",
					plan.InSync, plan.Status, tc.wantInSync, tc.wantStatus)
			}
			assertCertsShowRan(t, calls)
			assertNoLetsencryptProbe(t, calls)
		})
	}
}

// TestCertsTaskPlanGlobalUnusableFingerprintFallsBackToShow keeps a reported
// value that is not a SHA-256 digest from being compared in the global scope
// either. The empty case is what the report hands back when openssl cannot read
// server.crt; comparing it would plan drift on every run and reinstall the same
// certificate forever.
func TestCertsTaskPlanGlobalUnusableFingerprintFallsBackToShow(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name     string
		reported string
	}{
		{name: "empty, as openssl failing to read server.crt reports", reported: ""},
		{name: "sha1 length", reported: "B7:DF:D5:84:C6:2E:27:BF:12:34:56:78:9A:BC:DE:F0:12:34:56:78"},
		{name: "still labelled", reported: "sha256 Fingerprint=" + fingerprintOf(certPEM("global-a"))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			installed := certPEM("global-a")
			fixture := certsGlobalFixture(installed)
			fixture["--quiet global-cert:report --global --format json"] = globalCertReportJSON(tc.reported)

			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(fixture, &calls))

			plan := globalCertTask(installed).Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if !plan.InSync || plan.Status != PlanStatusOK {
				t.Fatalf("plan = {InSync:%v Status:%q}, want an in-sync ok", plan.InSync, plan.Status)
			}
			assertCertsShowRan(t, calls)
		})
	}
}

// TestCertsTaskPlanGlobalReportFailureFallsBackToShow keeps a plugin-level
// failure out of the plan. A dokku-global-cert too old to know `--format`
// rejects the invocation outright; the read-back answers the question exactly,
// so there is nothing to surface - and nothing to leak, since the probe's
// stderr never reaches the plan.
func TestCertsTaskPlanGlobalReportFailureFallsBackToShow(t *testing.T) {
	t.Parallel()
	installed := certPEM("global-a")
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		calls = append(calls, joined)
		switch {
		case strings.Contains(joined, "--format json"):
			response := subprocess.ExecCommandResponse{ExitCode: 1, Stderr: "Invalid flag passed, valid flags: --global-cert-dir, --global-cert-enabled"}
			return response, &subprocess.ExecError{Response: response, Err: errors.New(response.Stderr), Ran: true}
		case strings.Contains(joined, "global-cert:show"):
			return subprocess.ExecCommandResponse{Stdout: installed}, nil
		case strings.Contains(joined, "--global-cert-enabled"):
			return subprocess.ExecCommandResponse{Stdout: "true"}, nil
		}
		return subprocess.ExecCommandResponse{Stdout: "false"}, nil
	})

	plan := globalCertTask(certPEM("global-renewed")).Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	if strings.Contains(plan.Reason, "Invalid flag") {
		t.Errorf("Reason carried the probe's stderr: %q", plan.Reason)
	}
	assertCertsShowRan(t, calls)
}

// TestCertsTaskPlanGlobalReportSSHErrorIsProbeError keeps a transport failure an
// error in the global scope too. A server docket cannot reach is not a server
// that is in sync, and falling back would only reach for the same unreachable
// host again.
func TestCertsTaskPlanGlobalReportSSHErrorIsProbeError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if strings.Contains(joined, "--format json") {
			return subprocess.ExecCommandResponse{ExitCode: 255}, &subprocess.SSHError{
				Host:   "dokku@unreachable",
				Stderr: "ssh: connect to host unreachable port 22: Connection refused",
			}
		}
		if strings.Contains(joined, "--global-cert-enabled") {
			return subprocess.ExecCommandResponse{Stdout: "true"}, nil
		}
		return subprocess.ExecCommandResponse{Stdout: "false"}, nil
	})

	plan := globalCertTask(certPEM("global-a")).Plan(ctx)
	if plan.Status != PlanStatusError {
		t.Fatalf("Status = %q, want %q", plan.Status, PlanStatusError)
	}
	var sshErr *subprocess.SSHError
	if !errors.As(plan.Error, &sshErr) {
		t.Errorf("plan.Error = %v, want an *subprocess.SSHError", plan.Error)
	}
}

// TestCertsTaskPlanGlobalChainRecipeComparesWholePEM keeps a global recipe
// pinning a chain on the exact comparison. dokku-global-cert digests the first
// certificate in server.crt and ignores the rest, so settling a chain by
// fingerprint would stop reporting a changed intermediate - a narrowing the
// recipe never asked for.
func TestCertsTaskPlanGlobalChainRecipeComparesWholePEM(t *testing.T) {
	t.Parallel()
	leaf := certPEM("global-leaf")
	installedChain := leaf + certPEM("global-intermediate-old")

	tests := []struct {
		name       string
		desired    string
		wantInSync bool
		wantStatus PlanStatus
	}{
		{name: "same chain", desired: installedChain, wantInSync: true, wantStatus: PlanStatusOK},
		{name: "same leaf, rotated intermediate", desired: leaf + certPEM("global-intermediate-new"), wantStatus: PlanStatusModify},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var calls []string
			ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(certsGlobalFixture(installedChain), &calls))

			plan := globalCertTask(tc.desired).Plan(ctx)
			if plan.Error != nil {
				t.Fatalf("unexpected plan error: %v", plan.Error)
			}
			if plan.InSync != tc.wantInSync || plan.Status != tc.wantStatus {
				t.Fatalf("plan = {InSync:%v Status:%q}, want {InSync:%v Status:%q}",
					plan.InSync, plan.Status, tc.wantInSync, tc.wantStatus)
			}
			// The report is not even asked: a leaf digest cannot answer for a chain.
			assertNoFingerprintReport(t, calls)
			assertCertsShowRan(t, calls)
		})
	}
}

// TestDesiredCertFingerprint locks what the digest is taken over and what is
// refused. It is the DER a block decodes to, never the PEM text, which is what
// makes it the value openssl prints for the same certificate.
func TestDesiredCertFingerprint(t *testing.T) {
	t.Parallel()

	sum := sha256.Sum256([]byte("cert-a"))
	wantCertA := strings.ToUpper(hex.EncodeToString(sum[:]))

	tests := []struct {
		name  string
		input string
		want  string
		ok    bool
	}{
		{name: "single certificate", input: certPEM("cert-a"), want: wantCertA, ok: true},
		{name: "trailing whitespace is not digested", input: certPEM("cert-a") + "\n\n", want: wantCertA, ok: true},
		{name: "text ahead of the block is not digested", input: "Certificate:\n    Serial Number: 1\n" + certPEM("cert-a"), want: wantCertA, ok: true},
		{name: "chain", input: certPEM("leaf") + certPEM("intermediate")},
		{name: "certificate and key together", input: certPEM("cert-a") + "-----BEGIN PRIVATE KEY-----\na2V5\n-----END PRIVATE KEY-----\n"},
		{name: "private key alone", input: "-----BEGIN PRIVATE KEY-----\na2V5\n-----END PRIVATE KEY-----\n"},
		{name: "no pem block", input: "not a certificate"},
		{name: "empty", input: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := desiredCertFingerprint(tc.input)
			if ok != tc.ok {
				t.Fatalf("desiredCertFingerprint ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("desiredCertFingerprint = %q, want %q", got, tc.want)
			}
		})
	}
}

// TestNormalizeFingerprint locks the reported value to the form the local digest
// takes, and locks what is refused rather than compared.
func TestNormalizeFingerprint(t *testing.T) {
	t.Parallel()
	digest := strings.Repeat("AB", 32)

	tests := []struct {
		name     string
		reported string
		want     string
		ok       bool
	}{
		{name: "colon separated uppercase, as dokku reports", reported: strings.TrimSuffix(strings.Repeat("AB:", 32), ":"), want: digest, ok: true},
		{name: "lowercase", reported: strings.ToLower(digest), want: digest, ok: true},
		{name: "bare uppercase", reported: digest, want: digest, ok: true},
		{name: "surrounding whitespace", reported: "  " + digest + "\n", want: digest, ok: true},
		{name: "empty", reported: ""},
		{name: "sha1 length", reported: strings.Repeat("AB", 20)},
		{name: "not hex", reported: strings.Repeat("zz", 32)},
		{name: "labelled", reported: "sha256 Fingerprint=" + digest},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, ok := normalizeFingerprint(tc.reported)
			if ok != tc.ok {
				t.Fatalf("normalizeFingerprint ok = %v, want %v", ok, tc.ok)
			}
			if got != tc.want {
				t.Errorf("normalizeFingerprint = %q, want %q", got, tc.want)
			}
		})
	}
}

// assertNoCertsShow fails when the plan read the certificate back, which is the
// whole point of settling from the report.
func assertNoCertsShow(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if isCertsShow(call) {
			t.Errorf("plan read the certificate back rather than settling from its fingerprint: %q", call)
		}
	}
}

// assertCertsShowRan fails when a plan that should have fallen back to the exact
// comparison never made it, which would mean a verdict reached some other way.
func assertCertsShowRan(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if isCertsShow(call) {
			return
		}
	}
	t.Errorf("plan never fell back to reading the certificate back; calls were %v", calls)
}

// isCertsShow reports whether a recorded call read a certificate back. Both
// spellings have to be named: the app scope reads it back with certs:show and
// the global scope with global-cert:show, and neither string contains the other
// - `global-cert:show` is singular, so matching "certs:show" alone would pass
// every global test vacuously.
func isCertsShow(call string) bool {
	return strings.Contains(call, "certs:show") || strings.Contains(call, "global-cert:show")
}

// assertNoFingerprintReport fails when a plan read a fingerprint report it could
// not have used. A recipe pinning a chain is the case: a leaf digest cannot
// answer for the certificates behind it.
func assertNoFingerprintReport(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if strings.Contains(call, "--format json") {
			t.Errorf("plan read a fingerprint report it could not use: %q", call)
		}
	}
}

// assertNoLetsencryptProbe fails when a global plan asked whether letsencrypt
// manages the certificate. That plugin manages per-app certificates, so there is
// no global certificate for it to own.
func assertNoLetsencryptProbe(t *testing.T, calls []string) {
	t.Helper()
	for _, call := range calls {
		if strings.Contains(call, "letsencrypt:active") {
			t.Errorf("global plan probed letsencrypt: %q", call)
		}
	}
}
