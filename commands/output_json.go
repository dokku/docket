package commands

import (
	"encoding/json"
	"time"

	"github.com/dokku/docket/subprocess"
	"github.com/dokku/docket/tasks"
	"github.com/mitchellh/cli"
)

// jsonSchemaVersion is the wire-format version every emitted event carries
// in its `version` field. Bumps are reserved for breaking schema changes;
// additive changes within a major version do not bump.
const jsonSchemaVersion = 1

// JSONEmitter writes one JSON-lines event per call to the underlying Ui's
// stdout (Output) sink. Sensitive values registered via
// subprocess.SetGlobalSensitive are masked before any string field that
// could carry them is serialised.
type JSONEmitter struct {
	ui cli.Ui
}

// NewJSONEmitter constructs a JSONEmitter bound to the given Ui.
func NewJSONEmitter(ui cli.Ui) *JSONEmitter {
	return &JSONEmitter{ui: ui}
}

// PlayStart emits a `play_start` event.
func (e *JSONEmitter) PlayStart(name, host string) {
	ev := map[string]interface{}{
		"version": jsonSchemaVersion,
		"type":    "play_start",
		"name":    name,
		"ts":      nowRFC3339(),
	}
	if host != "" {
		ev["host"] = host
	}
	e.write(ev)
}

// ApplyTask emits one `task` event for an apply run. Status is one of
// "ok", "changed", "skipped", "error".
func (e *JSONEmitter) ApplyTask(ev ApplyTaskEvent) {
	out := map[string]interface{}{
		"version":       jsonSchemaVersion,
		"type":          "task",
		"play":          ev.Play,
		"name":          ev.Name,
		"changed":       ev.State.Changed,
		"state":         string(ev.State.State),
		"desired_state": string(ev.State.DesiredState),
		"duration_ms":   ev.Duration.Milliseconds(),
		"ts":            timestampOrNow(ev.Timestamp),
	}
	switch {
	case ev.WhenError != nil:
		out["status"] = "error"
		out["error"] = subprocess.MaskString(ev.WhenError.Error())
	case ev.Skipped:
		out["status"] = "skipped"
	case ev.State.Error != nil:
		out["status"] = "error"
		out["error"] = subprocess.MaskString(PrefixErrorMessage(ev.State.Error))
	case ev.InvalidState:
		out["status"] = "error"
		out["error"] = subprocess.MaskString(invalidStateMessage(ev.State))
	case ev.State.Changed:
		out["status"] = "changed"
	default:
		out["status"] = "ok"
	}
	if cmds := maskedCommands(ev.State.Commands); len(cmds) > 0 {
		out["commands"] = cmds
	}
	e.write(out)
}

// PlanTask emits one `task` event for a plan run. Status is one of
// "ok", "+", "~", "-", "skipped", "error".
func (e *JSONEmitter) PlanTask(ev PlanTaskEvent) {
	out := map[string]interface{}{
		"version":       jsonSchemaVersion,
		"type":          "task",
		"play":          ev.Play,
		"name":          ev.Name,
		"would_change":  !ev.Result.InSync && ev.Result.Error == nil && !ev.Skipped && ev.WhenError == nil,
		"state":         string(ev.Result.DesiredState),
		"desired_state": string(ev.Result.DesiredState),
		"duration_ms":   ev.Duration.Milliseconds(),
		"ts":            timestampOrNow(ev.Timestamp),
	}
	switch {
	case ev.WhenError != nil:
		out["status"] = "error"
		out["would_change"] = false
		out["error"] = subprocess.MaskString(ev.WhenError.Error())
	case ev.Skipped:
		out["status"] = "skipped"
		out["would_change"] = false
	case ev.Result.Error != nil:
		out["status"] = "error"
		out["would_change"] = false
		out["error"] = subprocess.MaskString(PrefixErrorMessage(ev.Result.Error))
	case ev.Result.InSync:
		out["status"] = "ok"
	default:
		out["status"] = string(ev.Result.Status)
		if out["status"] == "" {
			out["status"] = string(tasks.PlanStatusModify)
		}
		if ev.Result.Reason != "" {
			out["reason"] = subprocess.MaskString(ev.Result.Reason)
		}
		if len(ev.Result.Mutations) > 0 {
			out["mutations"] = maskedCommands(ev.Result.Mutations)
		}
		if cmds := maskedCommands(ev.Result.Commands); len(cmds) > 0 {
			out["commands"] = cmds
		}
	}
	e.write(out)
}

// ApplySummary emits the end-of-run summary event for apply.
func (e *JSONEmitter) ApplySummary(c ApplyCounts, d time.Duration) {
	e.write(map[string]interface{}{
		"version":     jsonSchemaVersion,
		"type":        "summary",
		"tasks":       c.Tasks,
		"changed":     c.Changed,
		"ok":          c.OK,
		"skipped":     c.Skipped,
		"errors":      c.Errors,
		"duration_ms": d.Milliseconds(),
	})
}

// PlanSummary emits the end-of-run summary event for plan.
func (e *JSONEmitter) PlanSummary(c PlanCounts, d time.Duration) {
	e.write(map[string]interface{}{
		"version":      jsonSchemaVersion,
		"type":         "summary",
		"tasks":        c.Tasks,
		"would_change": c.WouldChange,
		"in_sync":      c.InSync,
		"skipped":      c.Skipped,
		"errors":       c.Errors,
		"duration_ms":  d.Milliseconds(),
	})
}

// write serialises ev to a single JSON line on stdout. Errors during marshal
// are surfaced via the Ui's Error sink so the consumer sees the failure
// without corrupting the stream.
func (e *JSONEmitter) write(ev map[string]interface{}) {
	b, err := json.Marshal(ev)
	if err != nil {
		e.ui.Error("json marshal error: " + err.Error())
		return
	}
	e.ui.Output(string(b))
}

// maskedCommands returns a copy of cmds with each entry passed through
// subprocess.MaskString. Returns nil for an empty slice so the caller can
// detect "no commands" and omit the JSON field.
func maskedCommands(cmds []string) []string {
	if len(cmds) == 0 {
		return nil
	}
	out := make([]string, len(cmds))
	for i, c := range cmds {
		out[i] = subprocess.MaskString(c)
	}
	return out
}

// invalidStateMessage formats the state-mismatch error apply.go reports
// inline today. Kept here so the JSON path renders identical wording.
func invalidStateMessage(s tasks.TaskOutputState) string {
	return "invalid state: expected=" + string(s.DesiredState) + " actual=" + string(s.State)
}

// nowRFC3339 returns the current UTC instant formatted RFC3339.
func nowRFC3339() string {
	return time.Now().UTC().Format(time.RFC3339)
}

// timestampOrNow renders ts as RFC3339 UTC; if ts is zero, returns the
// current time. apply.go / plan.go fill in Timestamp at event-build time
// so a single run produces strictly increasing timestamps.
func timestampOrNow(ts time.Time) string {
	if ts.IsZero() {
		return nowRFC3339()
	}
	return ts.UTC().Format(time.RFC3339)
}
