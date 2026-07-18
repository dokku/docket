package tasks

import (
	"reflect"
	"testing"

	"github.com/dokku/docket/subprocess"
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

func TestResourceReserveSetCommandDeterministicOrder(t *testing.T) {
	// Both cpu and memory drift, so the set command carries both flags and both
	// mutations are reported. Sorting the resource keys must yield byte-identical,
	// alphabetical output on every run so plan and apply agree (issue #341).
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet resource:reserve test-app": "cpu: 50\nmemory: 256",
	}))()

	task := ResourceReserveTask{
		App:       "test-app",
		Resources: map[string]string{"memory": "512", "cpu": "100"},
		State:     StatePresent,
	}

	wantCommands := []string{"dokku resource:reserve --cpu 100 --memory 512 test-app"}
	wantMutations := []string{`set cpu=100 (was "50")`, `set memory=512 (was "256")`}

	// Repeat so a reintroduced map-order bug is caught reliably rather than
	// passing by chance on a lucky iteration.
	for i := 0; i < 20; i++ {
		plan := task.Plan()
		if plan.Error != nil {
			t.Fatalf("iteration %d: unexpected plan error: %v", i, plan.Error)
		}
		if !reflect.DeepEqual(plan.Commands, wantCommands) {
			t.Fatalf("iteration %d commands = %v, want %v", i, plan.Commands, wantCommands)
		}
		if !reflect.DeepEqual(plan.Mutations, wantMutations) {
			t.Fatalf("iteration %d mutations = %v, want %v", i, plan.Mutations, wantMutations)
		}
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
