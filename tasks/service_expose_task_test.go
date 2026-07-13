package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
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

func TestServiceExposeSameOrderInSync(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet redis:info my-svc --exposed-ports": "1111->1111 2222->2222",
	}))()

	plan := ServiceExposeTask{Service: "redis", Name: "my-svc", Ports: []string{"1111", "2222"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync when ports match in order, got %#v", plan)
	}
}

func TestServiceExposeReorderReportsDrift(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet redis:info my-svc --exposed-ports": "1111->1111 2222->2222",
	}))()

	plan := ServiceExposeTask{Service: "redis", Name: "my-svc", Ports: []string{"2222", "1111"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the same ports are reordered (dokku maps them positionally)")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("expected Modify status when already exposed, got %q", plan.Status)
	}
	foundUnexpose := false
	foundOrderedExpose := false
	for _, m := range plan.Mutations {
		if m == "redis:unexpose my-svc" {
			foundUnexpose = true
		}
		if m == "redis:expose my-svc 2222 1111" {
			foundOrderedExpose = true
		}
	}
	if !foundUnexpose {
		t.Errorf("expected an unexpose before re-expose, got %v", plan.Mutations)
	}
	if !foundOrderedExpose {
		t.Errorf("expected re-expose in the desired order, got %v", plan.Mutations)
	}
}

func TestServiceExposeCreatesWhenNotExposed(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet redis:info my-svc --exposed-ports": "",
	}))()

	plan := ServiceExposeTask{Service: "redis", Name: "my-svc", Ports: []string{"1111", "2222"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the service is not exposed")
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected Create status when not yet exposed, got %q", plan.Status)
	}
	for _, m := range plan.Mutations {
		if strings.Contains(m, "unexpose") {
			t.Errorf("did not expect an unexpose when nothing is exposed, got %v", plan.Mutations)
		}
	}
}
