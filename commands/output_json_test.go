package commands

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"
	"github.com/mitchellh/cli"
)

// emitterTestUI returns a fresh MockUi and the emitter under test.
func emitterTestUI() (*JSONEmitter, *cli.MockUi) {
	ui := cli.NewMockUi()
	return NewJSONEmitter(ui), ui
}

// decodeOnly parses the captured stdout as a single JSON-lines event and
// returns the resulting map. Fails the test on any parse error.
func decodeOnly(t *testing.T, out string) map[string]interface{} {
	t.Helper()
	out = strings.TrimRight(out, "\n")
	var ev map[string]interface{}
	if err := json.Unmarshal([]byte(out), &ev); err != nil {
		t.Fatalf("invalid JSON: %v\nraw: %q", err, out)
	}
	return ev
}

// decodeLines parses every newline-delimited JSON event from out.
func decodeLines(t *testing.T, out string) []map[string]interface{} {
	t.Helper()
	var events []map[string]interface{}
	for _, line := range strings.Split(strings.TrimRight(out, "\n"), "\n") {
		if line == "" {
			continue
		}
		var ev map[string]interface{}
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("invalid JSON line %q: %v", line, err)
		}
		events = append(events, ev)
	}
	return events
}

func TestJSONEmitterPlayStart(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlayStart("tasks", "")
	ev := decodeOnly(t, ui.OutputWriter.String())

	if got, want := ev["version"], float64(1); got != want {
		t.Errorf("version = %v, want %v", got, want)
	}
	if got, want := ev["type"], "play_start"; got != want {
		t.Errorf("type = %v, want %v", got, want)
	}
	if got, want := ev["name"], "tasks"; got != want {
		t.Errorf("name = %v, want %v", got, want)
	}
	if _, ok := ev["host"]; ok {
		t.Errorf("host should be omitted when empty; got %v", ev["host"])
	}
	if _, ok := ev["ts"]; !ok {
		t.Error("ts must be set")
	}
}

func TestJSONEmitterPlayStartIncludesHostWhenSet(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlayStart("tasks", "alice@host:2222")
	ev := decodeOnly(t, ui.OutputWriter.String())
	if got, want := ev["host"], "alice@host:2222"; got != want {
		t.Errorf("host = %v, want %v", got, want)
	}
}

func TestJSONEmitterApplyTaskOK(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{
		Play: "tasks",
		Name: "ensure api",
		State: tasks.TaskOutputState{
			Changed:      false,
			State:        tasks.StatePresent,
			DesiredState: tasks.StatePresent,
		},
		Duration:  42 * time.Millisecond,
		Timestamp: time.Date(2026, 4, 26, 11, 30, 0, 0, time.UTC),
	})
	ev := decodeOnly(t, ui.OutputWriter.String())

	if ev["version"].(float64) != 1 {
		t.Errorf("version mismatch")
	}
	if ev["type"] != "task" {
		t.Errorf("type mismatch")
	}
	if ev["status"] != "ok" {
		t.Errorf("status = %v, want ok", ev["status"])
	}
	if ev["changed"] != false {
		t.Errorf("changed should be false")
	}
	if ev["duration_ms"].(float64) != 42 {
		t.Errorf("duration_ms = %v, want 42", ev["duration_ms"])
	}
	if ev["state"] != string(tasks.StatePresent) {
		t.Errorf("state = %v, want %v", ev["state"], tasks.StatePresent)
	}
	if ev["ts"] != "2026-04-26T11:30:00Z" {
		t.Errorf("ts = %v, want 2026-04-26T11:30:00Z", ev["ts"])
	}
}

func TestJSONEmitterApplyTaskChangedIncludesCommands(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{
		Play: "tasks",
		Name: "ensure api",
		State: tasks.TaskOutputState{
			Changed:      true,
			State:        tasks.StatePresent,
			DesiredState: tasks.StatePresent,
			Commands:     []string{"dokku --quiet apps:create api"},
		},
		Duration: 100 * time.Millisecond,
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "changed" {
		t.Errorf("status = %v, want changed", ev["status"])
	}
	if ev["changed"] != true {
		t.Errorf("changed should be true")
	}
	cmds, ok := ev["commands"].([]interface{})
	if !ok {
		t.Fatalf("commands should be an array, got %T", ev["commands"])
	}
	if len(cmds) != 1 || cmds[0] != "dokku --quiet apps:create api" {
		t.Errorf("commands = %v", cmds)
	}
}

func TestJSONEmitterApplyTaskError(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{
		Play: "tasks",
		Name: "create app",
		State: tasks.TaskOutputState{
			Error:        errors.New("app foo does not exist"),
			DesiredState: tasks.StatePresent,
		},
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "error" {
		t.Errorf("status = %v, want error", ev["status"])
	}
	errStr, _ := ev["error"].(string)
	if !strings.HasPrefix(errStr, "dokku: ") {
		t.Errorf("error should be prefixed `dokku: `, got %q", errStr)
	}
}

func TestJSONEmitterApplyTaskWhenError(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{
		Play:      "tasks",
		Name:      "skip me",
		WhenError: errors.New("boom"),
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "error" {
		t.Errorf("status = %v, want error", ev["status"])
	}
	if ev["error"] != "boom" {
		t.Errorf("error = %v, want boom", ev["error"])
	}
}

func TestJSONEmitterApplyTaskSkipped(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{Play: "tasks", Name: "skipped", Skipped: true})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "skipped" {
		t.Errorf("status = %v, want skipped", ev["status"])
	}
}

func TestJSONEmitterApplyTaskInvalidState(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{
		Play: "tasks",
		Name: "weird",
		State: tasks.TaskOutputState{
			State:        tasks.StateAbsent,
			DesiredState: tasks.StatePresent,
		},
		InvalidState: true,
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "error" {
		t.Errorf("status = %v, want error", ev["status"])
	}
	errStr, _ := ev["error"].(string)
	if !strings.Contains(errStr, "expected=present actual=absent") {
		t.Errorf("error = %q does not mention expected/actual", errStr)
	}
}

func TestJSONEmitterPlanTaskInSync(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlanTask(PlanTaskEvent{
		Play:   "tasks",
		Name:   "stable",
		Result: tasks.PlanResult{InSync: true, Status: tasks.PlanStatusOK, DesiredState: tasks.StatePresent},
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "ok" {
		t.Errorf("status = %v, want ok", ev["status"])
	}
	if ev["would_change"] != false {
		t.Errorf("would_change should be false for in-sync")
	}
}

func TestJSONEmitterPlanTaskWouldChange(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlanTask(PlanTaskEvent{
		Play: "tasks",
		Name: "configure",
		Result: tasks.PlanResult{
			Status:       tasks.PlanStatusModify,
			Reason:       "2 keys to set",
			Mutations:    []string{"set KEY", "set SECRET"},
			Commands:     []string{"dokku --quiet config:set --encoded api KEY=*** SECRET=***"},
			DesiredState: tasks.StatePresent,
		},
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "~" {
		t.Errorf("status = %v, want ~", ev["status"])
	}
	if ev["would_change"] != true {
		t.Errorf("would_change should be true for drift")
	}
	if ev["reason"] != "2 keys to set" {
		t.Errorf("reason = %v", ev["reason"])
	}
	muts, ok := ev["mutations"].([]interface{})
	if !ok || len(muts) != 2 {
		t.Errorf("mutations = %v", ev["mutations"])
	}
	cmds, ok := ev["commands"].([]interface{})
	if !ok || len(cmds) != 1 {
		t.Errorf("commands = %v", ev["commands"])
	}
}

func TestJSONEmitterPlanTaskProbeError(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlanTask(PlanTaskEvent{
		Play:   "tasks",
		Name:   "broken",
		Result: tasks.PlanResult{Status: tasks.PlanStatusError, Error: errors.New("missing app")},
	})
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["status"] != "error" {
		t.Errorf("status = %v, want error", ev["status"])
	}
	if ev["would_change"] != false {
		t.Errorf("would_change should be false for probe error")
	}
}

func TestJSONEmitterApplySummary(t *testing.T) {
	e, ui := emitterTestUI()
	e.ApplySummary(ApplyCounts{Tasks: 3, Changed: 1, OK: 2, Skipped: 0, Errors: 0}, 1234*time.Millisecond)
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["type"] != "summary" {
		t.Errorf("type = %v", ev["type"])
	}
	if ev["tasks"].(float64) != 3 {
		t.Errorf("tasks count")
	}
	if ev["changed"].(float64) != 1 {
		t.Errorf("changed count")
	}
	if ev["duration_ms"].(float64) != 1234 {
		t.Errorf("duration_ms = %v, want 1234", ev["duration_ms"])
	}
}

func TestJSONEmitterPlanSummary(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlanSummary(PlanCounts{Tasks: 3, WouldChange: 2, InSync: 1, Skipped: 0, Errors: 0}, 500*time.Millisecond)
	ev := decodeOnly(t, ui.OutputWriter.String())
	if ev["type"] != "summary" {
		t.Errorf("type = %v", ev["type"])
	}
	if ev["would_change"].(float64) != 2 {
		t.Errorf("would_change count")
	}
	if ev["in_sync"].(float64) != 1 {
		t.Errorf("in_sync count")
	}
	if ev["duration_ms"].(float64) != 500 {
		t.Errorf("duration_ms = %v, want 500", ev["duration_ms"])
	}
}

func TestJSONEmitterMasksSensitiveValues(t *testing.T) {
	subprocess.SetGlobalSensitive([]string{"topsecret"})
	t.Cleanup(func() { subprocess.SetGlobalSensitive(nil) })

	e, ui := emitterTestUI()
	e.ApplyTask(ApplyTaskEvent{
		Play: "tasks",
		Name: "set config",
		State: tasks.TaskOutputState{
			Changed:      true,
			State:        tasks.StatePresent,
			DesiredState: tasks.StatePresent,
			Commands:     []string{"dokku config:set api KEY=topsecret"},
		},
	})
	out := ui.OutputWriter.String()
	if strings.Contains(out, "topsecret") {
		t.Errorf("sensitive value leaked into JSON output: %s", out)
	}
	if !strings.Contains(out, "***") {
		t.Errorf("expected mask placeholder in JSON output, got %s", out)
	}
}

func TestJSONEmitterEveryEventHasVersion1(t *testing.T) {
	e, ui := emitterTestUI()
	e.PlayStart("tasks", "")
	e.ApplyTask(ApplyTaskEvent{Play: "tasks", Name: "x", State: tasks.TaskOutputState{Changed: true, State: tasks.StatePresent, DesiredState: tasks.StatePresent}})
	e.PlanTask(PlanTaskEvent{Play: "tasks", Name: "y", Result: tasks.PlanResult{InSync: true, Status: tasks.PlanStatusOK}})
	e.ApplySummary(ApplyCounts{}, 0)
	e.PlanSummary(PlanCounts{}, 0)

	for _, ev := range decodeLines(t, ui.OutputWriter.String()) {
		if ev["version"].(float64) != 1 {
			t.Errorf("event missing version=1: %v", ev)
		}
	}
}

// TestEmitterInterfaceSatisfied compiles iff Formatter and JSONEmitter both
// satisfy the EventEmitter contract. Catches signature drift at build time.
func TestEmitterInterfaceSatisfied(t *testing.T) {
	var _ EventEmitter = (*Formatter)(nil)
	var _ EventEmitter = (*JSONEmitter)(nil)
}
