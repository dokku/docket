package commands

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/josegonzalez/cli-skeleton/command"
	"github.com/mitchellh/cli"
)

// These tests drive the multi-play executor on the plan path so the
// assertions stay deterministic without a Dokku server. Apply-only
// behaviour (--fail-fast vs default abort-current-play) is exercised
// against real Dokku in tests/bats/plays.bats.

func writeMultiPlayTasks(t *testing.T, body string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "tasks.yml")
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("write tasks.yml: %v", err)
	}
	return path
}

// runPlan stages os.Args because PlanCommand.FlagSet() reads --tasks
// from os.Args (so it can pre-register input flags before flag.Parse
// runs against the per-call args).
func runPlan(t *testing.T, path string, args ...string) (string, string, int) {
	t.Helper()
	origArgs := os.Args
	os.Args = []string{"docket-test", "plan", "--tasks", path}
	t.Cleanup(func() { os.Args = origArgs })

	c := &PlanCommand{}
	c.Meta = command.Meta{Ui: cli.NewMockUi()}
	all := append([]string{"--tasks", path}, args...)
	exit := c.Run(all)
	ui := c.Ui.(*cli.MockUi)
	return ui.OutputWriter.String(), ui.ErrorWriter.String(), exit
}

// TestMultiPlayPlanRunsAllPlaysInOrder is the headline multi-play case:
// two plays both produce a `==> Play:` header in source order.
func TestMultiPlayPlanRunsAllPlaysInOrder(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: noop-api
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop-worker
      dokku_app:
        app: docket-test-noop-worker
`)
	stdout, _, _ := runPlan(t, path)
	idxAPI := strings.Index(stdout, "==> Play: api")
	idxWorker := strings.Index(stdout, "==> Play: worker")
	if idxAPI < 0 || idxWorker < 0 {
		t.Fatalf("missing play headers; got:\n%s", stdout)
	}
	if idxAPI > idxWorker {
		t.Errorf("plays out of order; api should come before worker; got:\n%s", stdout)
	}
}

// TestMultiPlayPlayFilterRunsOnlyNamedPlay covers --play. Only the
// matched play should produce a header; the other play's tasks should
// not appear.
func TestMultiPlayPlayFilterRunsOnlyNamedPlay(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: noop-api
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop-worker
      dokku_app:
        app: docket-test-noop-worker
`)
	stdout, _, _ := runPlan(t, path, "--play", "api")
	if !strings.Contains(stdout, "==> Play: api") {
		t.Errorf("api play header missing; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "==> Play: worker") {
		t.Errorf("worker play should be filtered out; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "noop-worker") {
		t.Errorf("worker tasks should not appear; got:\n%s", stdout)
	}
}

// TestMultiPlayPlayFilterUnknownNameErrors verifies the user sees a
// useful diagnostic when --play does not match any play, including the
// available play names so they can correct the typo.
func TestMultiPlayPlayFilterUnknownNameErrors(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-worker
`)
	_, stderr, exit := runPlan(t, path, "--play", "missing")
	if exit == 0 {
		t.Error("expected non-zero exit when --play does not match")
	}
	for _, want := range []string{`--play "missing"`, `"api"`, `"worker"`} {
		if !strings.Contains(stderr, want) {
			t.Errorf("stderr missing %q; got:\n%s", want, stderr)
		}
	}
}

// TestMultiPlayPlayFilterComposesWithTags pins the --play + --tags
// composition: --play narrows to one play, --tags then filters tasks
// within that play.
func TestMultiPlayPlayFilterComposesWithTags(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tasks:
    - name: deploy-api
      tags: [deploy]
      dokku_stub: { key: deploy-api }
    - name: configure-api
      tags: [configure]
      dokku_stub: { key: configure-api }
- name: worker
  tasks:
    - name: deploy-worker
      tags: [deploy]
      dokku_stub: { key: deploy-worker }
`)
	stdout, _, _ := runPlan(t, path, "--play", "api", "--tags", "deploy")
	if !strings.Contains(stdout, "deploy-api") {
		t.Errorf("deploy-api should run; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "configure-api") {
		t.Errorf("configure-api should be filtered by --tags; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "deploy-worker") {
		t.Errorf("worker play tasks should be filtered by --play; got:\n%s", stdout)
	}
}

// TestMultiPlayPlayLevelTagsPropagateToTasks: --tags filters against
// the task's effective tag set, which includes the play's tags. A task
// with no per-task tag still passes a --tags filter that matches the
// play tag.
func TestMultiPlayPlayLevelTagsPropagateToTasks(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- name: api
  tags: [api]
  tasks:
    - name: deploy-api
      dokku_stub: { key: deploy-api }
- name: worker
  tags: [worker]
  tasks:
    - name: deploy-worker
      dokku_stub: { key: deploy-worker }
`)
	stdout, _, _ := runPlan(t, path, "--tags", "api")
	if !strings.Contains(stdout, "deploy-api") {
		t.Errorf("deploy-api should match play tag; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "deploy-worker") {
		t.Errorf("deploy-worker should be filtered by --tags; got:\n%s", stdout)
	}
}

// TestPlanRegisteredVisibleToFollowUp drives plan against the stub
// task and asserts that a register: + when: combination behaves the
// same way it does in apply: the second task's `when:` evaluates
// against the synthesized TaskOutputState of the first.
func TestPlanRegisteredVisibleToFollowUp(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: false})

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: first
      register: first
      dokku_stub: { key: a }
    - name: follow-up
      when: 'registered.first.Changed'
      dokku_stub: { key: b }
`)
	stdout, _, exit := runPlan(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "follow-up") || strings.Contains(stdout, "[skipped] follow-up") {
		t.Errorf("follow-up should plan because first.Changed is true; got:\n%s", stdout)
	}
}

// TestPlanFailedWhenClearsError pins that failed_when in plan mode
// rewrites the synthesized TaskOutputState's Error so the plan
// classifier no longer treats the task as a probe error.
func TestPlanFailedWhenClearsError(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{
		PlanError: errors.New("App nonexistent does not exist"),
		Stderr:    "App nonexistent does not exist",
	})

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: tolerant
      failed_when: 'result.Error != nil and not (result.Stderr contains "does not exist")'
      dokku_stub: { key: a }
`)
	stdout, _, exit := runPlan(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "[!]") {
		t.Errorf("probe error should be cleared by failed_when; got:\n%s", stdout)
	}
}

// TestPlanProbeErrorRendersMarkerAndExits pins the documented contract
// that an uncleared probe error renders [!] on stderr and makes plan
// exit 1, and that errors win over drift under --detailed-exitcode (exit
// 1, not 2). This is the end-to-end guard for #328: a probe that could
// not run must not be reported as absent with an optimistic [+] create.
func TestPlanProbeErrorRendersMarkerAndExits(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{
		PlanError: errors.New(`exec: "dokku": executable file not found in $PATH`),
	})

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: needs-dokku
      dokku_stub: { key: a }
`)

	stdout, stderr, exit := runPlan(t, path)
	if exit != 1 {
		t.Fatalf("exit = %d, want 1; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stderr, "[!]") {
		t.Errorf("expected [!] marker on stderr; got stdout=%s stderr=%s", stdout, stderr)
	}
	if strings.Contains(stdout, "[+]") {
		t.Errorf("plan must not predict [+] create for state it never read; got:\n%s", stdout)
	}

	// --detailed-exitcode: errors win over drift, so exit stays 1, not 2.
	_, _, exit = runPlan(t, path, "--detailed-exitcode")
	if exit != 1 {
		t.Errorf("detailed-exitcode exit = %d, want 1 (errors win over drift)", exit)
	}
}

// TestPlanChangedWhenTrueRecomputesStatus pins #313: changed_when: 'true'
// on an in-sync task must flip the marker and the JSON status to a
// would-change verdict, not leave the stale [ok] / "status":"ok" the
// probe returned.
func TestPlanChangedWhenTrueRecomputesStatus(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: false}) // Plan() -> InSync, PlanStatusOK

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: forced
      changed_when: 'true'
      dokku_stub: { key: a }
`)

	stdout, _, exit := runPlan(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "[ok]") {
		t.Errorf("changed_when:true must not render [ok]; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[~]") {
		t.Errorf("expected [~] marker for the forced change; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "1 would change") {
		t.Errorf("summary should count 1 would change; got:\n%s", stdout)
	}

	jsonOut, _, exit := runPlan(t, path, "--json")
	if exit != 0 {
		t.Fatalf("json exit = %d, want 0; out=%s", exit, jsonOut)
	}
	sawTask := false
	for _, ev := range decodeLines(t, jsonOut) {
		if ev["type"] != "task" {
			continue
		}
		sawTask = true
		if ev["status"] != "~" {
			t.Errorf("json status = %v, want ~", ev["status"])
		}
		if ev["would_change"] != true {
			t.Errorf("json would_change = %v, want true", ev["would_change"])
		}
	}
	if !sawTask {
		t.Fatalf("no task event in json output:\n%s", jsonOut)
	}
}

// TestPlanFailedWhenClearingErrorRecomputesStatus pins the #313 inverse:
// failed_when clearing a probe error leaves a would-change line marked
// [~], not the stale [!] the probe returned.
func TestPlanFailedWhenClearingErrorRecomputesStatus(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{
		PlanError: errors.New("App nonexistent does not exist"),
		Stderr:    "App nonexistent does not exist",
	})

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: tolerant
      failed_when: 'false'
      dokku_stub: { key: a }
`)

	jsonOut, _, exit := runPlan(t, path, "--json")
	if exit != 0 {
		t.Fatalf("json exit = %d, want 0; out=%s", exit, jsonOut)
	}
	for _, ev := range decodeLines(t, jsonOut) {
		if ev["type"] != "task" {
			continue
		}
		if ev["status"] == "error" || ev["status"] == "!" {
			t.Errorf("failed_when:false should clear the probe error; status = %v", ev["status"])
		}
	}
}

// TestPlanRejectsDuplicateRegisterName pins #314: apply/plan reject a
// register name reused across tasks at load time (the same rule validate
// enforces) instead of silently merging results.
func TestPlanRejectsDuplicateRegisterName(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: false})

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: first
      register: dup
      dokku_stub: { key: a }
    - name: second
      register: dup
      dokku_stub: { key: b }
`)
	_, stderr, exit := runPlan(t, path)
	if exit == 0 {
		t.Fatalf("expected non-zero exit for a reused register name")
	}
	if !strings.Contains(stderr, "already declared") {
		t.Errorf("expected an 'already declared' error; got stderr:\n%s", stderr)
	}
}

// TestPlanSingleIterationLoopRegisterResultsIndex pins #330: a loop that
// resolves to one iteration still exposes .Results[0], so a follow-up
// predicate that reads it plans normally instead of failing with an
// index-out-of-range error.
func TestPlanSingleIterationLoopRegisterResultsIndex(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: false})

	path := writeMultiPlayTasks(t, `---
- tasks:
    - name: deploy
      loop: ["only"]
      register: deploys
      dokku_stub: { key: a }
    - name: follow
      when: 'registered.deploys.Results[0].Changed'
      dokku_stub: { key: b }
`)
	stdout, stderr, exit := runPlan(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s stderr=%s", exit, stdout, stderr)
	}
	if strings.Contains(stdout, "index out of range") || strings.Contains(stderr, "index out of range") {
		t.Errorf("single-iteration loop register should expose Results[0]; got:\n%s\n%s", stdout, stderr)
	}
	if !strings.Contains(stdout, "follow") {
		t.Errorf("follow-up task reading Results[0] should be planned; got:\n%s", stdout)
	}
}

// TestMultiPlayWhenSkippedShowsInSummary: skipped plays count toward
// the new "n play skipped" summary segment.
func TestMultiPlayWhenSkippedShowsInSummary(t *testing.T) {
	path := writeMultiPlayTasks(t, `---
- inputs:
    - name: env
      default: dev
- name: api
  when: 'env == "prod"'
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-api
- name: worker
  tasks:
    - name: noop
      dokku_app:
        app: docket-test-noop-worker
`)
	stdout, _, _ := runPlan(t, path)
	if !strings.Contains(stdout, "1 play skipped") {
		t.Errorf("summary should include `1 play skipped`; got:\n%s", stdout)
	}
}
