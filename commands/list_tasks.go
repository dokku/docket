package commands

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/dokku/docket/subprocess"
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
	// masker holds the run's sensitive values; the rendered plan echoes task
	// and play names, which are exactly where a secret reaches output.
	masker *subprocess.Masker
	// target is the run-wide target the plays resolve against, so the
	// rendered plan can say which server each play would talk to. The JSON
	// stream is deliberately left alone: it has no play_start event to hang a
	// host on, and its schema forbids extra properties.
	target subprocess.Target
}

// renderListTasks walks the resolved task plan and prints one line per
// envelope without executing anything. Honors --tags / --skip-tags via
// FilterByTags and renders block / rescue / always children indented
// under their group's line.
//
// when: predicates are evaluated against the inputs only - values a
// prior task registers, and the failing block child a rescue branches
// on, are not available because no task has run. Predicates that
// reference those produce an [unknown] marker since their truth value
// cannot be determined statically. All other false predicates produce a
// [skipped] marker.
//
// A predicate that *errors* fails the listing with exit code 1, whether
// it sits on a play or on a task: with the undecidable references sorted
// into [unknown] first, what is left is a predicate that errors the same
// way at run time. A task carries the failure as its [when?] marker; a
// play carries it in its header, and its tasks are left unlisted.
//
// The one exception is reachability. A task under a group that itself
// rendered [skipped], [unknown] or [when?] is never evaluated by a run,
// so its own [when?] prints but leaves the exit code alone - the same
// reason a skipped play's tasks are not walked at all.
//
// Every string this walk emits is masked against the global sensitive value
// set (#455). The listing renders resolved values, so a `sensitive: true`
// input interpolated into a play name, a task name, a `when:` predicate, a
// tag, or a loop item would otherwise come back verbatim - and since #427 an
// unnamed task's name embeds its identity field values, which widened what
// can reach it. Callers register the task-declared values before this runs
// (commands/apply.go, commands/plan.go).
func renderListTasks(ui cli.Ui, opts listTasksOptions) int {
	// whenError records that at least one predicate failed to evaluate.
	// The walk is not short-circuited - every remaining play and task is
	// still listed, and the failure is carried by the exit code once the
	// listing is complete, matching the way the plan loop finishes before
	// returning 1.
	whenError := false
	for _, play := range opts.plays {
		if play.IsFileLevel() {
			continue
		}
		// Each recipe-derived value is masked exactly once, here, and both
		// the human and the --json branch read only the masked form. Masking
		// per value rather than per output site is what keeps the two paths
		// from drifting apart, and it leaves docket's own decorations - the
		// `==> Play: ` prefix, the markers, `(group)` - untouched.
		playName := opts.masker.String(play.Name)
		if play.HasWhen() {
			whenSrc := opts.masker.String(play.When)
			playCtx := buildEnvelopeExprContext(buildPlayWhenContext(opts.context, opts.fileLevelKeys, opts.userSet))
			ok, err := tasks.EvalBool(play.WhenProgram(), playCtx)
			if err != nil {
				whenError = true
				// An expr runtime error quotes the predicate's own source
				// back in its snippet, so the formatted error - not just the
				// play name - carries whatever the predicate interpolated.
				reason := opts.masker.String(fmt.Sprintf("when error: %v", err))
				if opts.jsonOut {
					emitListJSON(ui, map[string]interface{}{
						"type":   "play_skipped",
						"play":   playName,
						"when":   whenSrc,
						"reason": reason,
					})
				} else {
					ui.Output(fmt.Sprintf("==> Play: %s  (%s)", playName, reason))
				}
				continue
			}
			if !ok {
				if opts.jsonOut {
					emitListJSON(ui, map[string]interface{}{
						"type":   "play_skipped",
						"play":   playName,
						"when":   whenSrc,
						"reason": "when: " + whenSrc,
					})
				} else {
					ui.Output(fmt.Sprintf("==> Play: %s  (skipped: when %q)", playName, whenSrc))
				}
				continue
			}
		}

		if !opts.jsonOut {
			header := fmt.Sprintf("==> Play: %s", playName)
			if host := play.ResolveTarget(opts.target).Host; host != "" {
				header += fmt.Sprintf("  (host: %s)", host)
			}
			ui.Output(header)
		}

		rc := listRenderContext{
			masker:      opts.masker,
			ui:          ui,
			playName:    playName,
			playExprCtx: buildEnvelopeExprContext(tasks.BuildPerPlayContext(opts.context, play.Inputs, opts.userSet)),
			jsonOut:     opts.jsonOut,
		}

		idx := 0
		for _, name := range tasks.FilterByTags(play.Tasks, opts.includes, opts.skips) {
			if renderListEnvelope(rc, play.Tasks.GetEnvelope(name), idx, "", 0, true) {
				whenError = true
			}
			idx++
		}
	}
	if whenError {
		return 1
	}
	return 0
}

// listRenderContext carries the values renderListEnvelope needs that do
// not change as it recurses into a group's children: the output sink,
// the play the walk is inside, the expr context every predicate in that
// play evaluates against, and the output mode.
type listRenderContext struct {
	ui          cli.Ui
	playName    string
	playExprCtx map[string]interface{}
	jsonOut     bool
	masker      *subprocess.Masker
}

// renderListEnvelope renders one envelope's line and, for a group,
// recursively renders its block / rescue / always children indented one
// level. indent is the leading-space count; phase labels group children
// (matching the executor's phase decoration).
//
// reachable reports whether a run would evaluate this envelope's when:
// at all - false once an ancestor group's own predicate was skipped or
// could not be resolved. Returns whether a [when?] was rendered at a
// reachable position anywhere in this subtree, which is what fails the
// listing.
//
// The displayed label is the envelope name, masked. Before #427 an unnamed task
// carried a random `task #N <hex>` name, and this function substituted the
// task type plus the first field named App / Name / Service / ... to keep the
// listing readable - a heuristic that collapsed every phase and option of one
// app's dokku_docker_options onto the same line. The loader now names an
// unnamed task after the resource it addresses, so there is nothing to
// substitute.
//
// Masking is display-only: env.Name keeps its real value, so --start-at-task
// matching, the duplicate-name guard, and the loop collision suffix all still
// work on the unmasked names even when two lines render identically.
func renderListEnvelope(
	rc listRenderContext,
	env *tasks.TaskEnvelope,
	index int,
	phase string,
	indent int,
	reachable bool,
) bool {
	skipMarker := evaluateListWhen(env, rc.playExprCtx, phase)
	// Same rule as the play loop: every recipe-derived value is masked once
	// here and both branches below read only the masked form. `probe` and
	// `phase` are deliberately absent - they are docket's own vocabulary,
	// pinned as enums in docs/schemas/list-tasks-v1.schema.json, and masking
	// one would emit a stream that fails its own schema.
	display := rc.masker.String(env.Name)
	whenSrc := rc.masker.String(env.When)
	tags := maskedStrings(rc.masker, env.Tags)
	deprecation := ""
	caveat := ""
	var probe tasks.ProbeSupport
	if env != nil && env.Task != nil {
		deprecation = rc.masker.String(tasks.TaskDeprecation(env.Task))
		probe, _ = tasks.TaskProbeSupport(env.Task)
		caveat = rc.masker.String(probe.Caveat)
	}

	if rc.jsonOut {
		ev := map[string]interface{}{
			"type":  "list_task",
			"play":  rc.playName,
			"name":  display,
			"index": index,
		}
		if len(tags) > 0 {
			ev["tags"] = tags
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
			ev["probe_caveat"] = caveat
		}
		switch skipMarker {
		case "skipped":
			ev["skipped"] = true
			ev["when"] = whenSrc
		case "unknown":
			ev["unknown"] = true
			ev["when"] = whenSrc
		case "when_error":
			ev["when_error"] = true
			ev["when"] = whenSrc
		}
		if env.IsLoopExpansion {
			ev["loop_index"] = env.LoopIndex
			if env.LoopItem != nil {
				ev["loop_item"] = rc.masker.Value(env.LoopItem)
			}
		}
		emitListJSON(rc.ui, ev)
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
		if len(tags) > 0 {
			b.WriteString(fmt.Sprintf("  [tags=%s]", strings.Join(tags, ",")))
		}
		rc.ui.Output(b.String())
	}

	whenError := reachable && skipMarker == "when_error"

	if env.IsGroup() {
		// A group whose own predicate did not resolve true stands between
		// its children and any run, so their predicates are listed but no
		// longer count toward the exit code.
		childReachable := reachable && skipMarker == ""
		for _, phased := range []struct {
			name     string
			children []*tasks.TaskEnvelope
		}{
			{"block", env.Block},
			{"rescue", env.Rescue},
			{"always", env.Always},
		} {
			for i, child := range phased.children {
				if renderListEnvelope(rc, child, i, phased.name, indent+1, childReachable) {
					whenError = true
				}
			}
		}
	}
	return whenError
}

// evaluateListWhen returns the marker --list-tasks should print for
// env's when: predicate, where phase is the envelope's position in its
// parent group ("block", "rescue", "always", or "" at the top level).
// Returns "" when no predicate is present or it evaluates true;
// "skipped" when it evaluates false; "unknown" when the predicate
// references a value only a run can supply; "when_error" when the
// predicate compiles but errors at evaluation time against the inputs
// context.
//
// Once the undecidable references are sorted into "unknown", a
// "when_error" is a predicate that errors at run time too, so it fails
// the listing - see renderListTasks for the reachability caveat.
func evaluateListWhen(env *tasks.TaskEnvelope, playExprCtx map[string]interface{}, phase string) string {
	if !env.HasWhen() {
		return ""
	}
	if whenReferencesRuntimeValue(env.When, runtimeWhenVars(phase)) {
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

// registeredOnlyWhenVars and rescueRuntimeWhenVars are the expr
// identifiers --list-tasks cannot supply but a run can. `registered` is
// bound wherever a prior task has registered a value. `failed_task` is
// bound only for a rescue child (commands/apply.go, commands/plan.go):
// a group nested under rescue: does not pass it down to its own block:
// children, so anywhere else it is nil at run time too and a predicate
// that dereferences it is genuinely broken.
var (
	registeredOnlyWhenVars = map[string]bool{"registered": true}
	rescueRuntimeWhenVars  = map[string]bool{"registered": true, "failed_task": true}
)

// runtimeWhenVars returns the run-time-only identifiers in scope for an
// envelope sitting in the given group phase.
func runtimeWhenVars(phase string) map[string]bool {
	if phase == "rescue" {
		return rescueRuntimeWhenVars
	}
	return registeredOnlyWhenVars
}

// whenReferencesRuntimeValue reports whether src references any of the
// named run-time-only values, which makes its truth value undecidable
// offline. Such a predicate is reported as [unknown] rather than
// evaluated against a context that does not bind it - evaluating would
// yield either a misleading skip (`len(registered) > 0`) or a
// misleading error (`failed_task.Stderr contains "..."`).
//
// The check runs against the parsed identifiers so an input named
// `registered_at`, a string literal "registered", or a field access
// `x.failed_task` is not mistaken for a reference. A predicate that
// reaches here has already compiled at load, so the parse failure branch
// is not reachable through the CLI; it falls back to a substring match,
// which is never less conservative than matching identifiers.
func whenReferencesRuntimeValue(src string, runtime map[string]bool) bool {
	idents, err := tasks.PredicateIdentifiers(src)
	if err != nil {
		for name := range runtime {
			if strings.Contains(src, name) {
				return true
			}
		}
		return false
	}
	for name := range runtime {
		if idents[name] {
			return true
		}
	}
	return false
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
