package tasks

import (
	"testing"

	_ "github.com/gliderlabs/sigil/builtin"
)

func TestServiceExposeTaskInvalidState(t *testing.T) {
	task := ServiceExposeTask{Service: "redis", Name: "test-service", Ports: []string{"6379"}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestServiceExposeTaskPresentRequiresPorts(t *testing.T) {
	task := ServiceExposeTask{Service: "redis", Name: "test-service"}
	result := task.Plan()
	if result.Error == nil {
		t.Fatal("Plan with present state and no ports should return an error")
	}
}

func TestGetTasksServiceExposeTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: expose postgres service
      dokku_service_expose:
        service: postgres
        name: my-db
        ports:
          - "5432"
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("expose postgres service")
	if task == nil {
		t.Fatal("task 'expose postgres service' not found")
	}

	seTask, ok := task.(*ServiceExposeTask)
	if !ok {
		st, ok2 := task.(ServiceExposeTask)
		if !ok2 {
			t.Fatalf("task is not a ServiceExposeTask (type is %T)", task)
		}
		seTask = &st
	}

	if seTask.Service != "postgres" {
		t.Errorf("Service = %q, want %q", seTask.Service, "postgres")
	}
	if seTask.Name != "my-db" {
		t.Errorf("Name = %q, want %q", seTask.Name, "my-db")
	}
	if len(seTask.Ports) != 1 || seTask.Ports[0] != "5432" {
		t.Errorf("Ports = %v, want [5432]", seTask.Ports)
	}
	if seTask.State != StatePresent {
		t.Errorf("expected default state 'present', got %q", seTask.State)
	}
}

func TestServiceExposePortSetEqual(t *testing.T) {
	cases := []struct {
		name string
		a    map[string]bool
		b    map[string]bool
		want bool
	}{
		{"empty", map[string]bool{}, map[string]bool{}, true},
		{"same", map[string]bool{"5432": true}, map[string]bool{"5432": true}, true},
		{"different-len", map[string]bool{"5432": true}, map[string]bool{}, false},
		{"different-members", map[string]bool{"5432": true}, map[string]bool{"6379": true}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := portSetEqual(tc.a, tc.b); got != tc.want {
				t.Errorf("portSetEqual(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
