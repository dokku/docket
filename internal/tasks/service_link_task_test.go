package tasks

import (
	"testing"
)

func TestServiceLinkTaskInvalidState(t *testing.T) {
	task := ServiceLinkTask{App: "test-app", Service: "redis", Name: "test-service", State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestGetTasksServiceLinkTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: link redis service
      dokku_service_link:
        app: my-app
        service: redis
        name: my-redis
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("link redis service")
	if task == nil {
		t.Fatal("task 'link redis service' not found")
	}

	slTask, ok := task.(*ServiceLinkTask)
	if !ok {
		st, ok2 := task.(ServiceLinkTask)
		if !ok2 {
			t.Fatalf("task is not a ServiceLinkTask (type is %T)", task)
		}
		slTask = &st
	}

	if slTask.App != "my-app" {
		t.Errorf("App = %q, want %q", slTask.App, "my-app")
	}
	if slTask.Service != "redis" {
		t.Errorf("Service = %q, want %q", slTask.Service, "redis")
	}
	if slTask.Name != "my-redis" {
		t.Errorf("Name = %q, want %q", slTask.Name, "my-redis")
	}
	if slTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", slTask.State)
	}
}

func TestGetTasksServiceLinkWithTemplateContext(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: link {{ .service_type }} service
      dokku_service_link:
        app: {{ .app_name }}
        service: {{ .service_type }}
        name: {{ .service_name }}
`)
	context := map[string]interface{}{
		"app_name":     "my-app",
		"service_type": "postgres",
		"service_name": "my-db",
	}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("link postgres service")
	if task == nil {
		t.Fatal("task 'link postgres service' not found")
	}

	slTask, ok := task.(*ServiceLinkTask)
	if !ok {
		st, ok2 := task.(ServiceLinkTask)
		if !ok2 {
			t.Fatalf("task is not a ServiceLinkTask (type is %T)", task)
		}
		slTask = &st
	}

	if slTask.App != "my-app" {
		t.Errorf("App = %q, want %q", slTask.App, "my-app")
	}
	if slTask.Service != "postgres" {
		t.Errorf("Service = %q, want %q", slTask.Service, "postgres")
	}
	if slTask.Name != "my-db" {
		t.Errorf("Name = %q, want %q", slTask.Name, "my-db")
	}
}
