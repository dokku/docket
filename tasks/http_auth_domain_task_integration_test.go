package tasks

import (
	"testing"
)

func TestIntegrationHttpAuthDomain(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "http-auth")

	appName := "docket-test-http-auth-domain"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	currentDomains := func(t *testing.T, label string) map[string]bool {
		t.Helper()
		got, err := getHttpAuthDomains(appName)
		if err != nil {
			t.Fatalf("%s: getHttpAuthDomains failed: %v", label, err)
		}
		return got
	}
	assertHas := func(t *testing.T, label, domain string, want bool) {
		t.Helper()
		if got := currentDomains(t, label)[domain]; got != want {
			t.Errorf("%s: auth domain %q present=%v, want %v", label, domain, got, want)
		}
	}

	firstDomain := "auth-first.example.com"
	secondDomain := "auth-second.example.com"

	// initialize auth so the app has a valid htpasswd/nginx config; add-domain
	// flips enabled=true but does not create an htpasswd file on its own
	if result := (HttpAuthTask{App: appName, Username: "admin", Password: "secret", State: StatePresent}).Execute(); result.Error != nil {
		t.Fatalf("failed to enable http auth: %v", result.Error)
	}

	// http-auth:add-domain / set-domains only accept domains already attached to
	// the app as nginx vhosts, so attach them first
	if result := (DomainsTask{App: appName, Domains: []string{firstDomain, secondDomain}, State: StatePresent}).Execute(); result.Error != nil {
		t.Fatalf("failed to attach app domains: %v", result.Error)
	}

	// add two auth domains
	addTask := HttpAuthDomainTask{
		App:     appName,
		Domains: []string{firstDomain, secondDomain},
		State:   StatePresent,
	}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add auth domains: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	assertHas(t, "after add", firstDomain, true)
	assertHas(t, "after add", secondDomain, true)

	// adding the same auth domains again is idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent add")
	}

	// remove one auth domain
	removeTask := HttpAuthDomainTask{App: appName, Domains: []string{secondDomain}, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove auth domain: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	assertHas(t, "after remove second", secondDomain, false)
	assertHas(t, "after remove second", firstDomain, true)

	// removing the same auth domain again is idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent remove")
	}

	// set replaces the whole list; from {first} to {first, second} adds second
	setTask := HttpAuthDomainTask{App: appName, Domains: []string{firstDomain, secondDomain}, State: StateSet}
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to set auth domains: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first set")
	}
	if result.State != StateSet {
		t.Errorf("expected state 'set', got '%s'", result.State)
	}
	assertHas(t, "after set", firstDomain, true)
	assertHas(t, "after set", secondDomain, true)

	// setting the same list again is idempotent
	result = setTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second set: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent set")
	}

	// clear removes every auth domain
	clearTask := HttpAuthDomainTask{App: appName, State: StateClear}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear auth domains: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first clear")
	}
	if result.State != StateClear {
		t.Errorf("expected state 'clear', got '%s'", result.State)
	}
	if remaining := currentDomains(t, "after clear"); len(remaining) != 0 {
		t.Errorf("expected no auth domains after clear, got %v", remaining)
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
