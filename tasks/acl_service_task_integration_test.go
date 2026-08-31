package tasks

import (
	"reflect"
	"sort"
	"testing"
)

func TestIntegrationAclService(t *testing.T) {
	skipIfNoDokkuT(t)
	skipIfPluginMissingT(t, "acl")
	skipIfPluginMissingT(t, "redis")

	serviceType := "redis"
	serviceName := "docket-test-acl-service"

	// ensure clean state and create the redis service
	destroyService(testCtx(), serviceType, serviceName)
	if r := (ServiceCreateTask{Service: serviceType, Name: serviceName, State: StatePresent}).Execute(testCtx()); r.Error != nil {
		t.Fatalf("failed to create redis service: %v", r.Error)
	}
	t.Cleanup(func() {
		destroyService(testCtx(), serviceType, serviceName)
	})

	assertACL := func(t *testing.T, label string, want []string) {
		t.Helper()
		got, err := getAclServiceUsers(testCtx(), serviceType, serviceName)
		if err != nil {
			t.Fatalf("%s: getAclServiceUsers failed: %v", label, err)
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
	addTask := AclServiceTask{Service: serviceName, Type: serviceType, Users: []string{"alice", "bob"}, State: StatePresent}
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
	removeTask := AclServiceTask{Service: serviceName, Type: serviceType, Users: []string{"bob"}, State: StateAbsent}
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
	if err := (AclServiceTask{Service: serviceName, Type: serviceType, Users: []string{"bob", "carol"}, State: StatePresent}).Execute(testCtx()).Error; err != nil {
		t.Fatalf("failed to re-add users: %v", err)
	}
	assertACL(t, "after re-add", []string{"alice", "bob", "carol"})

	clearTask := AclServiceTask{Service: serviceName, Type: serviceType, State: StateAbsent}
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
