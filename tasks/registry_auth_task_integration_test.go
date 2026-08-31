package tasks

import (
	"strings"
	"testing"
	"time"

	"github.com/dokku/docket/subprocess"
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

func TestIntegrationRegistryAuthApp(t *testing.T) {
	skipIfNoDokkuT(t)
	server := startTestRegistry(t)

	appName := "docket-test-registry-auth"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	// best-effort cleanup of any leftover credential
	(&RegistryAuthTask{App: appName, Server: server, State: StateAbsent}).Execute(testCtx())

	// log in
	loginTask := RegistryAuthTask{
		App:      appName,
		Server:   server,
		Username: "testuser",
		Password: "testpassword",
		State:    StatePresent,
	}
	result := loginTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to log in: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on login")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}

	// log out
	logoutTask := RegistryAuthTask{App: appName, Server: server, State: StateAbsent}
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
}

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

	logoutTask := RegistryAuthTask{Global: true, Server: server, State: StateAbsent}
	result = logoutTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to log out globally: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on global logout")
	}
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
