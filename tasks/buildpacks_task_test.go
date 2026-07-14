package tasks

import (
	"strings"
	"testing"

	"github.com/dokku/docket/subprocess"
)

// assertBuildpacksReplaceInOrder asserts the plan issues a single
// buildpacks:set --replace command listing the desired buildpacks in order.
func assertBuildpacksReplaceInOrder(t *testing.T, plan PlanResult, want []string) {
	t.Helper()
	if len(plan.Commands) != 1 {
		t.Fatalf("expected exactly one replace command, got %v", plan.Commands)
	}
	cmd := plan.Commands[0]
	if !strings.Contains(cmd, "buildpacks:set") || !strings.Contains(cmd, "--replace") {
		t.Errorf("expected a buildpacks:set --replace command, got %q", cmd)
	}
	last := -1
	for _, bp := range want {
		idx := strings.Index(cmd, bp)
		if idx < 0 {
			t.Errorf("buildpack %q missing from command %q", bp, cmd)
			continue
		}
		if idx < last {
			t.Errorf("buildpack %q is out of order in command %q", bp, cmd)
		}
		last = idx
	}
}

func TestBuildpacksTaskInvalidState(t *testing.T) {
	task := BuildpacksTask{App: "test-app", Buildpacks: []string{"https://example.com/bp.git"}, State: "invalid"}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with invalid state should return an error")
	}
}

func TestBuildpacksTaskPresentMissingApp(t *testing.T) {
	task := BuildpacksTask{Buildpacks: []string{"https://example.com/bp.git"}, State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuildpacksTaskAbsentMissingApp(t *testing.T) {
	task := BuildpacksTask{State: StateAbsent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute without app should return an error")
	}
	if !strings.Contains(result.Error.Error(), "'app' is required") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuildpacksTaskPresentEmptyBuildpacks(t *testing.T) {
	task := BuildpacksTask{App: "test-app", State: StatePresent}
	result := task.Execute()
	if result.Error == nil {
		t.Fatal("Execute with empty buildpacks and state=present should return an error")
	}
	if !strings.Contains(result.Error.Error(), "must not be empty") {
		t.Errorf("unexpected error: %v", result.Error)
	}
}

func TestBuildpacksSameOrderInSync(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet buildpacks:report test-app --format json": `{"list":"nodejs,nginx"}`,
	}))()

	plan := BuildpacksTask{App: "test-app", Buildpacks: []string{"nodejs", "nginx"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if !plan.InSync {
		t.Fatalf("expected in-sync when the ordered lists match, got %#v", plan)
	}
}

func TestBuildpacksReorderReportsDrift(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet buildpacks:report test-app --format json": `{"list":"nginx,nodejs"}`,
	}))()

	plan := BuildpacksTask{App: "test-app", Buildpacks: []string{"nodejs", "nginx"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the buildpack order differs (order sets build precedence)")
	}
	if plan.Status != PlanStatusModify {
		t.Errorf("expected Modify status when buildpacks already exist, got %q", plan.Status)
	}
	assertBuildpacksReplaceInOrder(t, plan, []string{"nodejs", "nginx"})
}

func TestBuildpacksPartialSetUsesReplaceInOrder(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet buildpacks:report test-app --format json": `{"list":"nginx"}`,
	}))()

	plan := BuildpacksTask{App: "test-app", Buildpacks: []string{"nodejs", "nginx"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when the current list is incomplete")
	}
	// A plain append would have yielded [nginx, nodejs]; --replace restores order.
	assertBuildpacksReplaceInOrder(t, plan, []string{"nodejs", "nginx"})
}

func TestBuildpacksCreateWhenEmpty(t *testing.T) {
	defer subprocess.SetExecRunner(fakeDokku(map[string]string{
		"--quiet buildpacks:report test-app --format json": `{"list":""}`,
	}))()

	plan := BuildpacksTask{App: "test-app", Buildpacks: []string{"nodejs"}, State: StatePresent}.Plan()
	if plan.Error != nil {
		t.Fatalf("unexpected plan error: %v", plan.Error)
	}
	if plan.InSync {
		t.Fatal("expected drift when no buildpacks are set")
	}
	if plan.Status != PlanStatusCreate {
		t.Errorf("expected Create status on a fresh app, got %q", plan.Status)
	}
}

func TestGetTasksBuildpacksTaskParsedCorrectly(t *testing.T) {
	data := []byte(`---
- tasks:
    - name: add buildpacks
      dokku_buildpacks:
        app: test-app
        buildpacks:
          - https://github.com/heroku/heroku-buildpack-nodejs.git
          - https://github.com/heroku/heroku-buildpack-nginx.git
        state: present
`)
	context := map[string]interface{}{}

	tasks, err := GetTasks(data, context)
	if err != nil {
		t.Fatalf("GetTasks failed: %v", err)
	}

	task := tasks.Get("add buildpacks")
	if task == nil {
		t.Fatal("task 'add buildpacks' not found")
	}

	bpTask, ok := task.(*BuildpacksTask)
	if !ok {
		t.Fatalf("task is not a BuildpacksTask (type is %T)", task)
	}
	if bpTask.App != "test-app" {
		t.Errorf("App = %q, want %q", bpTask.App, "test-app")
	}
	if len(bpTask.Buildpacks) != 2 {
		t.Fatalf("expected 2 buildpacks, got %d", len(bpTask.Buildpacks))
	}
	if bpTask.State != StatePresent {
		t.Errorf("State = %q, want %q", bpTask.State, StatePresent)
	}
}
