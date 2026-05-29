package tasks

import (
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestServicePropertyTaskInvalidState(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Property: "restart-policy", Value: "always", State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestServicePropertyTaskPresentRequiresValue(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Property: "restart-policy"}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with present state and no value should return an error")
	}
}

func TestServicePropertyTaskAbsentRejectsValue(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Property: "restart-policy", Value: "always", State: StateAbsent}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with absent state and a value should return an error")
	}
}

func TestServicePropertyTaskRequiresProperty(t *testing.T) {
	task := ServicePropertyTask{Service: "redis", Name: "test-service", Value: "always"}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with no property should return an error")
	}
}

func TestGetTasksServicePropertyTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set postgres restart policy
      dokku_service_property:
        service: postgres
        name: my-db
        property: restart-policy
        value: always
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set postgres restart policy")
	if task == nil {
		t.Fatal("task 'set postgres restart policy' not found")
	}

	spTask, ok := task.(*ServicePropertyTask)
	if !ok {
		st, ok2 := task.(ServicePropertyTask)
		if !ok2 {
			t.Fatalf("task is not a ServicePropertyTask (type is %T)", task)
		}
		spTask = &st
	}

	if spTask.Service != "postgres" {
		t.Errorf("Service = %q, want %q", spTask.Service, "postgres")
	}
	if spTask.Name != "my-db" {
		t.Errorf("Name = %q, want %q", spTask.Name, "my-db")
	}
	if spTask.Property != "restart-policy" {
		t.Errorf("Property = %q, want %q", spTask.Property, "restart-policy")
	}
	if spTask.Value != "always" {
		t.Errorf("Value = %q, want %q", spTask.Value, "always")
	}
	if spTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", spTask.State)
	}
}
