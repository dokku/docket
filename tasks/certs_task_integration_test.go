package tasks

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// generateSelfSignedCert writes a self-signed cert+key pair into a fresh
// directory under /tmp and returns their paths. The directory and files are
// world-readable and the directory is registered for cleanup so the dokku
// user (which various dokku plugins shell out to) can read the files even
// when the test process runs as root.
func generateSelfSignedCert(t *testing.T, commonName string) (certPath, keyPath string) {
	t.Helper()

	dir, err := os.MkdirTemp("/tmp", "docket-certs-")
	if err != nil {
		t.Fatalf("failed to create cert dir: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	if err := os.Chmod(dir, 0o755); err != nil {
		t.Fatalf("failed to chmod cert dir: %v", err)
	}

	priv, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("failed to generate key: %v", err)
	}

	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		t.Fatalf("failed to generate serial: %v", err)
	}

	template := x509.Certificate{
		SerialNumber: serial,
		Subject:      pkix.Name{CommonName: commonName},
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(365 * 24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{commonName},
	}

	derBytes, err := x509.CreateCertificate(rand.Reader, &template, &template, &priv.PublicKey, priv)
	if err != nil {
		t.Fatalf("failed to create certificate: %v", err)
	}

	certPath = filepath.Join(dir, "test.crt")
	if err := os.WriteFile(certPath, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: derBytes}), 0o644); err != nil {
		t.Fatalf("failed to write cert file: %v", err)
	}

	keyBytes, err := x509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		t.Fatalf("failed to marshal key: %v", err)
	}
	keyPath = filepath.Join(dir, "test.key")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyBytes}), 0o644); err != nil {
		t.Fatalf("failed to write key file: %v", err)
	}

	return certPath, keyPath
}

func TestIntegrationCertsApp(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-certs"
	certPath, keyPath := generateSelfSignedCert(t, appName+".example.com")

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// initial state - no cert
	enabled, err := certsEnabled(testCtx(), CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected newly-created app to have no cert")
	}

	// add cert
	addTask := CertsTask{App: appName, Cert: certPath, Key: keyPath, State: StatePresent}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	enabled, err = certsEnabled(testCtx(), CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected cert to be enabled after add")
	}

	// add again with the same material - the probe compares the installed
	// certificate and finds nothing to do
	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent add")
	}

	// remove cert
	removeTask := CertsTask{App: appName, State: StateAbsent}
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	enabled, err = certsEnabled(testCtx(), CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Errorf("expected cert to be disabled after remove")
	}

	// remove again - idempotent
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent remove")
	}
}

func TestIntegrationCertsAppInline(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-certs-inline"
	certPath, keyPath := generateSelfSignedCert(t, appName+".example.com")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	enabled, err := certsEnabled(testCtx(), CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected newly-created app to have no cert")
	}

	addTask := CertsTask{App: appName, CertContent: string(certPEM), KeyContent: string(keyPEM), State: StatePresent}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add cert inline: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first inline add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	enabled, err = certsEnabled(testCtx(), CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected cert to be enabled after inline add")
	}

	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second inline add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent inline add")
	}

	removeTask := CertsTask{App: appName, State: StateAbsent}
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove inline cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
}

func TestIntegrationCertsGlobalInline(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "global-cert")

	certPath, keyPath := generateSelfSignedCert(t, "global-inline.example.com")
	certPEM, err := os.ReadFile(certPath)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	keyPEM, err := os.ReadFile(keyPath)
	if err != nil {
		t.Fatalf("read key: %v", err)
	}

	cleanup := func() {
		(CertsTask{Global: true, State: StateAbsent}).Execute(testCtx())
	}
	cleanup()
	defer cleanup()

	addTask := CertsTask{Global: true, CertContent: string(certPEM), KeyContent: string(keyPEM), State: StatePresent}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add global cert inline: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first global inline add")
	}
	enabled, err := certsEnabled(testCtx(), CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected global cert to be enabled after inline add")
	}

	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second global inline add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent global inline add")
	}
}

func TestIntegrationCertsGlobal(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "global-cert")

	certPath, keyPath := generateSelfSignedCert(t, "global.example.com")

	// best-effort cleanup before and after
	cleanup := func() {
		(CertsTask{Global: true, State: StateAbsent}).Execute(testCtx())
	}
	cleanup()
	defer cleanup()

	// add global cert
	addTask := CertsTask{Global: true, Cert: certPath, Key: keyPath, State: StatePresent}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add global cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first global add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	enabled, err := certsEnabled(testCtx(), CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected global cert to be enabled after add")
	}

	// add again - idempotent
	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second global add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent global add")
	}

	// remove global cert
	removeTask := CertsTask{Global: true, State: StateAbsent}
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove global cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first global remove")
	}
	enabled, err = certsEnabled(testCtx(), CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Errorf("expected global cert to be disabled after remove")
	}

	// remove again - idempotent
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second global remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent global remove")
	}
}

// TestIntegrationCertsAppRotation covers the case the probe was added for: an
// app already holding a certificate, and a recipe pinning a different one. The
// path form doubles as coverage of the local-file read, since a local run
// resolves `cert:` on this machine.
func TestIntegrationCertsAppRotation(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-certs-rotate"
	oldCert, oldKey := generateSelfSignedCert(t, appName+".example.com")
	newCert, newKey := generateSelfSignedCert(t, appName+".example.net")

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	install := CertsTask{App: appName, Cert: oldCert, Key: oldKey, State: StatePresent}
	if result := install.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to add cert: %v", result.Error)
	}

	rotate := CertsTask{App: appName, Cert: newCert, Key: newKey, State: StatePresent}
	plan := rotate.Plan(testCtx())
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}

	result := rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to rotate cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when the pinned certificate differs")
	}

	installed, err := certsShow(testCtx(), CertsTask{App: appName}, "crt")
	if err != nil {
		t.Fatalf("certsShow failed: %v", err)
	}
	wantPEM, err := os.ReadFile(newCert)
	if err != nil {
		t.Fatalf("read cert: %v", err)
	}
	if !samePEM(installed, string(wantPEM)) {
		t.Errorf("installed certificate is not the one the recipe pinned")
	}

	// The rotation settles: a second run of the same recipe is a no-op.
	result = rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second rotate: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false once the pinned certificate is installed")
	}
}

// TestIntegrationCertsAppInlineRotation is the same rotation through inline PEM,
// the form that works over any transport.
func TestIntegrationCertsAppInlineRotation(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-certs-inline-rotate"
	oldCert, oldKey := generateSelfSignedCert(t, appName+".example.com")
	newCert, newKey := generateSelfSignedCert(t, appName+".example.net")

	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	install := CertsTask{App: appName, CertContent: readFileT(t, oldCert), KeyContent: readFileT(t, oldKey), State: StatePresent}
	if result := install.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to add cert inline: %v", result.Error)
	}

	rotate := CertsTask{App: appName, CertContent: readFileT(t, newCert), KeyContent: readFileT(t, newKey), State: StatePresent}
	plan := rotate.Plan(testCtx())
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}

	result := rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to rotate cert inline: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when the pinned certificate differs")
	}

	result = rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second inline rotate: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false once the pinned certificate is installed")
	}
}

// TestIntegrationCertsGlobalRotation covers the same rotation for the global
// scope, which reads back through global-cert:show.
func TestIntegrationCertsGlobalRotation(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "global-cert")

	oldCert, oldKey := generateSelfSignedCert(t, "global-rotate.example.com")
	newCert, newKey := generateSelfSignedCert(t, "global-rotate.example.net")

	cleanup := func() {
		(CertsTask{Global: true, State: StateAbsent}).Execute(testCtx())
	}
	cleanup()
	defer cleanup()

	install := CertsTask{Global: true, Cert: oldCert, Key: oldKey, State: StatePresent}
	if result := install.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to add global cert: %v", result.Error)
	}

	rotate := CertsTask{Global: true, Cert: newCert, Key: newKey, State: StatePresent}
	plan := rotate.Plan(testCtx())
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {InSync:%v Status:%q}, want drift with %q", plan.InSync, plan.Status, PlanStatusModify)
	}

	result := rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to rotate global cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when the pinned global certificate differs")
	}

	result = rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second global rotate: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false once the pinned global certificate is installed")
	}
}

// readFileT reads a generated fixture, failing the test rather than returning an
// error the caller has to thread through a struct literal.
func readFileT(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}
