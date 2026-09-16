package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestHttpAuthAllowedIpTaskInvalidState(t *testing.T) {
	task := HttpAuthAllowedIpTask{App: "test-app", AllowedIps: []string{"192.0.2.1"}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestHttpAuthAllowedIpTaskPresentMissingApp(t *testing.T) {
	task := HttpAuthAllowedIpTask{AllowedIps: []string{"192.0.2.1"}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskAbsentMissingApp(t *testing.T) {
	task := HttpAuthAllowedIpTask{State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskClearMissingApp(t *testing.T) {
	task := HttpAuthAllowedIpTask{State: StateClear}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskPresentEmptyAllowedIps(t *testing.T) {
	task := HttpAuthAllowedIpTask{App: "test-app", State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty allowed_ips and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'allowed_ips' must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

// TestHttpAuthAllowedIpTaskAbsentEmptyAllowedIps guards the behaviour change in
// #531: an empty list under state 'absent' used to mean "remove every address",
// which state 'clear' now owns outright.
func TestHttpAuthAllowedIpTaskAbsentEmptyAllowedIps(t *testing.T) {
	task := HttpAuthAllowedIpTask{App: "test-app", State: StateAbsent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty allowed_ips and state=absent should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'allowed_ips' must not be empty for state 'absent'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskSetEmptyAllowedIps(t *testing.T) {
	task := HttpAuthAllowedIpTask{App: "test-app", State: StateSet}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with empty allowed_ips and state=set should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'allowed_ips' must not be empty for state 'set'") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestHttpAuthAllowedIpTaskClearRejectsAllowedIps(t *testing.T) {
	// The clear path calls http-auth:set-allowed-ips with no addresses, so a
	// list here would be silently discarded rather than removed.
	task := HttpAuthAllowedIpTask{App: "test-app", AllowedIps: []string{"192.0.2.1"}, State: StateClear}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected an error when allowed_ips are supplied with state 'clear'")
	}
	if !strings.Contains(err.Error(), "'allowed_ips' must not be set for state 'clear'") {
		t.Errorf("unexpected error: %v", err)
	}
}

// TestValidHttpAuthAddress pins the port of the plugin's
// fn-http-auth-valid-address: docket must accept every spelling nginx's allow
// directive takes, `all` and a unix socket path included.
func TestValidHttpAuthAddress(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		address string
		want    bool
	}{
		{"192.0.2.1", true},
		{"198.51.100.0/24", true},
		{"010.0.0.1", true},
		{"2001:db8::1", true},
		{"2001:db8::/32", true},
		{"::", true},
		{"all", true},
		{"unix:/var/run/nginx.sock", true},
		{"", false},
		{"10.0.0.0/", false},
		{"256.0.0.1", false},
		{"192.0.2.1/33", false},
		{"192.0.2.1/24/8", false},
		{"::1/129", false},
		{"1::2::3", false},
		{":1", false},
		{"1:", false},
		{"2001:db8::g1", false},
		{"not-an-ip", false},
		{"192.0.2", false},
	} {
		if got := validHttpAuthAddress(tc.address); got != tc.want {
			t.Errorf("validHttpAuthAddress(%q) = %v, want %v", tc.address, got, tc.want)
		}
	}
}

func TestHttpAuthAllowedIpTaskRejectsAnInvalidAddress(t *testing.T) {
	t.Parallel()
	for _, state := range []State{StatePresent, StateSet} {
		task := HttpAuthAllowedIpTask{App: "test-app", AllowedIps: []string{"192.0.2.1", "10.0.0.0/"}, State: state}
		err := task.Validate()
		if err == nil {
			t.Fatalf("state %q: expected an error for a malformed address", state)
		}
		if !strings.Contains(err.Error(), "allowed_ips[1]") || !strings.Contains(err.Error(), `"10.0.0.0/"`) {
			t.Errorf("state %q: error should name the offending entry, got: %v", state, err)
		}
	}
}

// TestHttpAuthAllowedIpTaskAbsentAcceptsAnInvalidAddress keeps an address a
// pre-0.14.0 plugin stored without checking removable: http-auth:remove-allowed-ip
// does not validate, so neither does docket.
func TestHttpAuthAllowedIpTaskAbsentAcceptsAnInvalidAddress(t *testing.T) {
	t.Parallel()
	task := HttpAuthAllowedIpTask{App: "test-app", AllowedIps: []string{"10.0.0.0/"}, State: StateAbsent}
	if err := task.Validate(); err != nil {
		t.Errorf("state 'absent' must accept a stored malformed address, got: %v", err)
	}
}

// httpAuthAllowedIpReportKey is the fakeDokku key for the probe
// getHttpAuthAllowedIps runs.
const httpAuthAllowedIpReportKey = "http-auth:report web --format json"

// httpAuthAllowedIpReport builds the report body the probe parses.
func httpAuthAllowedIpReport(ips string) map[string]string {
	return map[string]string{
		httpAuthAllowedIpReportKey: `{"enabled":"true","users":"","allowed-ips":"` + ips + `","domains":""}`,
	}
}

func TestHttpAuthAllowedIpsSetPlansFullReplacement(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthAllowedIpReport("192.0.2.1 198.51.100.2")))

	plan := HttpAuthAllowedIpTask{
		App:        "web",
		AllowedIps: []string{"203.0.113.5"},
		State:      StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the desired addresses differ from the report")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	// The command carries the complete desired list, not the per-address delta.
	if !strings.HasSuffix(plan.Commands[0], "http-auth:set-allowed-ips web 203.0.113.5") {
		t.Errorf("expected http-auth:set-allowed-ips with the full desired list, got %q", plan.Commands[0])
	}
	want := []string{"add 203.0.113.5", "remove 192.0.2.1", "remove 198.51.100.2"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestHttpAuthAllowedIpsSetConvergesWhenReportMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthAllowedIpReport("198.51.100.2 192.0.2.1")))

	plan := HttpAuthAllowedIpTask{
		App:        "web",
		AllowedIps: []string{"192.0.2.1", "198.51.100.2"},
		State:      StateSet,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync once the report matches the desired set, got %#v", plan)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no planned commands when in sync, got %v", plan.Commands)
	}
}

func TestHttpAuthAllowedIpsClearPlansEveryAddress(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthAllowedIpReport("198.51.100.2 192.0.2.1")))

	plan := HttpAuthAllowedIpTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the app still carries allowed ips")
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if plan.Reason != "clear 2 allowed ip(s)" {
		t.Errorf("Reason = %q, want %q", plan.Reason, "clear 2 allowed ip(s)")
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	// Clearing is the same command with no addresses behind it.
	if !strings.HasSuffix(plan.Commands[0], "http-auth:set-allowed-ips web") {
		t.Errorf("expected a bare http-auth:set-allowed-ips, got %q", plan.Commands[0])
	}
	want := []string{"remove 192.0.2.1", "remove 198.51.100.2"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestHttpAuthAllowedIpsClearIsInSyncWithoutAddresses(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthAllowedIpReport("")))

	plan := HttpAuthAllowedIpTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync when the allow-list is already empty, got %#v", plan)
	}
	if len(plan.Commands) != 0 {
		t.Errorf("expected no planned commands when in sync, got %v", plan.Commands)
	}
}

// TestHttpAuthAllowedIpsAbsentPlansOnlyThePresentAddresses is the other half of
// #531: absent no longer enumerates the server, it only removes what the recipe
// names.
func TestHttpAuthAllowedIpsAbsentPlansOnlyThePresentAddresses(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthAllowedIpReport("192.0.2.1 203.0.113.5")))

	plan := HttpAuthAllowedIpTask{
		App:        "web",
		AllowedIps: []string{"192.0.2.1", "198.51.100.2"},
		State:      StateAbsent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusDestroy {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusDestroy)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "http-auth:remove-allowed-ip web 192.0.2.1") {
		t.Errorf("expected only the stored address to be removed, got %q", plan.Commands[0])
	}
	if want := []string{"remove 192.0.2.1"}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestHttpAuthAllowedIpsPresentPlansOnlyTheMissingAddresses(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthAllowedIpReport("192.0.2.1")))

	plan := HttpAuthAllowedIpTask{
		App:        "web",
		AllowedIps: []string{"192.0.2.1", "198.51.100.2"},
		State:      StatePresent,
	}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusModify)
	}
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one planned command, got %v", plan.Commands)
	}
	if !strings.HasSuffix(plan.Commands[0], "http-auth:add-allowed-ip web 198.51.100.2") {
		t.Errorf("expected only the missing address to be added, got %q", plan.Commands[0])
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
