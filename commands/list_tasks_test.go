package commands

import (
	"strings"
	"testing"
)

// TestApplyListTasksPrintsResolvedPlan covers the headline path: a
// recipe with two tagged tasks should print one line per task without
// invoking the executor. The stub task fixture is intentionally left
// unset so a stray Execute() call would render as a [changed] line and
// trip one of the assertions below.
func TestApplyListTasksPrintsResolvedPlan(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: create api
      tags: [core]
      dokku_stub: { key: a }
    - name: deploy api
      tags: [deploy]
      dokku_stub: { key: b }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[0] create api") {
		t.Errorf("expected '[0] create api' line; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[1] deploy api") {
		t.Errorf("expected '[1] deploy api' line; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[tags=core]") || !strings.Contains(stdout, "[tags=deploy]") {
		t.Errorf("expected tags suffix on each line; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[changed]") || strings.Contains(stdout, "[ok]") {
		t.Errorf("--list-tasks must not invoke the executor; got:\n%s", stdout)
	}
}

// TestApplyListTasksHonorsTags pins the interaction with --tags: the
// listing should omit tasks the tag filter would drop, just like the
// executor does.
func TestApplyListTasksHonorsTags(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: api task
      tags: [api]
      dokku_stub: { key: a }
    - name: worker task
      tags: [worker]
      dokku_stub: { key: b }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks", "--tags", "api")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "api task") {
		t.Errorf("api task should appear; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "worker task") {
		t.Errorf("worker task should be filtered; got:\n%s", stdout)
	}
}

// TestApplyListTasksLoopExpansion confirms that `loop:` envelopes are
// expanded in the listing - one line per iteration, with the iteration
// suffix already rendered into the name.
func TestApplyListTasksLoopExpansion(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      loop: [api, worker, web]
      dokku_stub: { key: "{{ .item }}" }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	for _, want := range []string{"item=api", "item=worker", "item=web"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("listing should include %q; got:\n%s", want, stdout)
		}
	}
}

// TestApplyListTasksWhenFalseShowsSkipped pins the [skipped] marker for
// envelopes whose static when: predicate evaluates false against the
// inputs context.
func TestApplyListTasksWhenFalseShowsSkipped(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: gated
      when: 'false'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[skipped] gated") {
		t.Errorf("expected '[skipped] gated' line; got:\n%s", stdout)
	}
}

// TestApplyListTasksWhenRegisteredShowsUnknown covers the
// `.registered.<name>` case: --list-tasks cannot evaluate predicates
// that depend on prior task state, so the line renders [unknown]
// rather than [skipped].
func TestApplyListTasksWhenRegisteredShowsUnknown(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: first
      register: first
      dokku_stub: { key: a }
    - name: depends-on-first
      when: 'registered.first.Changed'
      dokku_stub: { key: b }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[unknown] depends-on-first") {
		t.Errorf("expected '[unknown] depends-on-first' line; got:\n%s", stdout)
	}
}

// TestApplyListTasksGroupRendersChildren pins the block / rescue /
// always rendering: the group's own name appears once, followed by
// each child indented with the [block] / [rescue] / [always] phase
// label.
func TestApplyListTasksGroupRendersChildren(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: deploy with rollback
      block:
        - name: clone
          dokku_stub: { key: a }
        - name: sync
          dokku_stub: { key: b }
      rescue:
        - name: cleanup
          dokku_stub: { key: c }
      always:
        - name: log
          dokku_stub: { key: d }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	for _, want := range []string{"deploy with rollback", "[block] clone", "[block] sync", "[rescue] cleanup", "[always] log"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("listing should include %q; got:\n%s", want, stdout)
		}
	}
}

// TestPlanListTasksWorks covers --list-tasks on the plan path. The
// listing should appear without any plan-time probe being run. We
// verify by confirming a stub key's PlanError is never surfaced.
func TestPlanListTasksWorks(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{PlanError: stubExecError("plan must not run")})

	path := writeTasksFile(t, `---
- tasks:
    - name: probe
      dokku_stub: { key: a }
`)
	stdout, stderr, exit := runPlan(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[0] probe") {
		t.Errorf("expected '[0] probe' line; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "plan must not run") || strings.Contains(stderr, "plan must not run") {
		t.Errorf("Plan() should not have run; got stdout=%s stderr=%s", stdout, stderr)
	}
}

// TestApplyStartAtTaskUnknownErrors pins the up-front guard:
// --start-at-task pointing at a name that does not exist returns 1
// with the available-names hint.
func TestApplyStartAtTaskUnknownErrors(t *testing.T) {
	defer stubReset()
	path := writeTasksFile(t, `---
- tasks:
    - name: first
      dokku_stub: { key: a }
    - name: second
      dokku_stub: { key: b }
`)
	_, stderr, exit := runApply(t, path, "--start-at-task", "no-such-task")
	if exit == 0 {
		t.Fatal("expected non-zero exit when --start-at-task misses")
	}
	for _, want := range []string{`--start-at-task "no-such-task"`, `"first"`, `"second"`, "no task matched name"} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, stderr)
		}
	}
}

// TestApplyStartAtTaskSkipsEarlier confirms the executor renders
// [skipped] for tasks before the target and runs the matched task plus
// successors. The stub fixtures track which tasks actually execute.
func TestApplyStartAtTaskSkipsEarlier(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: true})
	stubSet("c", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: first
      dokku_stub: { key: a }
    - name: second
      dokku_stub: { key: b }
    - name: third
      dokku_stub: { key: c }
`)
	stdout, stderr, exit := runApply(t, path, "--start-at-task", "second")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[skipped] first  (before --start-at-task)") {
		t.Errorf("first should be skipped with reason; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[changed] second") {
		t.Errorf("second should run as [changed]; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[changed] third") {
		t.Errorf("third should run as [changed]; got:\n%s", stdout)
	}
}

// TestApplyStartAtTaskInsideBlock pins the block-child interaction:
// matching a child of a block enters the group, skips earlier
// children, runs the matched child onward, and continues with rescue
// / always per normal block semantics. We verify the matched child
// runs and the earlier child does not.
func TestApplyStartAtTaskInsideBlock(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: true})
	stubSet("c", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: group-1
      block:
        - name: block-a
          dokku_stub: { key: a }
        - name: block-b
          dokku_stub: { key: b }
        - name: block-c
          dokku_stub: { key: c }
`)
	stdout, stderr, exit := runApply(t, path, "--start-at-task", "block-b")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[skipped] [block] block-a  (before --start-at-task)") {
		t.Errorf("block-a should be skipped with the reason; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[changed] [block] block-b") {
		t.Errorf("block-b should run as changed; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[changed] [block] block-c") {
		t.Errorf("block-c should run after the match; got:\n%s", stdout)
	}
}
