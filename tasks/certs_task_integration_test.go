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

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	// initial state - no cert
	enabled, err := certsEnabled(CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected newly-created app to have no cert")
	}

	// add cert
	addTask := CertsTask{App: appName, Cert: certPath, Key: keyPath, State: StatePresent}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	enabled, err = certsEnabled(CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected cert to be enabled after add")
	}

	// add again - should be idempotent (does not update; matches ansible-dokku semantics)
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent add")
	}

	// remove cert
	removeTask := CertsTask{App: appName, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	enabled, err = certsEnabled(CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Errorf("expected cert to be disabled after remove")
	}

	// remove again - idempotent
	result = removeTask.Execute()
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

	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	enabled, err := certsEnabled(CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Fatalf("expected newly-created app to have no cert")
	}

	addTask := CertsTask{App: appName, CertContent: string(certPEM), KeyContent: string(keyPEM), State: StatePresent}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add cert inline: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first inline add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	enabled, err = certsEnabled(CertsTask{App: appName})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected cert to be enabled after inline add")
	}

	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second inline add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent inline add")
	}

	removeTask := CertsTask{App: appName, State: StateAbsent}
	result = removeTask.Execute()
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
		(CertsTask{Global: true, State: StateAbsent}).Execute()
	}
	cleanup()
	defer cleanup()

	addTask := CertsTask{Global: true, CertContent: string(certPEM), KeyContent: string(keyPEM), State: StatePresent}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add global cert inline: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first global inline add")
	}
	enabled, err := certsEnabled(CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected global cert to be enabled after inline add")
	}

	result = addTask.Execute()
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
		(CertsTask{Global: true, State: StateAbsent}).Execute()
	}
	cleanup()
	defer cleanup()

	// add global cert
	addTask := CertsTask{Global: true, Cert: certPath, Key: keyPath, State: StatePresent}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add global cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first global add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	enabled, err := certsEnabled(CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if !enabled {
		t.Errorf("expected global cert to be enabled after add")
	}

	// add again - idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second global add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent global add")
	}

	// remove global cert
	removeTask := CertsTask{Global: true, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove global cert: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first global remove")
	}
	enabled, err = certsEnabled(CertsTask{Global: true})
	if err != nil {
		t.Fatalf("certsEnabled failed: %v", err)
	}
	if enabled {
		t.Errorf("expected global cert to be disabled after remove")
	}

	// remove again - idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second global remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent global remove")
	}
}
