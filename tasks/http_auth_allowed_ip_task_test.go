package tasks

import (
	"strings"
	"testing"
)

func TestHttpAuthAllowedIpTaskInvalidState(t *testing.T) {
	task := HttpAuthAllowedIpTask{App: "test-app", AllowedIps: []string{"192.0.2.1"}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthAllowedIpTaskPresentMissingApp(t *testing.T) {
	task := HttpAuthAllowedIpTask{AllowedIps: []string{"192.0.2.1"}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskAbsentMissingApp(t *testing.T) {
	task := HttpAuthAllowedIpTask{State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskPresentEmptyAllowedIps(t *testing.T) {
	task := HttpAuthAllowedIpTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty allowed_ips and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'allowed_ips' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestGetTasksHttpAuthAllowedIpTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: allow ips
      dokku_http_auth_allowed_ip:
        app: test-app
        allowed_ips:
          - 192.0.2.1
          - 198.51.100.0/24
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("allow ips")
	if task == nil {
		t.Fatal("task 'allow ips' not found")
	}

	allowedIpTask, ok := task.(*HttpAuthAllowedIpTask)
	if !ok {
		t.Fatalf("task is not an HttpAuthAllowedIpTask (type is %T)", task)
	}
	if allowedIpTask.App != "test-app" {
		t.Errorf("App = %q, want %q", allowedIpTask.App, "test-app")
	}
	if len(allowedIpTask.AllowedIps) != 2 {
		t.Fatalf("expected 2 allowed ips, got %d", len(allowedIpTask.AllowedIps))
	}
	if allowedIpTask.AllowedIps[0] != "192.0.2.1" || allowedIpTask.AllowedIps[1] != "198.51.100.0/24" {
		t.Errorf("AllowedIps = %v, want [192.0.2.1, 198.51.100.0/24]", allowedIpTask.AllowedIps)
	}
	if allowedIpTask.State != StatePresent {
		t.Errorf("State = %q, want %q", allowedIpTask.State, StatePresent)
	}
}
