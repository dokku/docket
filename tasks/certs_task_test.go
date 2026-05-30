package tasks

import (
	"archive/tar"
	"bytes"
	"io"
	"strings"
	"testing"
)

func TestCertsTaskInvalidState(t *testing.T) {
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", Key: "/tmp/key", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestCertsTaskMissingApp(t *testing.T) {
	task := CertsTask{Cert: "/tmp/cert", Key: "/tmp/key", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskGlobalWithApp(t *testing.T) {
	task := CertsTask{App: "test-app", Global: true, Cert: "/tmp/cert", Key: "/tmp/key", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskPresentMissingCert(t *testing.T) {
	task := CertsTask{App: "test-app", Key: "/tmp/key", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without cert should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' (or 'cert_content') and 'key' (or 'key_content') are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskPresentMissingKey(t *testing.T) {
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without key should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' (or 'cert_content') and 'key' (or 'key_content') are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskInlineMissingKeyContent(t *testing.T) {
	task := CertsTask{App: "test-app", CertContent: "cert-pem", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with cert_content but no key should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' (or 'cert_content') and 'key' (or 'key_content') are required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskInlineMixedSources(t *testing.T) {
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", KeyContent: "key-pem", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with cert + key_content should return a validation error")
	}
	if !strings.Contains(result.Error.Error(), "cannot be mixed") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestCertsTaskInlineBothCertForms(t *testing.T) {
	task := CertsTask{App: "test-app", Cert: "/tmp/cert", CertContent: "cert-pem", Key: "/tmp/key", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with both cert and cert_content should return a validation error")
	}
	if !strings.Contains(result.Error.Error(), "'cert' and 'cert_content' are mutually exclusive") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuildCertTarball(t *testing.T) {
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
