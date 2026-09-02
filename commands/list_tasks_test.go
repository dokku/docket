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
	t.Parallel()
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

// TestApplyListTasksUnnamedTasksShowResourceAddress covers the unnamed
// task path: a recipe with no `name:` is named after the resource each task
// addresses, so the listing reads back the body the user wrote. Before #427
// the loader produced `task #1 <hex>` and this display substituted the task
// type plus one guessed field.
func TestApplyListTasksUnnamedTasksShowResourceAddress(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- tasks:
    - dokku_app: { app: docket-test-list-1 }
    - dokku_app: { app: docket-test-list-2 }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	for _, want := range []string{"dokku_app[app=docket-test-list-1]", "dokku_app[app=docket-test-list-2]"} {
		if !strings.Contains(stdout, want) {
			t.Errorf("listing should include %q; got:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "task #") {
		t.Errorf("no listing line should carry a positional auto-name; got:\n%s", stdout)
	}
}

// TestApplyListTasksDistinguishesSameAppTasks is the case the old display
// heuristic got wrong: it keyed on the first App-like field, so every phase
// and option of one app's docker options collapsed onto the same label.
func TestApplyListTasksDistinguishesSameAppTasks(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- tasks:
    - dokku_docker_options: { app: api, phase: deploy, option: "--memory=512m" }
    - dokku_docker_options: { app: api, phase: build, option: "--shm-size=1g" }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	for _, want := range []string{
		"dokku_docker_options[app=api,phase=deploy,option=--memory=512m]",
		"dokku_docker_options[app=api,phase=build,option=--shm-size=1g]",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("listing should include %q; got:\n%s", want, stdout)
		}
	}
}

// TestApplyListTasksMarksProbeSupport pins the probe markers: a task type
// that cannot read its own state plans as drift on every run, so --list-tasks
// says so up front rather than leaving the user to infer it from a perpetual
// `~` in plan output. The two markers are distinct because they mean different
// things: `(never converges)` is total, `(partial probe)` converges for the
// fields it does read. A task that probes everything it manages is unmarked.
func TestApplyListTasksMarksProbeSupport(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- tasks:
    - name: unprobeable
      dokku_registry_auth:
        global: true
        server: docker.io
        username: deploy-bot
        password: examplepassword
    - name: partially probed
      dokku_git_from_image:
        app: api
        image: dokku/smoke-test-app:dockerfile
    - name: fully probed
      dokku_app: { app: api }
`)
	stdout, _, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	for _, want := range []string{
		"[0] unprobeable  (never converges)",
		"[1] partially probed  (partial probe)",
		"[2] fully probed",
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %q in the listing; got:\n%s", want, stdout)
		}
	}
	if strings.Contains(stdout, "fully probed  (") {
		t.Errorf("a fully probed task should carry no probe marker; got:\n%s", stdout)
	}
}

// TestApplyListTasksHonorsTags pins the interaction with --tags: the
// listing should omit tasks the tag filter would drop, just like the
// executor does.
func TestApplyListTasksHonorsTags(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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
	t.Parallel()
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

// TestApplyListTasksRescueWhenFailedTaskShowsUnknown is #462: a rescue
// child branching on the failing block child, the form
// docs/task-envelope.md documents, is undecidable offline for the same
// reason a `.registered.<name>` reference is - the executor binds
// `failed_task` only once a block child has failed. It renders
// [unknown], not the [when?] that reports a valid recipe as broken.
func TestApplyListTasksRescueWhenFailedTaskShowsUnknown(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      block:
        - name: create
          dokku_stub: { key: a }
      rescue:
        - name: report
          when: 'failed_task.Stderr contains "already exists"'
          dokku_stub: { key: b }
`)
	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[unknown] [rescue] report") {
		t.Errorf("expected '[unknown] [rescue] report' line; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[when?]") {
		t.Errorf("a rescue predicate on failed_task must not render [when?]; got:\n%s", stdout)
	}
}

// TestApplyListTasksFailedTaskOutsideRescueShowsWhenError is the other
// half of #462. `failed_task` is bound for a rescue child and nowhere
// else - not for a top-level task, and not for a block child of a group
// that is itself a rescue child - so dereferencing it there errors at
// run time exactly as it does here. That is a broken predicate, so it
// renders [when?] and fails the listing.
func TestApplyListTasksFailedTaskOutsideRescueShowsWhenError(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- tasks:
    - name: misplaced
      when: 'failed_task.Stderr contains "already exists"'
      dokku_stub: { key: a }
`)
	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[when?]   misplaced") {
		t.Errorf("expected the [when?] marker outside rescue scope; got:\n%s", stdout)
	}
}

// TestApplyListTasksGroupRendersChildren pins the block / rescue /
// always rendering: the group's own name appears once, followed by
// each child indented with the [block] / [rescue] / [always] phase
// label.
func TestApplyListTasksGroupRendersChildren(t *testing.T) {
	t.Parallel()
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
	t.Parallel()
	stubSet(t, "a", StubFixture{PlanError: stubExecError("plan must not run")})

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

// TestListTasksJSONMatchesSchema drives every branch of the
// --list-tasks --json emitter through one recipe and holds each line to
// docs/schemas/list-tasks-v1.schema.json. That stream is deliberately
// not the same shape as the apply/plan run stream - its play_skipped
// carries `play` where the run stream's carries `name`, and it has no
// `ts` - which is why it has its own schema file.
func TestListTasksJSONMatchesSchema(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- name: skipped play
  when: 'false'
  tasks:
    - name: never listed
      dokku_stub: { key: a }

- name: listed play
  tasks:
    - name: tagged
      tags: [core]
      dokku_stub: { key: a }
    - name: looped
      loop: [one, two]
      dokku_stub: { key: a }
    - name: when false
      when: 'false'
      dokku_stub: { key: a }
    - name: when registered
      when: 'registered.nothing.Changed'
      dokku_stub: { key: a }
    - name: deprecated
      dokku_storage_ensure:
        app: api
    - name: partially probed
      dokku_git_from_image:
        app: api
        image: dokku/smoke-test-app:dockerfile
    - name: group
      block:
        - name: in block
          dokku_stub: { key: a }
      rescue:
        - name: in rescue
          when: 'failed_task.Stderr contains "already exists"'
          dokku_stub: { key: a }
      always:
        - name: in always
          dokku_stub: { key: a }
    - name: unreachable gate
      when: 'false'
      block:
        - name: broken child
          when: '[][0] == 1'
          dokku_stub: { key: a }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--json")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)

	// Guard against the recipe silently losing a branch: if any of these
	// stop appearing, the schema is no longer being exercised end to end.
	for _, want := range []string{
		`"type":"play_skipped"`,
		`"tags":`,
		`"loop_index":`,
		`"skipped":true`,
		`"unknown":true`,
		`"when_error":true`,
		`"deprecated":true`,
		`"probe":"unsupported"`,
		`"probe":"partial"`,
		`"probe_caveat":`,
		`"group":true`,
		`"phase":"block"`,
		`"phase":"rescue"`,
		`"phase":"always"`,
	} {
		if !strings.Contains(stdout, want) {
			t.Errorf("expected %s somewhere in the stream; got:\n%s", want, stdout)
		}
	}
}

// TestApplyStartAtTaskUnknownErrors pins the up-front guard:
// --start-at-task pointing at a name that does not exist returns 1
// with the available-names hint.
func TestApplyStartAtTaskUnknownErrors(t *testing.T) {
	t.Parallel()
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

// playWhenErrorTasks is the recipe the play-level when:-error tests
// share: one play whose predicate errors, followed by one that lists
// normally.
//
// `[][0] == 1` compiles and then errors at evaluation (index out of
// range). That distinction is the whole point - a predicate that fails
// to *compile* is rejected at load, long before renderListTasks runs,
// and tests/bats/plays.bats already covers that through validate.
const playWhenErrorTasks = `---
- name: broken
  when: '[][0] == 1'
  tasks:
    - name: never listed
      dokku_stub: { key: a }

- name: fine
  tasks:
    - name: listed
      dokku_stub: { key: a }
`

// TestListTasksPlayWhenErrorExitsOne pins that a play whose when:
// predicate errors fails the listing. A play-level when: is evaluated
// against exactly the context the apply / plan loops build for it
// (buildPlayWhenContext, called identically at commands/plan.go), so an
// error here is the same error plan reports as fatal - reporting success
// would let a --list-tasks CI gate pass a recipe that cannot run.
//
// The rest of the listing still renders: the walk is not short-circuited,
// only the exit code carries the failure.
func TestListTasksPlayWhenErrorExitsOne(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, playWhenErrorTasks)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "==> Play: broken  (when error:") {
		t.Errorf("expected the broken play's header to name the evaluation error; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "never listed") {
		t.Errorf("the erroring play's tasks must not be listed; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "==> Play: fine") || !strings.Contains(stdout, "[0] listed") {
		t.Errorf("a later play should still be listed after the error; got:\n%s", stdout)
	}
}

// TestListTasksPlayWhenErrorJSONExitsOne is the same case under --json,
// and pins the half that a wrapper sees: the exit code is 1 but stdout
// still carries the complete stream. That is the exception to the
// "non-zero exit means the failure detail is on stderr" rule in
// docs/json-output.md, which holds only for load-time failures.
//
// It also exercises the `when error: <err>` form of play_skipped.reason.
// The schema has always documented both forms; only `when: <src>` was
// reachable from a test before this.
func TestListTasksPlayWhenErrorJSONExitsOne(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, playWhenErrorTasks)

	stdout, stderr, exit := runApply(t, path, "--list-tasks", "--json")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	assertLinesMatchSchema(t, listTasksSchemaPath, stdout)

	if !strings.Contains(stdout, `"type":"play_skipped"`) {
		t.Errorf("expected a play_skipped event; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"reason":"when error:`) {
		t.Errorf("expected the play_skipped reason to carry the when error: form; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, `"name":"listed"`) {
		t.Errorf("a non-zero exit must still emit the rest of the stream; got:\n%s", stdout)
	}
}

// TestListTasksTaskWhenErrorExitsOne pins that a *task*-level when: that
// errors fails the listing the same way a play-level one does. #429 left
// this at 0 because a rescue child branching on `failed_task` errored
// here while being valid at run time; #462 sorted that case into
// [unknown], so what reaches evaluation now is a predicate that errors
// whenever it is reached.
//
// As with a play, the walk is not short-circuited: the rest of the play
// still lists and only the exit code carries the failure.
func TestListTasksTaskWhenErrorExitsOne(t *testing.T) {
	t.Parallel()
	path := writeTasksFile(t, `---
- tasks:
    - name: broken predicate
      when: '[][0] == 1'
      dokku_stub: { key: a }
    - name: plain
      dokku_stub: { key: b }
`)

	stdout, stderr, exit := runApply(t, path, "--list-tasks")
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[when?]   broken predicate") {
		t.Errorf("expected the [when?] marker on the erroring task; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "plain") {
		t.Errorf("the rest of the play should still be listed; got:\n%s", stdout)
	}
}

// TestListTasksTaskWhenErrorUnderUnreachableGroupDoesNotAffectExit is the
// reachability caveat on the test above. A run never evaluates the
// children of a group whose own predicate resolved false or could not be
// resolved, so a broken child predicate there still renders [when?] but
// leaves the exit code alone - the same reason a skipped play's tasks are
// not walked at all.
func TestListTasksTaskWhenErrorUnderUnreachableGroupDoesNotAffectExit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		groupWhen string
		marker    string
	}{
		{"skipped group", `'false'`, "[skipped] gate"},
		{"unknown group", `'registered.earlier.Changed'`, "[unknown] gate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTasksFile(t, `---
- tasks:
    - name: gate
      when: `+tc.groupWhen+`
      block:
        - name: broken child
          when: '[][0] == 1'
          dokku_stub: { key: a }
`)

			stdout, stderr, exit := runApply(t, path, "--list-tasks")
			if exit != 0 {
				t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
			}
			if !strings.Contains(stdout, tc.marker) {
				t.Errorf("expected %q on the group line; got:\n%s", tc.marker, stdout)
			}
			if !strings.Contains(stdout, "[when?]   [block] broken child") {
				t.Errorf("the unreachable child should still render [when?]; got:\n%s", stdout)
			}
		})
	}
}

// TestWhenReferencesRuntimeValue drives the reference check directly.
// The identifier walk is what keeps an input named `registered_at`, a
// quoted "registered", or a field access `x.failed_task` from being
// mistaken for the run-time values, and `failed_task` counts only in
// rescue scope. The last case is the substring fallback, which the CLI
// cannot reach because every recipe predicate has already compiled.
func TestWhenReferencesRuntimeValue(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		src   string
		phase string
		want  bool
	}{
		{"registered lookup", `registered.first.Changed`, "", true},
		{"registered lookup in rescue", `registered.first.Changed`, "rescue", true},
		{"failed_task in rescue", `failed_task.Stderr contains "x"`, "rescue", true},
		{"failed_task outside rescue", `failed_task.Stderr contains "x"`, "", false},
		{"failed_task in block", `failed_task.Stderr contains "x"`, "block", false},
		{"input named registered_at", `registered_at != ""`, "rescue", false},
		{"registered as a string literal", `env == "registered"`, "rescue", false},
		{"failed_task as a field name", `payload.failed_task != nil`, "rescue", false},
		{"unparseable falls back to substring", `registered ==`, "", true},
		{"unparseable substring misses", `env ==`, "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := whenReferencesRuntimeValue(tc.src, runtimeWhenVars(tc.phase)); got != tc.want {
				t.Errorf("whenReferencesRuntimeValue(%q, phase %q) = %v, want %v", tc.src, tc.phase, got, tc.want)
			}
		})
	}
}

// TestApplyStartAtTaskSkipsEarlier confirms the executor renders
// [skipped] for tasks before the target and runs the matched task plus
// successors. The stub fixtures track which tasks actually execute.
func TestApplyStartAtTaskSkipsEarlier(t *testing.T) {
	t.Parallel()
	stubSet(t, "a", StubFixture{Changed: true})
	stubSet(t, "b", StubFixture{Changed: true})
	stubSet(t, "c", StubFixture{Changed: true})

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
	t.Parallel()
	stubSet(t, "a", StubFixture{Changed: true})
	stubSet(t, "b", StubFixture{Changed: true})
	stubSet(t, "c", StubFixture{Changed: true})

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

// TestApplyStartAtTaskThenTags is the #315 regression: tag filtering
// must run *after* the --start-at-task gate. The target task itself is
// excluded by --tags, so under the old (tags-first) order the run
// silently no-oped. The documented order selects the resume point
// first, then narrows by tags, so a later tag-matching task runs.
func TestApplyStartAtTaskThenTags(t *testing.T) {
	t.Parallel()
	stubSet(t, "a", StubFixture{Changed: true})
	stubSet(t, "b", StubFixture{Changed: true})
	stubSet(t, "c", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: first
      tags: [x]
      dokku_stub: { key: a }
    - name: second
      tags: [y]
      dokku_stub: { key: b }
    - name: third
      tags: [x]
      dokku_stub: { key: c }
`)
	stdout, stderr, exit := runApply(t, path, "--start-at-task", "second", "--tags", "x")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[skipped] first  (before --start-at-task)") {
		t.Errorf("first should be skipped before --start-at-task; got:\n%s", stdout)
	}
	// second is the resume target but carries tag y, which --tags x
	// excludes: it establishes the resume point but does not run and
	// leaves no output line.
	if strings.Contains(stdout, "second") {
		t.Errorf("second should be excluded by --tags and not appear; got:\n%s", stdout)
	}
	// third is the regression guard: the run must not silently no-op.
	if !strings.Contains(stdout, "[changed] third") {
		t.Errorf("third should run as [changed]; got:\n%s", stdout)
	}
}

// TestApplyStartAtTaskThenSkipTags pins the same ordering for the
// --skip-tags path: the resume target is dropped by --skip-tags but the
// gate still resumes so a later surviving task runs.
func TestApplyStartAtTaskThenSkipTags(t *testing.T) {
	t.Parallel()
	stubSet(t, "a", StubFixture{Changed: true})
	stubSet(t, "b", StubFixture{Changed: true})
	stubSet(t, "c", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: first
      tags: [x]
      dokku_stub: { key: a }
    - name: second
      tags: [y]
      dokku_stub: { key: b }
    - name: third
      tags: [x]
      dokku_stub: { key: c }
`)
	stdout, stderr, exit := runApply(t, path, "--start-at-task", "second", "--skip-tags", "y")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[skipped] first  (before --start-at-task)") {
		t.Errorf("first should be skipped before --start-at-task; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "second") {
		t.Errorf("second should be dropped by --skip-tags and not appear; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[changed] third") {
		t.Errorf("third should run as [changed]; got:\n%s", stdout)
	}
}

// TestApplyStartAtTaskGroupChildWithTags confirms that a resume target
// nested inside a group stays reachable even when the group's own tags
// fail the filter: descent into the resume-point group bypasses tag
// filtering so an explicitly named child is never hidden by --tags.
func TestApplyStartAtTaskGroupChildWithTags(t *testing.T) {
	t.Parallel()
	stubSet(t, "a", StubFixture{Changed: true})
	stubSet(t, "b", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: group-1
      tags: [other]
      block:
        - name: block-a
          dokku_stub: { key: a }
        - name: block-b
          dokku_stub: { key: b }
`)
	stdout, stderr, exit := runApply(t, path, "--start-at-task", "block-b", "--tags", "x")
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[skipped] [block] block-a  (before --start-at-task)") {
		t.Errorf("block-a should be skipped before --start-at-task; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[changed] [block] block-b") {
		t.Errorf("block-b should run despite the group's tag not matching --tags; got:\n%s", stdout)
	}
}
