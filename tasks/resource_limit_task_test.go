package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	_ "github.com/gliderlabs/sigil/builtin"
)

func TestResourceLimitTaskInvalidState(t *testing.T) {
	task := ResourceLimitTask{
		App:       "test-app",
		Resources: map[string]string{"cpu": "100"},
		State:     "invalid",
	}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestResourceLimitTaskEmptyResources(t *testing.T) {
	task := ResourceLimitTask{App: "test-app", Resources: map[string]string{}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty resources and state=present should return an error")
	}
}

func TestResourceLimitTaskNilResources(t *testing.T) {
	task := ResourceLimitTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with nil resources and state=present should return an error")
	}
}

func TestResourceLimitClearBeforeConvergesWhenMatched(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet resource:limit test-app": "cpu: 100",
	}))()

	plan := ResourceLimitTask{
		App:         "test-app",
		Resources:   map[string]string{"cpu": "100"},
		ClearBefore: boolPtr(true),
		State:       StatePresent,
	}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("clear_before should be a no-op when the server already matches, got %#v", plan)
	}
}

func TestResourceLimitClearBeforeIgnoresEmptyExtraResource(t *testing.T) {
	// memory reads as "0" (unset), so clearing before setting cpu changes nothing.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet resource:limit test-app": "cpu: 100\nmemory: 0",
	}))()

	plan := ResourceLimitTask{
		App:         "test-app",
		Resources:   map[string]string{"cpu": "100"},
		ClearBefore: boolPtr(true),
		State:       StatePresent,
	}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("clear_before should stay a no-op when non-desired keys are empty, got %#v", plan)
	}
}

func TestResourceLimitClearBeforeClearsNonDesiredResource(t *testing.T) {
	// memory holds a real value outside the desired map, so the clear must run.
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet resource:limit test-app": "cpu: 100\nmemory: 256",
	}))()

	plan := ResourceLimitTask{
		App:         "test-app",
		Resources:   map[string]string{"cpu": "100"},
		ClearBefore: boolPtr(true),
		State:       StatePresent,
	}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift: a non-desired resource must be cleared")
	}
	foundClear := false
	for _, cmd := range plan.Commands {
		if strings.Contains(cmd, "resource:limit-clear") {
			foundClear = true
		}
	}
	if !foundClear {
		t.Errorf("expected a resource:limit-clear command, got %v", plan.Commands)
	}
	foundClearMutation := false
	for _, m := range plan.Mutations {
		if m == "clear before set" {
			foundClearMutation = true
		}
	}
	if !foundClearMutation {
		t.Errorf("expected a 'clear before set' mutation, got %v", plan.Mutations)
	}
}

func TestResourceLimitSetCommandDeterministicOrder(t *testing.T) {
	// Both cpu and memory drift, so the set command carries both flags and both
	// mutations are reported. Sorting the resource keys must yield byte-identical,
	// alphabetical output on every run so plan and apply agree (issue #341).
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet resource:limit test-app": "cpu: 50\nmemory: 256",
	}))()

	task := ResourceLimitTask{
		App:       "test-app",
		Resources: map[string]string{"memory": "512", "cpu": "100"},
		State:     StatePresent,
	}

	wantCommands := []string{"dokku resource:limit --cpu 100 --memory 512 test-app"}
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

func TestGetTasksResourceLimitTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set resource limits
      dokku_resource_limit:
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

	task := tasks.Get("set resource limits")
	if task == nil {
		t.Fatal("task 'set resource limits' not found")
	}

	rlTask, ok := task.(*ResourceLimitTask)
	if !ok {
		rt, ok2 := task.(ResourceLimitTask)
		if !ok2 {
			t.Fatalf("task is not a ResourceLimitTask (type is %T)", task)
		}
		rlTask = &rt
	}

	if rlTask.App != "test-app" {
		t.Errorf("App = %q, want %q", rlTask.App, "test-app")
	}
	if rlTask.ProcessType != "web" {
		t.Errorf("ProcessType = %q, want %q", rlTask.ProcessType, "web")
	}
	if len(rlTask.Resources) != 2 {
		t.Fatalf("expected 2 resources, got %d", len(rlTask.Resources))
	}
	if rlTask.Resources["cpu"] != "100" {
		t.Errorf("Resources[cpu] = %q, want %q", rlTask.Resources["cpu"], "100")
	}
	if rlTask.Resources["memory"] != "256" {
		t.Errorf("Resources[memory] = %q, want %q", rlTask.Resources["memory"], "256")
	}
	// ClearBefore is a *bool, so an explicit clear_before: true survives decoding
	// (go-defaults leaves pointer fields untouched).
	if rlTask.ClearBefore == nil || !*rlTask.ClearBefore {
		t.Error("ClearBefore = false, want true (YAML value should be preserved)")
	}
}
