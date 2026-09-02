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
// the run's masker are masked before any string field that
// could carry them is serialised.
type JSONEmitter struct {
	ui cli.Ui
	// masker holds the run's sensitive values. An emitter renders text at
	// points that have no context to carry one, so it is handed the run's
	// masker at construction instead. A nil masker masks nothing, which is
	// what a caller that registered no secrets wants.
	masker *subprocess.Masker
}

// NewJSONEmitter constructs a JSONEmitter bound to the given Ui.
func NewJSONEmitter(ui cli.Ui, masker *subprocess.Masker) *JSONEmitter {
	return &JSONEmitter{ui: ui, masker: masker}
}

// PlayStart emits a `play_start` event.
func (e *JSONEmitter) PlayStart(name, host string) {
	ev := map[string]interface{}{
		"version": jsonSchemaVersion,
		"type":    "play_start",
		"name":    e.masker.String(name),
		"ts":      nowRFC3339(),
	}
	if host != "" {
		ev["host"] = host
	}
	e.write(ev)
}

// PlaySkipped emits a `play_skipped` event for a play that was filtered
// out by its `when:` predicate. The reason field carries the raw expr
// source so consumers can correlate the skip with the recipe. The name,
// when, and reason are masked against the global sensitive value set: a
// play predicate can sigil-interpolate a sensitive input, so whenSrc (and
// the same string wrapped with an eval error by apply/plan) may contain
// the literal secret.
func (e *JSONEmitter) PlaySkipped(name, whenSrc string) {
	ev := map[string]interface{}{
		"version": jsonSchemaVersion,
		"type":    "play_skipped",
		"name":    e.masker.String(name),
		"ts":      nowRFC3339(),
	}
	if whenSrc != "" {
		masked := e.masker.String(whenSrc)
		ev["when"] = masked
		ev["reason"] = "when: " + masked
	}
	e.write(ev)
}

// ApplyTask emits one `task` event for an apply run. Status is one of
// "ok", "changed", "skipped", "error".
func (e *JSONEmitter) ApplyTask(ev ApplyTaskEvent) {
	out := map[string]interface{}{
		"version":       jsonSchemaVersion,
		"type":          "task",
		"play":          e.masker.String(ev.Play),
		"name":          e.masker.String(ev.Name),
		"changed":       ev.State.Changed,
		"state":         string(ev.State.State),
		"desired_state": string(ev.State.DesiredState),
		"duration_ms":   ev.Duration.Milliseconds(),
		"ts":            timestampOrNow(ev.Timestamp),
	}
	switch {
	case ev.WhenError != nil:
		out["status"] = "error"
		out["error"] = e.masker.String(ev.WhenError.Error())
	case ev.Skipped:
		out["status"] = "skipped"
		if ev.SkipReason != "" {
			out["skip_reason"] = e.masker.String(ev.SkipReason)
		}
	case ev.State.Error != nil:
		out["status"] = "error"
		out["error"] = e.masker.String(PrefixErrorMessage(ev.State.Error))
		if ev.State.Stderr != "" {
			out["stderr"] = e.masker.String(ev.State.Stderr)
		}
		if ev.State.Stdout != "" {
			out["stdout"] = e.masker.String(ev.State.Stdout)
		}
		out["exit_code"] = ev.State.ExitCode
		if ev.Ignored {
			out["ignored"] = true
		}
	case ev.InvalidState:
		out["status"] = "error"
		out["error"] = e.masker.String(invalidStateMessage(ev.State))
		if ev.Ignored {
			out["ignored"] = true
		}
	case ev.State.Changed:
		out["status"] = "changed"
	default:
		out["status"] = "ok"
	}
	if cmds := maskedStrings(e.masker, ev.State.Commands); len(cmds) > 0 {
		out["commands"] = cmds
	}
	if ev.Phase != "" {
		out["phase"] = ev.Phase
	}
	if ev.Group {
		out["group"] = true
	}
	e.write(out)
}

// PlanTask emits one `task` event for a plan run. Status is one of
// "ok", "+", "~", "-", "skipped", "error".
func (e *JSONEmitter) PlanTask(ev PlanTaskEvent) {
	out := map[string]interface{}{
		"version":       jsonSchemaVersion,
		"type":          "task",
		"play":          e.masker.String(ev.Play),
		"name":          e.masker.String(ev.Name),
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
		out["error"] = e.masker.String(ev.WhenError.Error())
	case ev.Skipped:
		out["status"] = "skipped"
		out["would_change"] = false
	case ev.Result.Error != nil:
		out["status"] = "error"
		out["would_change"] = false
		out["error"] = e.masker.String(PrefixErrorMessage(ev.Result.Error))
	case ev.Result.InSync:
		out["status"] = "ok"
	default:
		out["status"] = string(ev.Result.Status)
		if out["status"] == "" {
			out["status"] = string(tasks.PlanStatusModify)
		}
		if ev.Result.Reason != "" {
			out["reason"] = e.masker.String(ev.Result.Reason)
		}
		if len(ev.Result.Mutations) > 0 {
			out["mutations"] = maskedStrings(e.masker, ev.Result.Mutations)
		}
		if cmds := maskedStrings(e.masker, ev.Result.Commands); len(cmds) > 0 {
			out["commands"] = cmds
		}
	}
	if ev.Phase != "" {
		out["phase"] = ev.Phase
	}
	if ev.Group {
		out["group"] = true
	}
	e.write(out)
}

// TaskWarning emits a `warning` event keyed by `reason` and tied to a
// specific task. reason is "deprecated" for a deprecation notice or a
// tasks.WarnReason* value for a probe diagnostic; the event is emitted
// before the task's own `task` event so consumers can correlate by
// ordering.
func (e *JSONEmitter) TaskWarning(play, name, reason, message string) {
	if message == "" {
		return
	}
	e.write(map[string]interface{}{
		"version": jsonSchemaVersion,
		"type":    "warning",
		"play":    e.masker.String(play),
		"name":    e.masker.String(name),
		"reason":  reason,
		"message": e.masker.String(message),
		"ts":      nowRFC3339(),
	})
}

// ApplySummary emits the end-of-run summary event for apply.
func (e *JSONEmitter) ApplySummary(c ApplyCounts, d time.Duration) {
	e.write(map[string]interface{}{
		"version":       jsonSchemaVersion,
		"type":          "summary",
		"tasks":         c.Tasks,
		"changed":       c.Changed,
		"ok":            c.OK,
		"skipped":       c.Skipped,
		"errors":        c.Errors,
		"plays_skipped": c.PlaysSkipped,
		"duration_ms":   d.Milliseconds(),
	})
}

// PlanSummary emits the end-of-run summary event for plan.
func (e *JSONEmitter) PlanSummary(c PlanCounts, d time.Duration) {
	e.write(map[string]interface{}{
		"version":       jsonSchemaVersion,
		"type":          "summary",
		"tasks":         c.Tasks,
		"would_change":  c.WouldChange,
		"in_sync":       c.InSync,
		"skipped":       c.Skipped,
		"errors":        c.Errors,
		"plays_skipped": c.PlaysSkipped,
		"duration_ms":   d.Milliseconds(),
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

// maskedStrings returns a copy of in with each entry passed through
// the run's masker. Returns nil for an empty slice so the caller can
// detect "nothing here" and omit the JSON field. Serves the run stream's
// `commands` and `mutations` arrays and the --list-tasks `tags` array, all of
// which are rendered from the recipe and can carry an interpolated secret.
func maskedStrings(masker *subprocess.Masker, in []string) []string {
	if len(in) == 0 {
		return nil
	}
	out := make([]string, len(in))
	for i, v := range in {
		out[i] = masker.String(v)
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
