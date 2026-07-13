package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
	_ "github.com/gliderlabs/sigil/builtin"
)

func TestDomainsTaskInvalidState(t *testing.T) {
	task := DomainsTask{App: "test-app", Domains: []string{"example.com"}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestDomainsTaskMissingApp(t *testing.T) {
	task := DomainsTask{Domains: []string{"example.com"}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestDomainsTaskGlobalWithApp(t *testing.T) {
	task := DomainsTask{App: "test-app", Global: true, Domains: []string{"example.com"}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestDomainsTaskEmptyDomains(t *testing.T) {
	states := []State{StatePresent, StateAbsent, StateSet}
	for _, s := range states {
		task := DomainsTask{App: "test-app", Domains: []string{}, State: s}
		result := task.Execute()
		if result.Error == nil {
			t.Fatalf("Execute with empty domains and state=%s should return an error", s)
		}
	}
}

func TestDomainsTaskClearNoDomains(t *testing.T) {
	task := DomainsTask{App: "test-app", State: StateClear}
	result := task.Execute()
	// Should fail because dokku isn't running, but NOT because of missing domains
	if result.Error != nil && strings.Contains(result.Error.Error(), "must not be empty") {
		t.Error("clear state should not require domains")
	}
}

// assertNoGlobalPositional fails when any planned command carries the literal
// "--global" token, which the *-global domains subcommands would write as a
// real domain (issue #309).
func assertNoGlobalPositional(t *testing.T, commands []string) {
	t.Helper()
	if len(commands) == 0 {
		t.Fatal("expected at least one planned command")
	}
	for _, cmd := range commands {
		for _, field := range strings.Fields(cmd) {
			if field == "--global" {
				t.Errorf("planned command must not pass --global as a positional: %q", cmd)
			}
		}
	}
}

func TestDomainsGlobalSetOmitsGlobalPositional(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"domains:report --global --domains-global-vhosts": "",
	}))()

	plan := DomainsTask{Global: true, Domains: []string{"global.example.com"}, State: StateSet}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the global vhost is unset")
	}
	assertNoGlobalPositional(t, plan.Commands)
	if !strings.Contains(plan.Commands[0], "domains:set-global") {
		t.Errorf("expected domains:set-global command, got %q", plan.Commands[0])
	}
	if !strings.Contains(plan.Commands[0], "global.example.com") {
		t.Errorf("expected the desired domain in the command, got %q", plan.Commands[0])
	}
}

func TestDomainsGlobalSetConvergesWhenReportMatches(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"domains:report --global --domains-global-vhosts": "global.example.com",
	}))()

	plan := DomainsTask{Global: true, Domains: []string{"global.example.com"}, State: StateSet}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync once the global vhost matches, got %#v", plan)
	}
}

func TestDomainsGlobalAddOmitsGlobalPositional(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"domains:report --global --domains-global-vhosts": "",
	}))()

	plan := DomainsTask{Global: true, Domains: []string{"global.example.com"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the global vhost is unset")
	}
	assertNoGlobalPositional(t, plan.Commands)
	if !strings.Contains(plan.Commands[0], "domains:add-global") {
		t.Errorf("expected domains:add-global command, got %q", plan.Commands[0])
	}
}

func TestDomainsGlobalClearOmitsGlobalPositional(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"domains:report --global --domains-global-vhosts": "old.example.com",
	}))()

	plan := DomainsTask{Global: true, State: StateClear}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when a global vhost is present")
	}
	assertNoGlobalPositional(t, plan.Commands)
	if !strings.Contains(plan.Commands[0], "domains:clear-global") {
		t.Errorf("expected domains:clear-global command, got %q", plan.Commands[0])
	}
}

func TestGetTasksDomainsTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: add domains
      dokku_domains:
        app: test-app
        domains:
          - example.com
          - www.example.com
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("add domains")
	if task == nil {
		t.Fatal("task 'add domains' not found")
	}

	dTask, ok := task.(*DomainsTask)
	if !ok {
		dt, ok2 := task.(DomainsTask)
		if !ok2 {
			t.Fatalf("task is not a DomainsTask (type is %T)", task)
		}
		dTask = &dt
	}

	if dTask.App != "test-app" {
		t.Errorf("App = %q, want %q", dTask.App, "test-app")
	}
	if len(dTask.Domains) != 2 {
		t.Fatalf("expected 2 domains, got %d", len(dTask.Domains))
	}
	if dTask.Domains[0] != "example.com" {
		t.Errorf("Domains[0] = %q, want %q", dTask.Domains[0], "example.com")
	}
	if dTask.Domains[1] != "www.example.com" {
		t.Errorf("Domains[1] = %q, want %q", dTask.Domains[1], "www.example.com")
	}
	if dTask.State != StatePresent {
		t.Errorf("expected state 'present', got %q", dTask.State)
	}
}

func TestGetTasksDomainsTaskGlobalParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: set global domains
      dokku_domains:
        global: true
        domains:
          - global.example.com
        state: set
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("set global domains")
	if task == nil {
		t.Fatal("task 'set global domains' not found")
	}

	dTask, ok := task.(*DomainsTask)
	if !ok {
		dt, ok2 := task.(DomainsTask)
		if !ok2 {
			t.Fatalf("task is not a DomainsTask (type is %T)", task)
		}
		dTask = &dt
	}

	if !dTask.Global {
		t.Error("Global = false, want true")
	}
	if len(dTask.Domains) != 1 {
		t.Fatalf("expected 1 domain, got %d", len(dTask.Domains))
	}
	if dTask.Domains[0] != "global.example.com" {
		t.Errorf("Domains[0] = %q, want %q", dTask.Domains[0], "global.example.com")
	}
	if dTask.State != StateSet {
		t.Errorf("expected state 'set', got %q", dTask.State)
	}
}
