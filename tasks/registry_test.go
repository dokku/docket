package tasks

import (
	"sort"
	"testing"
)

// allRegisteredTasks returns a Lookup instance for every registered type, for
// the coverage tests that walk the whole registry.
func allRegisteredTasks() map[string]Task {
	out := make(map[string]Task, len(registeredTasks))
	for _, name := range TaskTypes() {
		task, _ := Lookup(name)
		out[name] = task
	}
	return out
}

// lookupTask is Lookup for a type key a test knows is registered.
func lookupTask(name string) Task {
	task, _ := Lookup(name)
	return task
}

// TestTaskTypesIsSortedAndComplete guards the helper the catalog, the --task
// validator and the shell completion all read from.
func TestTaskTypesIsSortedAndComplete(t *testing.T) {
	names := TaskTypes()
	if len(names) != len(registeredTasks) {
		t.Errorf("got %d names; registry has %d", len(names), len(registeredTasks))
	}
	if !sort.StringsAreSorted(names) {
		t.Errorf("names are not sorted: %v", names)
	}
	for _, name := range names {
		if _, ok := Lookup(name); !ok {
			t.Errorf("name %q is not in the registry", name)
		}
	}

	// The result is a copy, so a caller may sort or truncate it in place.
	if len(names) > 0 {
		names[0] = "mutated"
		if again := TaskTypes(); again[0] == "mutated" {
			t.Error("TaskTypes returned a slice aliasing shared state")
		}
	}
}

func TestLookupUnknownTypeReportsAbsent(t *testing.T) {
	task, ok := Lookup("dokku_does_not_exist")
	if ok {
		t.Error("Lookup reported an unregistered type as registered")
	}
	if task != nil {
		t.Errorf("Lookup returned %T for an unregistered type", task)
	}
}

func TestLookupReturnsTheRegisteredType(t *testing.T) {
	task, ok := Lookup("dokku_app")
	if !ok {
		t.Fatal("Lookup(dokku_app) reported it unregistered")
	}
	if _, ok := task.(*AppTask); !ok {
		t.Errorf("Lookup(dokku_app) returned %T, want *AppTask", task)
	}
}

// TestLookupDoesNotShareInstances is the property #565 exists for: the
// registry used to hand out one shared prototype per type, so a caller that
// set a field on it changed what every later lookup, validation and NewTask
// saw.
func TestLookupDoesNotShareInstances(t *testing.T) {
	first, _ := Lookup("dokku_app")
	second, _ := Lookup("dokku_app")
	if first == second {
		t.Fatal("Lookup returned the same instance twice; it must allocate")
	}

	first.(*AppTask).App = "api"
	if got := second.(*AppTask).App; got != "" {
		t.Errorf("an earlier Lookup instance saw App = %q", got)
	}
	if again, _ := Lookup("dokku_app"); again.(*AppTask).App != "" {
		t.Errorf("a later Lookup saw App = %q", again.(*AppTask).App)
	}
	task, err := NewTask("dokku_app")
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if got := task.(*AppTask).App; got != "" {
		t.Errorf("NewTask saw App = %q set on a Lookup instance", got)
	}
}

// TestNearestTaskKeyTieBreakIsStable pins the suggestion for a candidate one
// edit from two task types. The registry is a map, so walking it directly
// picked whichever tied key came up first; walking TaskTypes picks the
// alphabetically first every time.
func TestNearestTaskKeyTieBreakIsStable(t *testing.T) {
	for i := 0; i < 20; i++ {
		if got := nearestEnvelopeOrTaskKey("dokku_perts"); got != "dokku_certs" {
			t.Fatalf("nearestEnvelopeOrTaskKey(dokku_perts) = %q on call %d, want dokku_certs", got, i)
		}
	}
}
