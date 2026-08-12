package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dokku/docket/tasks"
	"github.com/mitchellh/cli"
)

// listTasksOptions captures everything renderListTasks needs to walk the
// resolved play set and print one line per envelope. It is constructed by
// the apply and plan commands once filterPlaysByName has narrowed the
// run.
type listTasksOptions struct {
	plays         []*tasks.Play
	includes      []string
	skips         []string
	fileLevelKeys map[string]bool
	userSet       map[string]bool
	context       map[string]interface{}
	jsonOut       bool
}

// renderListTasks walks the resolved task plan and prints one line per
// envelope without executing anything. Honors --tags / --skip-tags via
// FilterByTags and renders block / rescue / always children indented
// under their group's line.
//
// when: predicates are evaluated against the inputs only - registered
// values from prior tasks are not available because no task has run.
// Predicates that reference `.registered.<name>` produce an [unknown]
// marker since their truth value cannot be determined statically. All
// other false predicates produce a [skipped] marker.
//
// A predicate that *errors* is treated differently depending on where it
// sits. A play-level when: is evaluated against exactly the context the
// apply / plan loops build for it, so an error here is the same error
// they would hit: the listing still renders in full, and the exit code
// is 1. A task-level when: sees a reduced context (no result, no
// registered, no failed_task), so an error there may be an artifact of
// listing offline rather than a broken predicate - it renders [when?]
// and leaves the exit code alone. See evaluateListWhen.
func renderListTasks(ui cli.Ui, opts listTasksOptions) int {
	// playWhenError records that at least one play predicate failed to
	// evaluate. The walk is not short-circuited - every remaining play is
	// still listed, and the failure is carried by the exit code once the
	// listing is complete, matching the way the plan loop finishes before
	// returning 1.
	playWhenError := false
	for _, play := range opts.plays {
		if play.IsFileLevel() {
			continue
		}
		if play.HasWhen() {
			playCtx := buildEnvelopeExprContext(buildPlayWhenContext(opts.context, opts.fileLevelKeys, opts.userSet))
			ok, err := tasks.EvalBool(play.WhenProgram(), playCtx)
			if err != nil {
				playWhenError = true
				if opts.jsonOut {
					emitListJSON(ui, map[string]interface{}{
						"type":   "play_skipped",
						"play":   play.Name,
						"when":   play.When,
						"reason": fmt.Sprintf("when error: %v", err),
					})
				} else {
					ui.Output(fmt.Sprintf("==> Play: %s  (when error: %v)", play.Name, err))
				}
				continue
			}
			if !ok {
				if opts.jsonOut {
					emitListJSON(ui, map[string]interface{}{
						"type":   "play_skipped",
						"play":   play.Name,
						"when":   play.When,
						"reason": "when: " + play.When,
					})
				} else {
					ui.Output(fmt.Sprintf("==> Play: %s  (skipped: when %q)", play.Name, play.When))
				}
				continue
			}
		}

		if !opts.jsonOut {
			ui.Output(fmt.Sprintf("==> Play: %s", play.Name))
		}

		playExprCtx := buildEnvelopeExprContext(tasks.BuildPerPlayContext(opts.context, play.Inputs, opts.userSet))

		idx := 0
		for _, name := range tasks.FilterByTags(play.Tasks, opts.includes, opts.skips) {
			renderListEnvelope(ui, play.Name, play.Tasks.GetEnvelope(name), idx, "", 0, playExprCtx, opts.jsonOut)
			idx++
		}
	}
	if playWhenError {
		return 1
	}
	return 0
}

// renderListEnvelope renders one envelope's line and, for a group,
// recursively renders its block / rescue / always children indented one
// level. indent is the leading-space count; phase labels group children
// (matching the executor's phase decoration).
//
// The displayed label is the envelope name as-is. Before #427 an unnamed task
// carried a random `task #N <hex>` name, and this function substituted the
// task type plus the first field named App / Name / Service / ... to keep the
// listing readable - a heuristic that collapsed every phase and option of one
// app's dokku_docker_options onto the same line. The loader now names an
// unnamed task after the resource it addresses, so there is nothing to
// substitute.
func renderListEnvelope(
	ui cli.Ui,
	playName string,
	env *tasks.TaskEnvelope,
	index int,
	phase string,
	indent int,
	playExprCtx map[string]interface{},
	jsonOut bool,
) {
	skipMarker := evaluateListWhen(env, playExprCtx)
	display := env.Name
	deprecation := ""
	var probe tasks.ProbeSupport
	if env != nil && env.Task != nil {
		deprecation = tasks.TaskDeprecation(env.Task)
		probe, _ = tasks.TaskProbeSupport(env.Task)
	}

	if jsonOut {
		ev := map[string]interface{}{
			"type":  "list_task",
			"play":  playName,
			"name":  display,
			"index": index,
		}
		if len(env.Tags) > 0 {
			ev["tags"] = append([]string{}, env.Tags...)
		}
		if env.IsGroup() {
			ev["group"] = true
		}
		if phase != "" {
			ev["phase"] = phase
		}
		if deprecation != "" {
			ev["deprecated"] = true
			ev["deprecation"] = deprecation
		}
		if probe.Status == tasks.ProbeUnsupported || probe.Status == tasks.ProbePartial {
			ev["probe"] = string(probe.Status)
			ev["probe_caveat"] = probe.Caveat
		}
		switch skipMarker {
		case "skipped":
			ev["skipped"] = true
			ev["when"] = env.When
		case "unknown":
			ev["unknown"] = true
			ev["when"] = env.When
		case "when_error":
			ev["when_error"] = true
			ev["when"] = env.When
		}
		if env.IsLoopExpansion {
			ev["loop_index"] = env.LoopIndex
			if env.LoopItem != nil {
				ev["loop_item"] = env.LoopItem
			}
		}
		emitListJSON(ui, ev)
	} else {
		var b strings.Builder
		if indent > 0 {
			b.WriteString(strings.Repeat("  ", indent))
		}
		switch skipMarker {
		case "skipped":
			b.WriteString("[skipped] ")
		case "unknown":
			b.WriteString("[unknown] ")
		case "when_error":
			b.WriteString("[when?]   ")
		default:
			b.WriteString(fmt.Sprintf("[%d] ", index))
		}
		decorated := display
		if phase != "" {
			decorated = fmt.Sprintf("[%s] %s", phase, display)
		}
		b.WriteString(decorated)
		if env.IsGroup() {
			b.WriteString("  (group)")
		}
		if deprecation != "" {
			b.WriteString("  (deprecated)")
		}
		switch probe.Status {
		case tasks.ProbeUnsupported:
			b.WriteString("  (never converges)")
		case tasks.ProbePartial:
			b.WriteString("  (partial probe)")
		}
		if len(env.Tags) > 0 {
			b.WriteString(fmt.Sprintf("  [tags=%s]", strings.Join(env.Tags, ",")))
		}
		ui.Output(b.String())
	}

	if env.IsGroup() {
		for i, child := range env.Block {
			renderListEnvelope(ui, playName, child, i, "block", indent+1, playExprCtx, jsonOut)
		}
		for i, child := range env.Rescue {
			renderListEnvelope(ui, playName, child, i, "rescue", indent+1, playExprCtx, jsonOut)
		}
		for i, child := range env.Always {
			renderListEnvelope(ui, playName, child, i, "always", indent+1, playExprCtx, jsonOut)
		}
	}
}

// evaluateListWhen returns the marker --list-tasks should print for
// env's when: predicate. Returns "" when no predicate is present or it
// evaluates true; "skipped" when it evaluates false; "unknown" when the
// predicate references `.registered.<name>` (we can't decide without
// running prior tasks); "when_error" when the predicate compiles but
// errors at evaluation time against the inputs context.
//
// "when_error" is a marker only - it does not fail the listing the way a
// play-level when: error does. The context here is missing `result` and
// `failed_task`, so a predicate that is valid at run time can error when
// evaluated offline: a rescue child written the documented way,
// `when: 'failed_task.Stderr contains "..."'`, is `nil.Stderr` here.
func evaluateListWhen(env *tasks.TaskEnvelope, playExprCtx map[string]interface{}) string {
	if !env.HasWhen() {
		return ""
	}
	if whenReferencesRegistered(env.When) {
		return "unknown"
	}
	ok, err := tasks.EvalBool(env.WhenProgram(), envelopeExprContext(playExprCtx, env, nil, nil, nil))
	if err != nil {
		return "when_error"
	}
	if !ok {
		return "skipped"
	}
	return ""
}

// whenReferencesRegistered does a substring check against the raw expr
// source for `registered.` references. The check is intentionally
// conservative: any predicate that mentions `registered` is reported as
// [unknown] in --list-tasks rather than evaluated against an empty map
// (which would yield a misleading skip).
func whenReferencesRegistered(src string) bool {
	return strings.Contains(src, "registered")
}

// emitListJSON serialises ev as a single JSON-lines event on the Ui's
// output sink. Errors are routed to Ui.Error so the consumer sees the
// failure without corrupting the stream.
func emitListJSON(ui cli.Ui, ev map[string]interface{}) {
	ev["version"] = jsonSchemaVersion
	b, err := json.Marshal(ev)
	if err != nil {
		ui.Error("json marshal error: " + err.Error())
		return
	}
	ui.Output(string(b))
}
