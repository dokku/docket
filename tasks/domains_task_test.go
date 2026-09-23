package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

func TestDomainsTaskInvalidState(t *testing.T) {
	t.Parallel()
	task := DomainsTask{App: "test-app", Domains: []string{"example.com"}, State: "invalid"}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestDomainsTaskMissingApp(t *testing.T) {
	t.Parallel()
	task := DomainsTask{Domains: []string{"example.com"}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("Execute without app and global=false should return an error")
	}
}

func TestDomainsTaskGlobalWithApp(t *testing.T) {
	t.Parallel()
	task := DomainsTask{App: "test-app", Global: true, Domains: []string{"example.com"}, State: StatePresent}
	result := task.Execute(testCtx())
	if result.Error == nil {
		t.Fatal("expected error when both global and app are set")
	}
	if !strings.Contains(result.Error.Error(), "must not be set when 'global' is set to true") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestDomainsTaskEmptyDomains(t *testing.T) {
	t.Parallel()
	states := []State{StatePresent, StateAbsent, StateSet}
	for _, s := range states {
		task := DomainsTask{App: "test-app", Domains: []string{}, State: s}
		result := task.Execute(testCtx())
		if result.Error == nil {
			t.Fatalf("Execute with empty domains and state=%s should return an error", s)
		}
	}
}

func TestDomainsTaskClearRejectsDomains(t *testing.T) {
	t.Parallel()
	// domains:clear takes no domains, so a list here would be silently
	// discarded rather than removed.
	task := DomainsTask{App: "test-app", Domains: []string{"example.com"}, State: StateClear}
	err := task.Validate()
	if err == nil {
		t.Fatal("expected an error when domains are supplied with state 'clear'")
	}
	if !strings.Contains(err.Error(), "'domains' must not be set for state 'clear'") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestDomainsTaskClearNoDomains(t *testing.T) {
	t.Parallel()
	task := DomainsTask{App: "test-app", State: StateClear}
	result := task.Execute(testCtx())
	// Should fail because dokku isn't running, but NOT because of missing domains
	if result.Error != nil && strings.Contains(result.Error.Error(), "must not be empty") {
		t.Error("clear state should not require domains")
	}
}

// domainsAppReportKey and domainsGlobalReportKey are the fakeDokku keys for the
// probe getDomains runs in each scope.
const (
	domainsAppReportKey    = "domains:report web --domains-app-vhosts"
	domainsGlobalReportKey = "domains:report --global --domains-global-vhosts"
)

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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsGlobalReportKey: "",
	}))

	plan := DomainsTask{Global: true, Domains: []string{"global.example.com"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the global vhost is unset")
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
	assertNoGlobalPositional(t, plan.Commands)
	if !strings.Contains(plan.Commands[0], "domains:set-global") {
		t.Errorf("expected domains:set-global command, got %q", plan.Commands[0])
	}
	if !strings.Contains(plan.Commands[0], "global.example.com") {
		t.Errorf("expected the desired domain in the command, got %q", plan.Commands[0])
	}
	if want := []string{"add global.example.com"}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestDomainsGlobalSetConvergesWhenReportMatches(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsGlobalReportKey: "global.example.com",
	}))

	plan := DomainsTask{Global: true, Domains: []string{"global.example.com"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync once the global vhost matches, got %#v", plan)
	}
}

func TestDomainsGlobalAddOmitsGlobalPositional(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsGlobalReportKey: "",
	}))

	plan := DomainsTask{Global: true, Domains: []string{"global.example.com"}, State: StatePresent}.Plan(ctx)
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
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsGlobalReportKey: "old.example.com",
	}))

	plan := DomainsTask{Global: true, State: StateClear}.Plan(ctx)
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
	if want := []string{"remove old.example.com"}; !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

// TestDomainsSetPlansSortedMutations pins the itemized diff of a full
// replacement. The desired list is deliberately unsorted: the mutation lines
// read in sorted order however the recipe wrote them, while the command still
// carries the declared list verbatim.
func TestDomainsSetPlansSortedMutations(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsAppReportKey: "c.example.com a.example.com b.example.com",
	}))

	plan := DomainsTask{
		App:     "web",
		Domains: []string{"z.example.com", "m.example.com", "a.example.com"},
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
	if !strings.HasSuffix(plan.Commands[0], "domains:set web z.example.com m.example.com a.example.com") {
		t.Errorf("expected domains:set with the full desired list, got %q", plan.Commands[0])
	}
	want := []string{
		"add m.example.com",
		"add z.example.com",
		"remove b.example.com",
		"remove c.example.com",
	}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

func TestDomainsSetOnAppWithNoDomainsIsACreate(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsAppReportKey: "",
	}))

	plan := DomainsTask{App: "web", Domains: []string{"a.example.com"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("Status = %q, want %q", plan.Status, PlanStatusCreate)
	}
}

func TestDomainsClearPlansSortedMutations(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		domainsAppReportKey: "c.example.com a.example.com b.example.com",
	}))

	plan := DomainsTask{App: "web", State: StateClear}.Plan(ctx)
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
	// domains:clear takes the app and nothing else.
	if !strings.HasSuffix(plan.Commands[0], "domains:clear web") {
		t.Errorf("expected a bare domains:clear command, got %q", plan.Commands[0])
	}
	want := []string{"remove a.example.com", "remove b.example.com", "remove c.example.com"}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
}

// TestDomainsPlanMutationsAreStableAcrossRuns is the regression test for issue
// #556. The set and clear planners used to format their mutation lines straight
// out of a Go map, so two plans of an unchanged server itemized the same drift
// in a different order and a JSON consumer diffing them saw churn that was not
// there. The exact-slice assertions above can pass by luck on a single run;
// this one cannot.
func TestDomainsPlanMutationsAreStableAcrossRuns(t *testing.T) {
	t.Parallel()
	const report = "d.example.com b.example.com e.example.com a.example.com c.example.com"
	cases := map[string]DomainsTask{
		"set": {
			App:     "web",
			Domains: []string{"e.example.com", "z.example.com", "a.example.com"},
			State:   StateSet,
		},
		"clear": {App: "web", State: StateClear},
	}

	for name, task := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
				domainsAppReportKey: report,
			}))

			first := task.Plan(ctx)
			if first.Error != nil {
				t.Fatalf("unexpected plan error: %v", first.Error)
			}
			if len(first.Mutations) < 2 {
				t.Fatalf("need several mutations to detect a reordering, got %v", first.Mutations)
			}
			for i := 0; i < 50; i++ {
				got := task.Plan(ctx).Mutations
				if !reflect.DeepEqual(got, first.Mutations) {
					t.Fatalf("run %d reordered the mutations: %v, want %v", i+2, got, first.Mutations)
				}
			}
		})
	}
}

func TestGetTasksDomainsTaskParsedCorrectly(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
