package tasks

import (
	"strings"
	"testing"
)

// new_task_test.go covers the constructors #425 asks for. Building a task by
// struct literal is the documented way to drive the engine from Go, and it
// skips the `default:` tags the loader applies - so a literal with no State
// gets "" and falls into the `invalid state: ""` branch of DispatchPlan rather
// than behaving as present. NewTask is that allocation without needing YAML.

// TestNewTaskAppliesFieldDefaults is the foot-gun the issue names: the
// difference between a struct literal and a task the loader built.
func TestNewTaskAppliesFieldDefaults(t *testing.T) {
	t.Parallel()
	task, err := NewTask("dokku_app")
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	app, ok := task.(*AppTask)
	if !ok {
		t.Fatalf("NewTask returned %T, want *AppTask", task)
	}
	if app.State != StatePresent {
		t.Errorf("State = %q, want %q; the default: tag was not applied", app.State, StatePresent)
	}

	// The contrast, so the test says what it is protecting against.
	if (AppTask{}).State == StatePresent {
		t.Error("a bare struct literal already defaults its State; this test no longer means anything")
	}
}

// TestNewTaskRejectsAnUnknownType keeps the error a caller sees on a typo
// close to the loader's.
func TestNewTaskRejectsAnUnknownType(t *testing.T) {
	t.Parallel()
	if _, err := NewTask("dokku_not_a_task"); err == nil {
		t.Fatal("NewTask should reject an unregistered type key")
	} else if !strings.Contains(err.Error(), "unknown task type") {
		t.Errorf("error = %q, want it to name the unknown type", err)
	}
}

// TestNewTaskReturnsADistinctValue pins that the constructor allocates rather
// than handing back the registry prototype, which every later caller shares.
func TestNewTaskReturnsADistinctValue(t *testing.T) {
	t.Parallel()
	first, err := NewTask("dokku_app")
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	second, err := NewTask("dokku_app")
	if err != nil {
		t.Fatalf("NewTask: %v", err)
	}
	if first == second {
		t.Fatal("NewTask returned the same value twice; it must allocate")
	}
	first.(*AppTask).App = "api"
	if got := second.(*AppTask).App; got != "" {
		t.Errorf("second task saw the first's App = %q; the prototype is being shared", got)
	}
	if proto := RegisteredTasks["dokku_app"]; proto == first || proto == second {
		t.Error("NewTask handed back the registry prototype itself")
	}
}

// TestDecodeTaskAppliesDefaultsAndFields is the YAML-bearing form, exported so
// a caller holding a recipe fragment does not reimplement the loader's path.
func TestDecodeTaskAppliesDefaultsAndFields(t *testing.T) {
	t.Parallel()
	task, err := DecodeTask("dokku_app", []byte("app: api\n"))
	if err != nil {
		t.Fatalf("DecodeTask: %v", err)
	}
	app, ok := task.(*AppTask)
	if !ok {
		t.Fatalf("DecodeTask returned %T, want *AppTask", task)
	}
	if app.App != "api" {
		t.Errorf("App = %q, want %q", app.App, "api")
	}
	if app.State != StatePresent {
		t.Errorf("State = %q, want %q; the default: tag was not applied", app.State, StatePresent)
	}
}
