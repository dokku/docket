package tasks

import (
	"testing"
)

func TestIntegrationHttpAuthAllowedIp(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "http-auth")

	appName := "docket-test-http-auth-allowed-ip"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	currentIps := func(t *testing.T, label string) map[string]bool {
		t.Helper()
		got, err := getHttpAuthAllowedIps(appName)
		if err != nil {
			t.Fatalf("%s: getHttpAuthAllowedIps failed: %v", label, err)
		}
		return got
	}
	assertHas := func(t *testing.T, label, ip string, want bool) {
		t.Helper()
		if got := currentIps(t, label)[ip]; got != want {
			t.Errorf("%s: allowed ip %q present=%v, want %v", label, ip, got, want)
		}
	}

	// initialize auth so the app has a valid htpasswd/nginx config; add-allowed-ip
	// flips enabled=true but does not create an htpasswd file on its own
	if result := (HttpAuthTask{App: appName, Username: "admin", Password: "secret", State: StatePresent}).Execute(); result.Error != nil {
		t.Fatalf("failed to enable http auth: %v", result.Error)
	}

	firstIp := "192.0.2.1"
	secondIp := "198.51.100.2"

	// add two allowed ips
	addTask := HttpAuthAllowedIpTask{
		App:        appName,
		AllowedIps: []string{firstIp, secondIp},
		State:      StatePresent,
	}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add allowed ips: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	assertHas(t, "after add", firstIp, true)
	assertHas(t, "after add", secondIp, true)

	// adding the same allowed ips again is idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent add")
	}

	// remove one allowed ip
	removeTask := HttpAuthAllowedIpTask{App: appName, AllowedIps: []string{secondIp}, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove allowed ip: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	assertHas(t, "after remove second", secondIp, false)
	assertHas(t, "after remove second", firstIp, true)

	// removing the same allowed ip again is idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent remove")
	}

	// clearing with empty allowed_ips removes every remaining allowed ip
	clearTask := HttpAuthAllowedIpTask{App: appName, State: StateAbsent}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear allowed ips: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first clear")
	}
	if remaining := currentIps(t, "after clear"); len(remaining) != 0 {
		t.Errorf("expected no allowed ips after clear, got %v", remaining)
	}

	// clearing again is idempotent
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second clear: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent clear")
	}
}
