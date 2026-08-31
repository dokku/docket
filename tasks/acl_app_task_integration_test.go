package tasks

import (
	"reflect"
	"sort"
	"testing"
)

func TestIntegrationAclApp(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "acl")

	appName := "docket-test-acl-app"
	destroyApp(testCtx(), appName)
	createApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	assertACL := func(t *testing.T, label string, want []string) {
		t.Helper()
		got, err := getAclAppUsers(testCtx(), appName)
		if err != nil {
			t.Fatalf("%s: getAclAppUsers failed: %v", label, err)
		}
		var gotSlice []string
		for u := range got {
			gotSlice = append(gotSlice, u)
		}
		sort.Strings(gotSlice)
		sort.Strings(want)
		if want == nil {
			want = []string{}
		}
		if gotSlice == nil {
			gotSlice = []string{}
		}
		if !reflect.DeepEqual(gotSlice, want) {
			t.Errorf("%s: ACL = %v, want %v", label, gotSlice, want)
		}
	}

	// initial state - empty ACL
	assertACL(t, "initial", nil)

	// add two users
	addTask := AclAppTask{App: appName, Users: []string{"alice", "bob"}, State: StatePresent}
	result := addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to add users: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first add")
	}
	if result.State != StatePresent {
		t.Errorf("expected state 'present', got '%s'", result.State)
	}
	assertACL(t, "after add", []string{"alice", "bob"})

	// add same users again - idempotent
	result = addTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second add: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent add")
	}
	assertACL(t, "after idempotent add", []string{"alice", "bob"})

	// remove one user
	removeTask := AclAppTask{App: appName, Users: []string{"bob"}, State: StateAbsent}
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to remove user: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first remove")
	}
	if result.State != StateAbsent {
		t.Errorf("expected state 'absent', got '%s'", result.State)
	}
	assertACL(t, "after remove bob", []string{"alice"})

	// remove same user again - idempotent
	result = removeTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second remove: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent remove")
	}
	assertACL(t, "after idempotent remove", []string{"alice"})

	// re-add bob and carol, then clear with empty users
	if err := (AclAppTask{App: appName, Users: []string{"bob", "carol"}, State: StatePresent}).Execute(testCtx()).Error; err != nil {
		t.Fatalf("failed to re-add users: %v", err)
	}
	assertACL(t, "after re-add", []string{"alice", "bob", "carol"})

	clearTask := AclAppTask{App: appName, State: StateAbsent}
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed to clear ACL: %v", result.Error)
	}
	if !result.Changed {
		t.Errorf("expected Changed=true on first clear")
	}
	assertACL(t, "after clear", nil)

	// clear again - idempotent
	result = clearTask.Execute(testCtx())
	if result.Error != nil {
		t.Fatalf("failed second clear: %v", result.Error)
	}
	if result.Changed {
		t.Errorf("expected Changed=false on idempotent clear")
	}
	assertACL(t, "after idempotent clear", nil)
}
