package tasks

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dokku/docket/internal/subprocess"
)

// The tests here cover the set and clear states added to the collection tasks
// that used to offer only present and absent (#619), and clear on
// dokku_http_auth_user. An empty set planning as clear is covered for every
// task by TestEmptySetPlansAsClear.

// commandSuffixes asserts each planned command ends with the matching suffix,
// which leaves the masked `dokku` prefix to resolveCommands.
func commandSuffixes(t *testing.T, plan PlanResult, want ...string) {
	t.Helper()
	if len(plan.Commands) != len(want) {
		t.Fatalf("Commands = %v, want %d command(s) ending %v", plan.Commands, len(want), want)
	}
	for i, suffix := range want {
		if !strings.HasSuffix(plan.Commands[i], suffix) {
			t.Errorf("Commands[%d] = %q, want it to end %q", i, plan.Commands[i], suffix)
		}
	}
}

// TestCollectionClearRejectsEntries pins that every task newly offering clear
// refuses a collection alongside it, since its clear command takes none and
// would silently discard it.
func TestCollectionClearRejectsEntries(t *testing.T) {
	t.Parallel()
	cases := []struct {
		task InputValidator
		want string
	}{
		{BuildpacksTask{App: "web", Buildpacks: []string{"https://example.com/bp.git"}, State: StateClear}, "'buildpacks' must not be set for state 'clear'"},
		{ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "1"}, State: StateClear}, "resources must not be set when state is clear"},
		{ResourceReserveTask{App: "web", Resources: map[string]string{"cpu": "1"}, State: StateClear}, "resources must not be set when state is clear"},
		{ServiceExposeTask{Service: "redis", Name: "cache", Ports: []string{"6379"}, State: StateClear}, "'ports' must not be set for state 'clear'"},
		{SchedulerK3sChartTask{Chart: "traefik", Values: map[string]any{"a": "b"}, State: StateClear}, "'values' must not be set for state 'clear'"},
		{HttpAuthUserTask{App: "web", Users: []HttpAuthUser{{Username: "admin"}}, State: StateClear}, "'users' must not be set for state 'clear'"},
	}
	for _, tc := range cases {
		err := tc.task.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%T: Validate() = %v, want an error containing %q", tc.task, err, tc.want)
		}
	}
}

func TestBuildpacksSetReplacesTheList(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet buildpacks:report web --format json": `{"list":"https://example.com/a.git,https://example.com/b.git"}`,
	}))
	plan := BuildpacksTask{App: "web", Buildpacks: []string{"https://example.com/b.git"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a modify", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "buildpacks:set --replace web https://example.com/b.git")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateSet {
		t.Errorf("apply = {Error %v, State %q}, want set", out.Error, out.State)
	}
}

func TestBuildpacksClearRunsClear(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet buildpacks:list web": "=====> web buildpack urls\nhttps://example.com/a.git",
	}))
	plan := BuildpacksTask{App: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusDestroy {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a destroy", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "buildpacks:clear web")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateClear {
		t.Errorf("apply = {Error %v, State %q}, want clear", out.Error, out.State)
	}
}

// TestResourceLimitSetClearsUndeclaredResources pins the single command a set
// plans: declared resources are written and every other set resource is
// passed the literal "clear", which dokku reads as a removal.
func TestResourceLimitSetClearsUndeclaredResources(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet resource:limit web": "cpu: 100\nmemory: 256\nnetwork: 0\nnvidia-gpu: ",
	}))
	plan := ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "200"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a modify", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "resource:limit --cpu 200 --memory clear web")
	want := []string{`set cpu=200 (was "100")`, `clear memory (was "256")`}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateSet {
		t.Errorf("apply = {Error %v, State %q}, want set", out.Error, out.State)
	}
}

func TestResourceReserveSetInSyncWhenOnlyDeclaredAreSet(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet resource:reserve --process-type worker web": "cpu: \nmemory: 256\nnetwork: 0",
	}))
	plan := ResourceReserveTask{App: "web", ProcessType: "worker", Resources: map[string]string{"memory": "256"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil || !plan.InSync {
		t.Fatalf("plan = {Error %v, InSync %v}, want in sync: unset and zero resources need no clear", plan.Error, plan.InSync)
	}
}

func TestResourceLimitSetRejectsUnknownResource(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet resource:limit web": "cpu: 100",
	}))
	plan := ResourceLimitTask{App: "web", Resources: map[string]string{"gpu": "1"}, State: StateSet}.Plan(ctx)
	if plan.Error == nil || !strings.Contains(plan.Error.Error(), "unknown resource gpu") {
		t.Errorf("plan error = %v, want an unknown-resource error", plan.Error)
	}
}

func TestResourceLimitClearRunsLimitClear(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet resource:limit --process-type web web": "cpu: 100",
	}))
	plan := ResourceLimitTask{App: "web", ProcessType: "web", State: StateClear}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusDestroy {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a destroy", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "resource:limit-clear --process-type web web")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateClear {
		t.Errorf("apply = {Error %v, State %q}, want clear", out.Error, out.State)
	}
}

func TestResourceSetValidation(t *testing.T) {
	t.Parallel()
	cases := []struct {
		task ResourceLimitTask
		want string
	}{
		{ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "1"}, ClearBefore: boolPtr(true), State: StateSet}, "clear_before is only valid when state is present, got state set"},
		{ResourceLimitTask{App: "web", ClearBefore: boolPtr(true), State: StateClear}, "clear_before is only valid when state is present, got state clear"},
		{ResourceLimitTask{App: "web", Resources: map[string]string{"cpu": "clear"}, State: StateSet}, `resource cpu must not be set to "clear" when state is set`},
	}
	for _, tc := range cases {
		err := tc.task.Validate()
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("Validate(%+v) = %v, want an error containing %q", tc.task, err, tc.want)
		}
	}
}

func TestServiceExposeSetReexposesOnExactlyThePorts(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet redis:info cache --exposed-ports": "6379->6379",
	}))
	plan := ServiceExposeTask{Service: "redis", Name: "cache", Ports: []string{"6380"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a modify", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "redis:unexpose cache", "redis:expose cache 6380")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateSet {
		t.Errorf("apply = {Error %v, State %q}, want set", out.Error, out.State)
	}
}

func TestServiceExposeClearUnexposes(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet redis:info cache --exposed-ports": "6379->6379",
	}))
	plan := ServiceExposeTask{Service: "redis", Name: "cache", State: StateClear}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusDestroy {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a destroy", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "redis:unexpose cache")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateClear {
		t.Errorf("apply = {Error %v, State %q}, want clear", out.Error, out.State)
	}
}

// TestSchedulerK3sChartSetWritesAndUnsetsPerKey pins the per-key form a chart
// set takes, since dokku has no whole-chart write: one charts:set per changed
// value, then one valueless charts:set per value the recipe does not declare.
func TestSchedulerK3sChartSetWritesAndUnsetsPerKey(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet scheduler-k3s:charts:report traefik --format json": `{"traefik.a":"1","traefik.b":"2","traefik.c":"3"}`,
	}))
	plan := SchedulerK3sChartTask{Chart: "traefik", Values: map[string]any{"a": "1", "b": "9", "d": "4"}, State: StateSet}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusModify {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a modify", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan,
		"scheduler-k3s:charts:set traefik.b 9",
		"scheduler-k3s:charts:set traefik.d 4",
		// The unset passes an empty value, as state absent's does, which
		// dokku reads as no value.
		"scheduler-k3s:charts:set traefik.c ",
	)
	want := []string{`set b=9 (was "2")`, "set d=4 (new)", `unset c (was "3")`}
	if !reflect.DeepEqual(plan.Mutations, want) {
		t.Errorf("Mutations = %v, want %v", plan.Mutations, want)
	}
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateSet {
		t.Errorf("apply = {Error %v, State %q}, want set", out.Error, out.State)
	}
}

func TestSchedulerK3sChartSetInSync(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet scheduler-k3s:charts:report traefik --format json": `{"traefik.a":"1"}`,
	}))
	plan := SchedulerK3sChartTask{Chart: "traefik", Values: map[string]any{"a": 1}, State: StateSet}.Plan(ctx)
	if plan.Error != nil || !plan.InSync {
		t.Fatalf("plan = {Error %v, InSync %v}, want in sync", plan.Error, plan.InSync)
	}
}

func TestSchedulerK3sChartClearUnsetsEveryValue(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(map[string]string{
		"--quiet scheduler-k3s:charts:report traefik --format json": `{"traefik.b":"2","traefik.a":"1","ingress-nginx.c":"3"}`,
	}))
	plan := SchedulerK3sChartTask{Chart: "traefik", State: StateClear}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusDestroy {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a destroy", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "scheduler-k3s:charts:set traefik.a ", "scheduler-k3s:charts:set traefik.b ")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateClear {
		t.Errorf("apply = {Error %v, State %q}, want clear", out.Error, out.State)
	}
}

func TestSchedulerK3sChartSetRejectsEmptyValue(t *testing.T) {
	t.Parallel()
	err := SchedulerK3sChartTask{Chart: "traefik", Values: map[string]any{"a": ""}, State: StateSet}.Validate()
	if err == nil || !strings.Contains(err.Error(), `chart value "a" must not be empty for state 'set'`) {
		t.Errorf("Validate() = %v, want the empty-value error", err)
	}
}

func TestHttpAuthUserClearRemovesEveryUser(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture(
		"alice:"+aliceHash+"\nbob:"+bobHash+"\n", "bob alice",
	)))
	plan := HttpAuthUserTask{App: "test-app", State: StateClear}.Plan(ctx)
	if plan.Error != nil || plan.InSync || plan.Status != PlanStatusDestroy {
		t.Fatalf("plan = {Error %v, InSync %v, Status %q}, want a destroy", plan.Error, plan.InSync, plan.Status)
	}
	commandSuffixes(t, plan, "http-auth:remove-user test-app alice", "http-auth:remove-user test-app bob")
	out := ExecutePlan(ctx, plan)
	if out.Error != nil || out.State != StateClear {
		t.Errorf("apply = {Error %v, State %q}, want clear", out.Error, out.State)
	}
}

func TestHttpAuthUserClearInSyncWithNoUsers(t *testing.T) {
	t.Parallel()
	ctx := subprocess.ContextWithRunner(testCtx(), fakeDokku(httpAuthUserFixture("", "")))
	plan := HttpAuthUserTask{App: "test-app", State: StateClear}.Plan(ctx)
	if plan.Error != nil || !plan.InSync {
		t.Fatalf("plan = {Error %v, InSync %v}, want in sync", plan.Error, plan.InSync)
	}
}
