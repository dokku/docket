package tasks

import (
	"strings"
	"testing"
)

func TestHttpAuthDomainTaskInvalidState(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", Domains: []string{"app.example.com"}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthDomainTaskPresentMissingApp(t *testing.T) {
	task := HttpAuthDomainTask{Domains: []string{"app.example.com"}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskAbsentMissingApp(t *testing.T) {
	task := HttpAuthDomainTask{Domains: []string{"app.example.com"}, State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskClearMissingApp(t *testing.T) {
	task := HttpAuthDomainTask{State: StateClear}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskPresentEmptyDomains(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty domains and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'domains' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskAbsentEmptyDomains(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty domains and state=absent should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'domains' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskSetEmptyDomains(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", State: StateSet}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty domains and state=set should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'domains' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksHttpAuthDomainTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: restrict auth domains
      dokku_http_auth_domain:
        app: test-app
        domains:
          - app.example.com
          - www.example.com
        state: set
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("restrict auth domains")
	if task == nil {
		t.Fatal("task 'restrict auth domains' not found")
	}

	domainTask, ok := task.(*HttpAuthDomainTask)
	if !ok {
		t.Fatalf("task is not an HttpAuthDomainTask (type is %T)", task)
	}
	if domainTask.App != "test-app" {
		t.Errorf("App = %q, want %q", domainTask.App, "test-app")
	}
	if len(domainTask.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(domainTask.Domains))
	}
	if domainTask.Domains[0] != "app.example.com" || domainTask.Domains[1] != "www.example.com" {
		t.Errorf("Domains = %v, want [app.example.com, www.example.com]", domainTask.Domains)
	}
	if domainTask.State != StateSet {
		t.Errorf("State = %q, want %q", domainTask.State, StateSet)
	}
}
