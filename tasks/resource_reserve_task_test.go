package tasks

import (
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestResourceReserveTaskInvalidState(t *testing.T) {
	task := ResourceReserveTask{
		App:       "test-app",
		Resources: map[string]string{"cpu": "100"},
		State:     "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestResourceReserveTaskEmptyResources(t *testing.T) {
	task := ResourceReserveTask{App: "test-app", Resources: map[string]string{}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty resources and state=present should return an error")
	}
}

func TestResourceReserveTaskNilResources(t *testing.T) {
	task := ResourceReserveTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with nil resources and state=present should return an error")
	}
}

func TestGetTasksResourceReserveTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set resource reservations
      dokku_resource_reserve:
        app: test-app
        process_type: web
        resources:
          cpu: "100"
          memory: "256"
        clear_before: true
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set resource reservations")
	if task == nil {
		t.Fatal("task 'set resource reservations' not found")
	}

	rrTask, ok := task.(*ResourceReserveTask)
	if !ok {
		rt, ok2 := task.(ResourceReserveTask)
		if !ok2 {
			t.Fatalf("task is not a ResourceReserveTask (type is %T)", task)
		}
		rrTask = &rt
	}

	if rrTask.App != "test-app" {
		t.Errorf("App = %q, want %q", rrTask.App, "test-app")
	}
	if rrTask.ProcessType != "web" {
		t.Errorf("ProcessType = %q, want %q", rrTask.ProcessType, "web")
	}
	if len(rrTask.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(rrTask.Resources))
	}
	if rrTask.Resources["cpu"] != "100" {
		t.Errorf("Resources[cpu] = %q, want %q", rrTask.Resources["cpu"], "100")
	}
	if rrTask.Resources["memory"] != "256" {
		t.Errorf("Resources[memory] = %q, want %q", rrTask.Resources["memory"], "256")
	}
	// ClearBefore is a *bool, so an explicit clear_before: true survives decoding
	// (go-defaults leaves pointer fields untouched).
	if rrTask.ClearBefore == nil || !*rrTask.ClearBefore {
		t.Error("ClearBefore = false, want true (YAML value should be preserved)")
	}
}
