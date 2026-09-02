package tasks

import (
	"archive/tar"
	"bytes"
	"context"
	"encoding/pem"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
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

// certsAppFixture answers the three reads the app-scope present branch makes:
// a certificate is installed, letsencrypt does not manage it, and certs:show
// hands back installed.
func certsAppFixture(app, installed string) map[string]string {
	return map[string]string{
		"--quiet certs:report " + app + " --ssl-enabled": "true",
		"--quiet letsencrypt:active " + app:              "false",
		"--quiet certs:show " + app + " crt":             installed,
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

// TestCertsTaskPlanNormalizesPEM locks the comparison to the decoded blocks.
// certs:show is a plain cat of server.crt and StdoutContents trims it, so an
// inline cert_content that ends in a newline - which every PEM file does - would
// otherwise plan as drift forever against the certificate it just installed.
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

// TestCertsTaskPlanGlobalComparesGlobalCertShow locks the global scope to
// global-cert:show, the twin of certs:show the export path already uses, and to
// leaving the letsencrypt probe alone - that plugin manages per-app
// certificates, so there is no global certificate for it to own.
func TestCertsTaskPlanGlobalComparesGlobalCertShow(t *testing.T) {
	t.Parallel()
	var calls []string
	ctx := subprocess.ContextWithRunner(testCtx(), recordingDokku(map[string]string{
		"--quiet global-cert:report --global --global-cert-enabled": "true",
		"--quiet global-cert:show crt":                              certPEM("global-old"),
	}, &calls))

	plan := CertsTask{
		Global:      true,
		CertContent: certPEM("global-renewed"),
		KeyContent:  "-----BEGIN PRIVATE KEY-----\nglobal-key\n-----END PRIVATE KEY-----\n",
		State:       StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}
	if !reflect.DeepEqual(plan.Mutations, []string{"replace certificate for (global)"}) {
		t.Errorf("Mutations = %v, want [replace certificate for (global)]", plan.Mutations)
	}
	if len(plan.Commands) != 1 || !strings.HasSuffix(plan.Commands[0], "global-cert:set") {
		t.Errorf("Commands = %v, want one command ending in global-cert:set", plan.Commands)
	}
	for _, call := range calls {
		if strings.Contains(call, "letsencrypt:active") {
			t.Errorf("global plan probed letsencrypt: %q", call)
		}
	}
	assertNoCertMaterial(t, plan)
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
		if strings.Contains(call, "certs:show") {
			t.Errorf("plan read the certificate back with nothing to compare it to: %q", call)
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
		if strings.Contains(call, "certs:show") {
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
// certs:report failure.
func TestCertsTaskPlanShowFailureIsProbeError(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), func(_ context.Context, in subprocess.ExecCommandInput) (subprocess.ExecCommandResponse, error) {
		joined := strings.Join(in.Args, " ")
		if strings.Contains(joined, "certs:show") {
			return subprocess.ExecCommandResponse{ExitCode: 1}, errors.New("test-app doesn't have an SSL endpoint defined")
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
