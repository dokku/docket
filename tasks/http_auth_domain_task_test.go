package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestHttpAuthDomainTaskInvalidState(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", Domains: []string{"app.example.com"}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthDomainTaskPresentMissingApp(t *testing.T) {
	task := HttpAuthDomainTask{Domains: []string{"app.example.com"}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskAbsentMissingApp(t *testing.T) {
	task := HttpAuthDomainTask{Domains: []string{"app.example.com"}, State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskClearMissingApp(t *testing.T) {
	task := HttpAuthDomainTask{State: StateClear}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskPresentEmptyDomains(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty domains and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'domains' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskAbsentEmptyDomains(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty domains and state=absent should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'domains' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskSetEmptyDomains(t *testing.T) {
	task := HttpAuthDomainTask{App: "test-app", State: StateSet}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty domains and state=set should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'domains' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthDomainTaskClearRejectsDomains(t *testing.T) {
	// The clear path calls http-auth:set-domains with no domains, so a list
	// here would be silently discarded rather than removed.
	task := HttpAuthDomainTask{App: "test-app", Domains: []string{"app.example.com"}, State: StateClear}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected an error when domains are supplied with state 'clear'")
	}
	if !strings.Contains(err.Error(), "'domains' must not be set for state 'clear'") {
		t.Errorf("unexpected error: %v", err)
	}
}

// httpAuthDomainReport builds the report body getHttpAuthDomains parses. Both
// http-auth probes read the same blob, so it reuses httpAuthAllowedIpReportKey.
func httpAuthDomainReport(domains string) map[string]string {
	return map[string]string{
		httpAuthAllowedIpReportKey: `{"enabled":"true","users":"","allowed-ips":"","domains":"` + domains + `"}`,
	}
}

func TestHttpAuthDomainsSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthDomainReport("www.example.com app.example.com")))

	plan := HttpAuthDomainTask{
		App:     "web",
		Domains: []string{"api.example.com"},
		State:   StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the desired domains differ from the report")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	// The command carries the complete desired list, not the per-domain delta.
	if !strings.HasSuffix(plan.Commands[0], "http-auth:set-domains web api.example.com") {
		t.Errorf("expected http-auth:set-domains with the full desired list, got %q", plan.Commands[0])
	}
	want := []string{"add api.example.com", "remove app.example.com", "remove www.example.com"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestHttpAuthDomainsSetOnAppWithNoDomainsIsACreate(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthDomainReport("")))

	plan := HttpAuthDomainTask{
		App:     "web",
		Domains: []string{"api.example.com"},
		State:   StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
}

func TestHttpAuthDomainsClearPlansEveryDomain(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthDomainReport("www.example.com app.example.com")))

	plan := HttpAuthDomainTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the app has domains to clear")
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	// Clearing is http-auth:set-domains with no domains at all.
	if !strings.HasSuffix(plan.Commands[0], "http-auth:set-domains web") {
		t.Errorf("expected a bare http-auth:set-domains command, got %q", plan.Commands[0])
	}
	want := []string{"remove app.example.com", "remove www.example.com"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
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
