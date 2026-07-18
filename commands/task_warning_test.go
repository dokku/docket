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
	defer stubReset()
	stubSet("a", stubWithWarning())
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
	defer stubReset()
	stubSet("a", stubWithWarning())
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
