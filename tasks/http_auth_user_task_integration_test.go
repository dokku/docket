package tasks

import (
	"reflect"
	"strings"
	"testing"
)

func TestIntegrationHttpAuthUser(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "http-auth")

	appName := "docket-test-http-auth-user"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	currentUsers := func(t *testing.T, label string) map[string]bool {
		t.Helper()
		got, err := getHttpAuthUsers(testCtx(), appName)
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
	if result := (HttpAuthTask{App: appName, Username: "admin", Password: "secret", State: StatePresent}).Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to enable http auth: %v", result.Error)
	}

	// add two users
	addTask := HttpAuthUserTask{
		App: appName,
		Users: []HttpAuthUser{
			{Username: "alice", Password: "alice-pass"},
			{Username: "bob", Password: "bob-pass"},
		},
		State: StatePresent,
	}
	result := addTask.Execute(testCtx())
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
	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent add")
	}

	// present for an existing user without update_password is in sync
	noop := HttpAuthUserTask{App: appName, Users: []HttpAuthUser{{Username: "alice", Password: "ignored"}}, State: StatePresent}
	result = noop.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed present no-op: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false for present existing user without update_password")
	}

	// update_password re-issues add-user for an existing user
	rotate := HttpAuthUserTask{App: appName, Users: []HttpAuthUser{{Username: "alice", Password: "new-pass"}}, UpdatePassword: boolPtr(true), State: StatePresent}
	result = rotate.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to rotate password: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true when update_password rotates an existing user")
	}

	// remove one user
	removeTask := HttpAuthUserTask{App: appName, Users: []HttpAuthUser{{Username: "bob"}}, State: StateAbsent}
	result = removeTask.Execute(testCtx())
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
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent remove")
	}

	// clearing with empty users removes every remaining user
	clearTask := HttpAuthUserTask{App: appName, State: StateAbsent}
	result = clearTask.Execute(testCtx())
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
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second clear: %v", result.Error)
	}
	if result.Changed {
		t.Error("expected Changed=false on idempotent clear")
	}
}

// TestIntegrationHttpAuthUserHashes exercises the credential form that survives
// a migration (#443): users expressed as htpasswd hashes, applied over stdin
// with http-auth:import-users. The hashes are read back from the live plugin
// rather than invented, so the in-sync assertions also pin that the round trip
// through import-users is byte-exact - a plugin that ever normalized entries on
// import would fail here rather than causing silent perpetual drift.
func TestIntegrationHttpAuthUserHashes(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "http-auth")

	appName := "docket-test-http-auth-hash"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	storedHashes := func(t *testing.T, label string) map[string]string {
		t.Helper()
		got, err := getHttpAuthUserHashes(testCtx(), appName)
		if err != nil {
			t.Fatalf("%s: getHttpAuthUserHashes failed: %v", label, err)
		}
		return got
	}
	usernames := func(t *testing.T, label string) []string {
		t.Helper()
		got, err := getHttpAuthUsers(testCtx(), appName)
		if err != nil {
			t.Fatalf("%s: getHttpAuthUsers failed: %v", label, err)
		}
		return sortedSetKeys(got)
	}

	// Seed two users the ordinary way, then read their stored hashes back. This
	// is exactly what `docket export` does.
	seed := HttpAuthUserTask{
		App: appName,
		Users: []HttpAuthUser{
			{Username: "alice", Password: "alice-pass"},
			{Username: "bob", Password: "bob-pass"},
		},
		State: StatePresent,
	}
	if result := seed.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to seed users: %v", result.Error)
	}
	seeded := storedHashes(t, "after seed")
	if len(seeded) != 2 || seeded["alice"] == "" || seeded["bob"] == "" {
		t.Fatalf("expected a stored hash for alice and bob, got %v", seeded)
	}

	// A task rebuilt from those hashes describes the server exactly, so it must
	// report no drift. The cleartext path can never reach this state.
	byHash := HttpAuthUserTask{
		App: appName,
		Users: []HttpAuthUser{
			{Username: "alice", Hash: seeded["alice"]},
			{Username: "bob", Hash: seeded["bob"]},
		},
		State: StatePresent,
	}
	if plan := byHash.Plan(testCtx()); !plan.InSync {
		t.Errorf("a task rebuilt from the stored hashes should be in sync, got status %v reason %q", plan.Status, plan.Reason)
	}

	// Importing a hash onto a fresh username reproduces the credential without
	// anyone knowing the password behind it.
	carolHash := seeded["alice"]
	addCarol := HttpAuthUserTask{
		App:   appName,
		Users: []HttpAuthUser{{Username: "carol", Hash: carolHash}},
		State: StatePresent,
	}
	result := addCarol.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to import carol: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true when importing a new user by hash")
	}
	if got := storedHashes(t, "after import")["carol"]; got != carolHash {
		t.Errorf("carol's stored hash = %q, want %q", got, carolHash)
	}
	// The hash rides on stdin, so it must not appear in any rendered command.
	for _, c := range result.Commands {
		if strings.Contains(c, carolHash) {
			t.Errorf("hash leaked into a rendered command: %q", c)
		}
	}

	// Re-importing the same hash is a no-op, even with update_password set:
	// the stored hash is readable, so the comparison wins over the flag.
	repeat := HttpAuthUserTask{
		App:            appName,
		Users:          []HttpAuthUser{{Username: "carol", Hash: carolHash}},
		UpdatePassword: boolPtr(true),
		State:          StatePresent,
	}
	if result := repeat.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed idempotent import: %v", result.Error)
	} else if result.Changed {
		t.Error("expected Changed=false when the stored hash already matches")
	}

	// A rotated hash converges without update_password, which the cleartext
	// path cannot do because a password is never readable.
	rotate := HttpAuthUserTask{
		App:   appName,
		Users: []HttpAuthUser{{Username: "carol", Hash: seeded["bob"]}},
		State: StatePresent,
	}
	if result := rotate.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed to rotate carol's hash: %v", result.Error)
	} else if !result.Changed {
		t.Error("expected Changed=true when the desired hash differs from the stored one")
	}
	if got := storedHashes(t, "after rotate")["carol"]; got != seeded["bob"] {
		t.Errorf("carol's stored hash = %q, want the rotated value", got)
	}

	// state: set with an all-hash list goes through import-users --replace, so
	// the app ends up with exactly the listed users.
	setTask := HttpAuthUserTask{
		App:   appName,
		Users: []HttpAuthUser{{Username: "alice", Hash: seeded["alice"]}},
		State: StateSet,
	}
	result = setTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to set users: %v", result.Error)
	}
	if !result.Changed {
		t.Error("expected Changed=true when the set drops users")
	}
	// commands/apply.go relies on the resulting state matching the desired one.
	if result.State != StateSet {
		t.Errorf("expected state 'set', got '%s'", result.State)
	}
	if got := usernames(t, "after set"); !reflect.DeepEqual(got, []string{"alice"}) {
		t.Errorf("users after set = %v, want [alice]", got)
	}
	if result := setTask.Execute(testCtx()); result.Error != nil {
		t.Fatalf("failed idempotent set: %v", result.Error)
	} else if result.Changed {
		t.Error("expected Changed=false on idempotent set")
	}

	// A set carrying a cleartext password cannot use --replace, since a
	// password has no htpasswd line to stream; it removes the strays and
	// upserts instead, and still lands the exact set.
	mixed := HttpAuthUserTask{
		App: appName,
		Users: []HttpAuthUser{
			{Username: "bob", Hash: seeded["bob"]},
			{Username: "dave", Password: "dave-pass"},
		},
		State: StateSet,
	}
	result = mixed.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed mixed set: %v", result.Error)
	}
	for _, c := range result.Commands {
		if strings.Contains(c, "--replace") {
			t.Errorf("a set containing a cleartext password must not use --replace, got %v", result.Commands)
		}
	}
	if got := usernames(t, "after mixed set"); !reflect.DeepEqual(got, []string{"bob", "dave"}) {
		t.Errorf("users after mixed set = %v, want [bob dave]", got)
	}
}
