package commands

import (
	"strings"
	"testing"

	"github.com/dokku/docket/tasks"
)

// These tests pin #353: a probe diagnostic a task's Plan() surfaces on
// PlanResult.Warnings (carried onto TaskOutputState for apply) is drained by
// the run loops through EventEmitter.TaskWarning, so it renders as a `[warning]`
// line (human) / `warning` event (JSON) correlated with its task rather than a
// raw log line. Masking is covered by the emitter-level tests; here we pin the
// routing, the reason, and the "warning precedes task" ordering.

const probeWarningMessage = "dokku registry:report rejected probe for property \"password\""

func stubWithWarning() StubFixture {
	return StubFixture{
		Changed: true,
		Warnings: []tasks.PlanWarning{
			{Reason: tasks.WarnReasonProbeRejected, Message: probeWarningMessage},
		},
	}
}

const warningRecipe = `---
- tasks:
    - name: set token
      dokku_stub: { key: a }
`

func TestPlanSurfacesProbeWarning(t *testing.T) {
	stubSet(t, "a", stubWithWarning())
	path := writeTasksFile(t, warningRecipe)

	stdout, _, _ := runPlan(t, path)
	if !strings.Contains(stdout, "[warning]") {
		t.Errorf("expected [warning] line in plan output; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, probeWarningMessage) {
		t.Errorf("expected warning message in plan output; got:\n%s", stdout)
	}
	// The warning line must precede the task's own result line.
	if wi, ti := strings.Index(stdout, "[warning]"), strings.Index(stdout, "[~]"); wi < 0 || ti < 0 || wi > ti {
		t.Errorf("warning should precede the task line; got:\n%s", stdout)
	}

	jsonOut, _, _ := runPlan(t, path, "--json")
	assertWarningEventPrecedesTask(t, jsonOut)
}

func TestApplySurfacesProbeWarning(t *testing.T) {
	stubSet(t, "a", stubWithWarning())
	path := writeTasksFile(t, warningRecipe)

	stdout, _, _ := runApply(t, path)
	if !strings.Contains(stdout, "[warning]") {
		t.Errorf("expected [warning] line in apply output; got:\n%s", stdout)
	}
	if !strings.Contains(stdout, probeWarningMessage) {
		t.Errorf("expected warning message in apply output; got:\n%s", stdout)
	}

	jsonOut, _, _ := runApply(t, path, "--json")
	assertWarningEventPrecedesTask(t, jsonOut)
}

// assertWarningEventPrecedesTask checks the JSON stream carries a `warning`
// event with the probe reason and message, emitted before its `task` event.
func assertWarningEventPrecedesTask(t *testing.T, jsonOut string) {
	t.Helper()
	warnIdx, taskIdx := -1, -1
	for i, ev := range decodeLines(t, jsonOut) {
		switch ev["type"] {
		case "warning":
			warnIdx = i
			if ev["reason"] != tasks.WarnReasonProbeRejected {
				t.Errorf("warning reason = %v, want %v", ev["reason"], tasks.WarnReasonProbeRejected)
			}
			if msg, _ := ev["message"].(string); !strings.Contains(msg, probeWarningMessage) {
				t.Errorf("warning message = %q, want to contain %q", msg, probeWarningMessage)
			}
		case "task":
			if taskIdx == -1 {
				taskIdx = i
			}
		}
	}
	if warnIdx == -1 {
		t.Fatalf("no warning event in JSON stream:\n%s", jsonOut)
	}
	if taskIdx == -1 || warnIdx > taskIdx {
		t.Errorf("warning event should precede task event; warnIdx=%d taskIdx=%d\n%s", warnIdx, taskIdx, jsonOut)
	}
}

// A task can read its state, find a difference, and decline to reconcile it:
// dokku_service_create warns when a service is running an image other than the
// one the recipe pins, because dokku's only remedy recreates the container.
// That is the first warning in the codebase to ride on an in-sync task, so the
// run loops have to drain it from a plan that reports no drift and from an
// apply that changes nothing.
const imageDriftWarningMessage = "redis service cache is running redis:7.2.4, recipe pins redis:7.2.5"

func stubWithInSyncWarning() StubFixture {
	return StubFixture{
		Changed: false,
		Warnings: []tasks.PlanWarning{
			{Reason: tasks.WarnReasonServiceImageDrift, Message: imageDriftWarningMessage},
		},
	}
}

func TestPlanSurfacesWarningOnInSyncTask(t *testing.T) {
	stubSet(t, "a", stubWithInSyncWarning())
	path := writeTasksFile(t, warningRecipe)

	stdout, _, _ := runPlan(t, path)
	if !strings.Contains(stdout, "[warning]") || !strings.Contains(stdout, imageDriftWarningMessage) {
		t.Errorf("expected the drift warning in plan output; got:\n%s", stdout)
	}
	// The task itself is in sync: the warning reports, it does not drift.
	if wi, ti := strings.Index(stdout, "[warning]"), strings.Index(stdout, "[ok]"); wi < 0 || ti < 0 || wi > ti {
		t.Errorf("warning should precede an [ok] task line; got:\n%s", stdout)
	}

	jsonOut, _, _ := runPlan(t, path, "--json")
	assertImageDriftWarningPrecedesOKTask(t, jsonOut)
}

func TestApplySurfacesWarningOnInSyncTask(t *testing.T) {
	stubSet(t, "a", stubWithInSyncWarning())
	path := writeTasksFile(t, warningRecipe)

	stdout, _, _ := runApply(t, path)
	if !strings.Contains(stdout, "[warning]") || !strings.Contains(stdout, imageDriftWarningMessage) {
		t.Errorf("expected the drift warning in apply output; got:\n%s", stdout)
	}
	if wi, ti := strings.Index(stdout, "[warning]"), strings.Index(stdout, "[ok]"); wi < 0 || ti < 0 || wi > ti {
		t.Errorf("warning should precede an [ok] task line; got:\n%s", stdout)
	}

	jsonOut, _, _ := runApply(t, path, "--json")
	assertImageDriftWarningPrecedesOKTask(t, jsonOut)
}

// assertImageDriftWarningPrecedesOKTask mirrors assertWarningEventPrecedesTask
// for the in-sync case. decodeLines validates every event against
// events-v1.schema.json, so this also fails if the new reason was not added to
// the schema's closed `reason` enum.
func assertImageDriftWarningPrecedesOKTask(t *testing.T, jsonOut string) {
	t.Helper()
	warnIdx, taskIdx := -1, -1
	for i, ev := range decodeLines(t, jsonOut) {
		switch ev["type"] {
		case "warning":
			warnIdx = i
			if ev["reason"] != tasks.WarnReasonServiceImageDrift {
				t.Errorf("warning reason = %v, want %v", ev["reason"], tasks.WarnReasonServiceImageDrift)
			}
			if msg, _ := ev["message"].(string); !strings.Contains(msg, imageDriftWarningMessage) {
				t.Errorf("warning message = %q, want to contain %q", msg, imageDriftWarningMessage)
			}
		case "task":
			if taskIdx == -1 {
				taskIdx = i
				if ev["status"] != "ok" {
					t.Errorf("task status = %v, want ok - the warning must not make the task drift", ev["status"])
				}
			}
		}
	}
	if warnIdx == -1 {
		t.Fatalf("no warning event in JSON stream:\n%s", jsonOut)
	}
	if taskIdx == -1 || warnIdx > taskIdx {
		t.Errorf("warning event should precede task event; warnIdx=%d taskIdx=%d\n%s", warnIdx, taskIdx, jsonOut)
	}
}
