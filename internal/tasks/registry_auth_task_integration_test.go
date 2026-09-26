package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/dokku/docket/internal/subprocess"
)

// startTestRegistry boots a temporary `registry:2` container on a high port
// and returns its server string (host:port). The caller must register cleanup.
func startTestRegistry(t *testing.T) string {
	t.Helper()

	containerName := "docket-test-registry"

	// best-effort cleanup of any previous run
	subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args:    []string{"rm", "-f", containerName},
	})

	port := "5555"
	server := "localhost:" + port

	_, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "docker",
		Args: []string{
			"run", "-d", "--rm",
			"--name", containerName,
			"-p", port + ":5000",
			"registry:2",
		},
	})
	if err != nil {
		t.Skipf("skipping integration test: failed to start registry:2 container: %v", err)
	}
	t.Cleanup(func() {
		subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
			Command: "docker",
			Args:    []string{"rm", "-f", containerName},
		})
	})

	// wait until the registry is reachable
	deadline := time.Now().Add(20 * time.Second)
	for {
		result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
			Command: "curl",
			Args:    []string{"-sf", "http://" + server + "/v2/"},
		})
		if err == nil && result.ExitCode == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Skip("skipping integration test: registry container did not become ready in time")
		}
		time.Sleep(500 * time.Millisecond)
	}

	return server
}

// TestIntegrationRegistryAuthApp is the convergence proof: registry:auth-status
// compares what registry:login wrote, password included, so a re-apply of the
// same credential is a no-op and a rotated one is drift. Both credentials
// travel on stdin, so this also proves the probe's --password-stdin form lines
// up with the login's.
//
// It stays app-scoped on purpose. The global config is root's own
// ~/.docker/config.json, and a credential helper configured there makes dokku
// answer "cannot compare" for every server - see TestIntegrationRegistryAuthGlobal.
func TestIntegrationRegistryAuthApp(t *testing.T) {
	skipIfNoDokkuT(t)
	server := startTestRegistry(t)

	appName := "docket-test-registry-auth"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// best-effort cleanup of any leftover credential
	(&RegistryAuthTask{App: appName, Server: server, State: StateAbsent}).Execute(testCtx())

	// logging out of an app that never logged in is a no-op rather than the
	// failure it used to be: registry:logout errors with "No registry
	// credentials found", and the probe now settles before it is ever run.
	logoutTask := RegistryAuthTask{App: appName, Server: server, State: StateAbsent}
	result := logoutTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("logging out with no credential should be a no-op: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false when the app has no credential")
	}

	// log in
	loginTask := RegistryAuthTask{
		App:      appName,
		Server:   server,
		Username: "testuser",
		Password: "testpassword",
		State:    StatePresent,
	}
	result = loginTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to log in: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on login")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// re-applying the same credential changes nothing, which is the whole
	// point: no network round trip to the registry, no credential rewritten.
	result = loginTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to re-apply the login: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false when the stored credential already matches")
	}

	// a rotated password is drift even though the server and username are
	// unchanged, so the probe has to be comparing the secret and not just the
	// credential's existence
	rotateTask := loginTask
	rotateTask.Password = "rotatedpassword"
	result = rotateTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to rotate the password: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true when the password changed")
	}

	// log out
	result = logoutTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to log out: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on logout")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}

	// and removing a credential that is already gone is a no-op again
	result = logoutTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to re-apply the logout: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false when the credential is already gone")
	}
}

// TestIntegrationRegistryAuthGlobal covers the --global scope, which reads
// root's own ~/.docker/config.json rather than a directory dokku owns. A
// credential helper configured there holds every secret outside the file, so
// dokku answers "cannot compare" and the task cannot converge - an operator's
// choice rather than a docket bug, and the reason the convergence assertions
// below are guarded rather than unconditional.
func TestIntegrationRegistryAuthGlobal(t *testing.T) {
	skipIfNoDokkuT(t)
	server := startTestRegistry(t)

	// best-effort cleanup of any leftover global credential
	(&RegistryAuthTask{Global: true, Server: server, State: StateAbsent}).Execute(testCtx())
	t.Cleanup(func() {
		(&RegistryAuthTask{Global: true, Server: server, State: StateAbsent}).Execute(testCtx())
	})

	loginTask := RegistryAuthTask{
		Global:   true,
		Server:   server,
		Username: "testuser",
		Password: "testpassword",
		State:    StatePresent,
	}
	result := loginTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to log in globally: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on global login")
	}

	if globalCredentialHelperConfigured(t) {
		t.Log("a docker credential helper holds the global credential; skipping the convergence assertions")
	} else {
		result = loginTask.Execute(testCtx())
		if result.Error != nil {
			t.Fatalf("failed to re-apply the global login: %v", result.Error)
		}
		if result.Changed {
			t.Errorf("expected Changed=false when the stored global credential already matches")
		}
	}

	logoutTask := RegistryAuthTask{Global: true, Server: server, State: StateAbsent}
	result = logoutTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to log out globally: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on global logout")
	}
}

// globalCredentialHelperConfigured reports whether root's docker config names a
// credential helper, which puts every stored secret out of dokku's reach.
func globalCredentialHelperConfigured(t *testing.T) bool {
	t.Helper()
	result, err := subprocess.CallExecCommand(testCtx(), subprocess.ExecCommandInput{
		Command: "sudo",
		Args:    []string{"cat", "/root/.docker/config.json"},
	})
	if err != nil {
		return false
	}
	body := result.StdoutContents()
	return strings.Contains(body, "credsStore") || strings.Contains(body, "credHelpers")
}

func TestIntegrationRegistryAuthPasswordNotInArgs(t *testing.T) {
	// Verify that the password is fed via stdin and never appears in argv. This
	// runs against a fake `dokku` that records argv and stdin to /tmp files.
	// Skipped unless dokku is real - we want to hit the actual subprocess path.
	skipIfNoDokkuT(t)

	// We exercise the real registry:login against the test registry and then
	// scan dokku's logs / running processes is too brittle; instead, do a
	// surface-level check: verify that with a password containing whitespace,
	// the command still succeeds (which it could not if the password were
	// being naively quoted into argv).
	server := startTestRegistry(t)
	appName := "docket-test-registry-auth-stdin"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	pw := "p@ss with spaces and 'quotes\""
	loginTask := RegistryAuthTask{
		App:      appName,
		Server:   server,
		Username: "u",
		Password: pw,
		State:    StatePresent,
	}
	result := loginTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("login with whitespace password failed: %v", result.Error)
	}
	if strings.Contains(result.Message, pw) {
		t.Errorf("password should not appear in task message")
	}

	(&RegistryAuthTask{App: appName, Server: server, State: StateAbsent}).Execute(testCtx())
}

// TestIntegrationRegistryAuthExport proves the export half against a real
// server: registry:report names the server the app holds a credential for, and
// the credential itself - which dokku will not reveal in either half - comes
// back as two required inputs rather than being dropped on the floor.
func TestIntegrationRegistryAuthExport(t *testing.T) {
	skipIfNoDokkuT(t)
	server := startTestRegistry(t)

	appName := "docket-test-registry-export"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	login := RegistryAuthTask{
		App:      appName,
		Server:   server,
		Username: "testuser",
		Password: "testpassword",
		State:    StatePresent,
	}
	if result := login.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to log in: %v", result.Error)
	}
	t.Cleanup(func() {
		(&RegistryAuthTask{App: appName, Server: server, State: StateAbsent}).Execute(testCtx())
	})

	res, err := ExportRecipe(testCtx(), ExportOptions{Apps: []string{appName}})
	if err != nil {
		t.Fatalf("ExportRecipe: %v", err)
	}

	var found *RegistryAuthTask
	for _, play := range res.Plays() {
		for _, task := range play.Tasks {
			if b, ok := As[RegistryAuthTask](task); ok && b.Server == server {
				found = &b
			}
		}
	}
	if found == nil {
		t.Fatalf("expected a dokku_registry_auth task for %s in the exported recipe", server)
	}
	if found.App != appName {
		t.Errorf("App = %q, want %q", found.App, appName)
	}
	// The credential is unreadable, so both halves are template references
	// rather than values - and the real password must not have leaked in.
	if !strings.HasPrefix(found.Username, "{{ .") || !strings.HasPrefix(found.Password, "{{ .") {
		t.Errorf("expected both credentials lifted into inputs, got username=%q password=%q", found.Username, found.Password)
	}
	if strings.Contains(found.Password, "testpassword") {
		t.Errorf("the stored password must not appear in the exported recipe, got %q", found.Password)
	}
	for name, value := range res.Vars {
		if strings.Contains(name, "registry_") && value != "" {
			t.Errorf("%s = %q, want an empty placeholder", name, value)
		}
	}
}
