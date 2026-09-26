package tasks

import (
	"testing"
)

func TestIntegrationGetTasksFullWorkflow(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-workflow"

	// ensure clean state
	destroyApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	data := []byte(`---
- tasks:
    - name: create app
      dokku_app:
        app: ` + appName + `
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	for _, name := range tasks.Keys() {
		task := tasks.Get(name)
		state := task.Execute(testCtx())
		if state.Error != nil {
			t.Fatalf("task %q failed: %v", name, state.Error)
		}
		if state.State != state.DesiredState {
			t.Errorf("task %q: expected state %q, got %q", name, state.DesiredState, state.State)
		}
	}
}

func TestIntegrationMultiTaskWorkflow(t *testing.T) {
	skipIfNoDokkuT(t)

	appName := "docket-test-multi"

	destroyApp(testCtx(), appName)
	defer destroyApp(testCtx(), appName)

	data := []byte(`---
- tasks:
    - name: create app
      dokku_app:
        app: ` + appName + `
    - name: set config
      dokku_config:
        app: ` + appName + `
        restart: false
        config:
          TEST_KEY: test_value
    - name: ensure storage
      dokku_storage_ensure:
        app: ` + appName + `
        chown: herokuish
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	if len(tasks.Keys()) != 3 {
		t.Fatalf("expected 3 tasks, got %d", len(tasks.Keys()))
	}

	for _, name := range tasks.Keys() {
		task := tasks.Get(name)
		state := task.Execute(testCtx())
		if state.Error != nil {
			t.Fatalf("task %q failed: %v", name, state.Error)
		}
		if state.State != state.DesiredState {
			t.Errorf("task %q: expected state %q, got %q", name, state.DesiredState, state.State)
		}
		if !state.Changed {
			t.Errorf("task %q: expected changed=true on first run", name)
		}
	}
}
