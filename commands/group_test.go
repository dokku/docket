package commands

import (
	"errors"
	"strings"
	"testing"
)

// TestApplyGroupAllChildrenSucceed: a group with all-passing block
// children and no rescue/always reports OK at the group level and
// each child renders its own line.
func TestApplyGroupAllChildrenSucceed(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: false})

	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      block:
        - name: ensure first
          dokku_stub: { key: a }
        - name: ensure second
          dokku_stub: { key: b }
`)
	stdout, stderr, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s; stderr=%s", exit, stdout, stderr)
	}
	if !strings.Contains(stdout, "[block] ensure first") {
		t.Errorf("expected [block] ensure first in stdout; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "[block] ensure second") {
		t.Errorf("expected [block] ensure second in stdout; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "deploy  (group)") {
		t.Errorf("expected group summary line; got:\n%s", stdout)
	}
}

// TestApplyGroupBlockErrorTriggersRescue: the first block child errors,
// rescue runs and clears the failure, group reports OK.
func TestApplyGroupBlockErrorTriggersRescue(t *testing.T) {
	defer stubReset()
	stubSet("bad", StubFixture{ExecuteError: errors.New("block boom")})
	stubSet("rescue", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      block:
        - name: errors
          dokku_stub: { key: bad }
        - name: should-not-run
          dokku_stub: { key: bad }
      rescue:
        - name: cleanup
          dokku_stub: { key: rescue }
`)
	stdout, stderr, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0 (rescue cleared); stdout=%s; stderr=%s", exit, stdout, stderr)
	}
	combined := stdout + stderr
	if !strings.Contains(combined, "[block] errors") {
		t.Errorf("expected [block] errors line; combined=\n%s", combined)
	}
	if !strings.Contains(stdout, "[rescue] cleanup") {
		t.Errorf("expected [rescue] cleanup line; got:\n%s", stdout)
	}
	if strings.Contains(combined, "[block] should-not-run") {
		t.Errorf("post-error block child should not run; combined=\n%s", combined)
	}
}

// TestApplyGroupRescueFailureFailsGroup: when rescue itself errors,
// the group's verdict is failed and the run exits non-zero.
func TestApplyGroupRescueFailureFailsGroup(t *testing.T) {
	defer stubReset()
	stubSet("blockboom", StubFixture{ExecuteError: errors.New("block boom")})
	stubSet("rescueboom", StubFixture{ExecuteError: errors.New("rescue boom")})

	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      block:
        - dokku_stub: { key: blockboom }
      rescue:
        - dokku_stub: { key: rescueboom }
`)
	stdout, _, exit := runApply(t, path)
	if exit == 0 {
		t.Fatalf("expected non-zero exit when rescue errors; stdout=%s", stdout)
	}
}

// TestApplyGroupAlwaysRunsAfterSuccessAndRescue: always children run
// regardless of block success or rescue failure paths.
func TestApplyGroupAlwaysRunsAfterSuccess(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("marker", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: with-always
      block:
        - dokku_stub: { key: a }
      always:
        - name: stamp
          dokku_stub: { key: marker }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[always] stamp") {
		t.Errorf("expected [always] stamp line; got:\n%s", stdout)
	}
}

func TestApplyGroupAlwaysRunsAfterRescue(t *testing.T) {
	defer stubReset()
	stubSet("blockboom", StubFixture{ExecuteError: errors.New("block boom")})
	stubSet("rescue", StubFixture{Changed: true})
	stubSet("marker", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: with-rescue-and-always
      block:
        - dokku_stub: { key: blockboom }
      rescue:
        - dokku_stub: { key: rescue }
      always:
        - name: stamp
          dokku_stub: { key: marker }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[always] stamp") {
		t.Errorf("expected [always] stamp line after rescue; got:\n%s", stdout)
	}
}

// TestApplyGroupIgnoreErrorsOnChildDoesNotTriggerRescue mirrors the
// #210 rule: ignore_errors swallows a child's error and does NOT
// trigger rescue. The next block child still runs; rescue stays cold.
func TestApplyGroupIgnoreErrorsOnChildDoesNotTriggerRescue(t *testing.T) {
	defer stubReset()
	stubSet("bad", StubFixture{ExecuteError: errors.New("ignored boom")})
	stubSet("after", StubFixture{Changed: true})
	stubSet("rescue", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: swallow
      block:
        - name: errors quietly
          ignore_errors: true
          dokku_stub: { key: bad }
        - name: still runs
          dokku_stub: { key: after }
      rescue:
        - name: rescue should not run
          dokku_stub: { key: rescue }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[block] still runs") {
		t.Errorf("post-ignored block child should run; got:\n%s", stdout)
	}
	if strings.Contains(stdout, "[rescue]") {
		t.Errorf("rescue must not trigger when ignore_errors swallowed the error; got:\n%s", stdout)
	}
}

// TestApplyGroupFailedTaskBoundInRescue: a rescue child's `when:`
// references `.failed_task` and uses it to gate behaviour. The rescue
// should run because the predicate sees the failing block child's
// state.
func TestApplyGroupFailedTaskBoundInRescue(t *testing.T) {
	defer stubReset()
	stubSet("bad", StubFixture{ExecuteError: errors.New("triggered boom")})
	stubSet("cleanup", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      block:
        - dokku_stub: { key: bad }
      rescue:
        - name: clean if real
          when: 'failed_task.Error != nil'
          dokku_stub: { key: cleanup }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[rescue] clean if real") {
		t.Errorf("rescue should run since failed_task.Error != nil; got:\n%s", stdout)
	}
}

// TestApplyGroupIgnoreErrorsOnGroup: a residual error after rescue +
// always is suppressed when the group itself carries ignore_errors.
func TestApplyGroupIgnoreErrorsOnGroup(t *testing.T) {
	defer stubReset()
	stubSet("blockboom", StubFixture{ExecuteError: errors.New("block boom")})

	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      ignore_errors: true
      block:
        - dokku_stub: { key: blockboom }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("ignore_errors on group should swallow the residual error; exit=%d stdout=%s", exit, stdout)
	}
}

// TestApplyGroupRegisterSnapshotsAggregateState: register on a group
// captures the synthesized post-override outcome and exposes it to a
// later task's predicate.
func TestApplyGroupRegisterSnapshotsAggregateState(t *testing.T) {
	defer stubReset()
	stubSet("a", StubFixture{Changed: true})
	stubSet("b", StubFixture{Changed: false})

	path := writeTasksFile(t, `---
- tasks:
    - name: deploy
      register: deploy_outcome
      block:
        - dokku_stub: { key: a }
    - name: follow-up
      when: 'registered.deploy_outcome.Changed'
      dokku_stub: { key: b }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("exit = %d, want 0; stdout=%s", exit, stdout)
	}
	if strings.Contains(stdout, "[skipped] follow-up") {
		t.Errorf("follow-up must run because the group reported changed; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, "follow-up") {
		t.Errorf("follow-up event missing; got:\n%s", stdout)
	}
}

// TestApplyGroupNestedFailureReachesOuterRescue: an inner block's
// error bubbles up and triggers the outer block's rescue.
func TestApplyGroupNestedFailureReachesOuterRescue(t *testing.T) {
	defer stubReset()
	stubSet("inner-boom", StubFixture{ExecuteError: errors.New("inner boom")})
	stubSet("outer-rescue", StubFixture{Changed: true})

	path := writeTasksFile(t, `---
- tasks:
    - name: outer
      block:
        - name: inner
          block:
            - dokku_stub: { key: inner-boom }
      rescue:
        - name: outer rescue
          dokku_stub: { key: outer-rescue }
`)
	stdout, _, exit := runApply(t, path)
	if exit != 0 {
		t.Fatalf("outer rescue should clear the failure; exit=%d stdout=%s", exit, stdout)
	}
	if !strings.Contains(stdout, "[rescue] outer rescue") {
		t.Errorf("outer rescue should fire; got:\n%s", stdout)
	}
}
