package tasks

import (
	"testing"
)

func TestIntegrationHttpAuthUser(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "http-auth")

	appName := "docket-test-http-auth-user"
	destroyApp(appName)
	createApp(appName)
	defer destroyApp(appName)

	currentUsers := func(t *testing.T, label string) map[string]bool {
		t.Helper()
		got, err := getHttpAuthUsers(appName)
		if err != nil {
			t.Fatalf("%s: getHttpAuthUsers failed: %v", label, err)
		}
		return got
	}
	assertHas := func(t *testing.T, label, username string, want bool) {
		t.Helper()
		if got := currentUsers(t, label)[username]; got != want {
			t.Errorf("%s: user %q present=%v, want %v", label, username, got, want)
		}
	}

	// enable http auth so the app has an initialized auth config
	if result := (HttpAuthTask{App: appName, Username: "admin", Password: "secret", State: StatePresent}).Execute(); result.Error != nil {
		t.Fatalf("failed to enable http auth: %v", result.Error)
	}

	// add two users
	addTask := HttpAuthUserTask{
		App: appName,
		Users: []HttpAuthUser{
			{Username: "alice", Password: "alice-pass"},
			{Username: "bob", Password: "bob-pass"},
		},
	}
	result := addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to add users: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	assertHas(t, "after add", "alice", true)
	assertHas(t, "after add", "bob", true)

	// adding the same users again is idempotent
	result = addTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent add")
	}

	// present for an existing user without update_password is in sync
	noop := HttpAuthUserTask{App: appName, Users: []HttpAuthUser{{Username: "alice", Password: "ignored"}}}
	result = noop.Execute()
	if result.Error != nil {
		t.Fatalf("failed present no-op: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false for present existing user without update_password")
	}

	// update_password re-issues add-user for an existing user
	rotate := HttpAuthUserTask{App: appName, Users: []HttpAuthUser{{Username: "alice", Password: "new-pass"}}, UpdatePassword: true}
	result = rotate.Execute()
	if result.Error != nil {
		t.Fatalf("failed to rotate password: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true when update_password rotates an existing user")
	}

	// remove one user
	removeTask := HttpAuthUserTask{App: appName, Users: []HttpAuthUser{{Username: "bob"}}, State: StateAbsent}
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to remove user: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	assertHas(t, "after remove bob", "bob", false)
	assertHas(t, "after remove bob", "alice", true)

	// removing the same user again is idempotent
	result = removeTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent remove")
	}

	// clearing with empty users removes every remaining user
	clearTask := HttpAuthUserTask{App: appName, State: StateAbsent}
	result = clearTask.Execute()
	if result.Error != nil {
		t.Fatalf("failed to clear users: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true on first clear")
	}
	if remaining := currentUsers(t, "after clear"); len(remaining) != 0 {
		t.Errorf("expected no users after clear, got %v", remaining)
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
