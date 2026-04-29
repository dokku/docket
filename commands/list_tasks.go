package commands

import (
	"encoding/json"
	"fmt"
	"reflect"
	"regexp"
	"strings"

	"github.com/dokku/docket/tasks"
	"github.com/mitchellh/cli"
)

// autoNameRE matches the auto-generated task name format from
// tasks/main.go:generateTaskName ("task #<index> <8-byte-hex>"). When a
// listing line carries an auto-name, the display falls back to the
// task's TypeName plus an identifying body field so the user sees
// something meaningful instead of an opaque random suffix.
var autoNameRE = regexp.MustCompile(`^task #\d+ [0-9A-F]{16}$`)

// listingDisplayName returns the human-friendly label for env in
// --list-tasks output. User-supplied names pass through untouched. An
// auto-generated name (the loader's `task #N <hex>` form) is replaced
// with `<TypeName>` plus the first identifying body field (App, Name,
// Service, Repository, ...) when one is present, so a recipe with
// unnamed tasks still reads back the body the user wrote.
func listingDisplayName(env *tasks.TaskEnvelope, fallback string) string {
	if env == nil {
		return fallback
	}
	name := env.Name
	if name == "" {
		name = fallback
	}
	if !autoNameRE.MatchString(name) {
		return name
	}
	if env.TypeName == "" {
		return name
	}
	if id := identifyingBodyField(env.Task); id != "" {
		return fmt.Sprintf("%s: %s", env.TypeName, id)
	}
	return env.TypeName
}

// identifyingBodyField walks the task struct (via reflection) for the
// first non-empty string field whose name matches a common identifier
// (App, Name, Service, Repository, Mount, Url). Returns "" if no such
// field is present, in which case the caller falls back to TypeName
// alone.
func identifyingBodyField(task tasks.Task) string {
	if task == nil {
		return ""
	}
	v := reflect.ValueOf(task)
	for v.Kind() == reflect.Ptr {
		if v.IsNil() {
			return ""
		}
		v = v.Elem()
	}
	if v.Kind() != reflect.Struct {
		return ""
	}
	for _, candidate := range []string{"App", "Name", "Service", "Repository", "Mount", "Url"} {
		f := v.FieldByName(candidate)
		if !f.IsValid() {
			continue
		}
		if f.Kind() != reflect.String {
			continue
		}
		if s := f.String(); s != "" {
			return s
		}
	}
	return ""
}

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
func renderListTasks(ui cli.Ui, opts listTasksOptions) int {
	for _, play := range opts.plays {
		if play.IsFileLevel() {
			continue
		}
		playWhenSkipped := false
		if play.HasWhen() {
			playCtx := buildEnvelopeExprContext(buildPlayWhenContext(opts.context, opts.fileLevelKeys, opts.userSet))
			ok, err := tasks.EvalBool(play.WhenProgram(), playCtx)
			if err != nil {
				if opts.jsonOut {
					emitListJSON(ui, map[string]interface{}{
						"type":      "play_skipped",
						"play":      play.Name,
						"when":      play.When,
						"reason":    fmt.Sprintf("when error: %v", err),
					})
				} else {
					ui.Output(fmt.Sprintf("==> Play: %s  (when error: %v)", play.Name, err))
				}
				continue
			}
			if !ok {
				playWhenSkipped = true
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
		_ = playWhenSkipped

		if !opts.jsonOut {
			ui.Output(fmt.Sprintf("==> Play: %s", play.Name))
		}

		playExprCtx := buildEnvelopeExprContext(tasks.BuildPerPlayContext(opts.context, play.Inputs, opts.userSet))

		idx := 0
		for _, name := range tasks.FilterByTags(play.Tasks, opts.includes, opts.skips) {
			env := play.Tasks.GetEnvelope(name)
			renderListEnvelope(ui, play.Name, name, env, idx, "", 0, playExprCtx, opts.jsonOut)
			idx++
		}
	}
	return 0
}

// renderListEnvelope renders one envelope's line and, for a group,
// recursively renders its block / rescue / always children indented one
// level. indent is the leading-space count; phase labels group children
// (matching the executor's phase decoration).
func renderListEnvelope(
	ui cli.Ui,
	playName, name string,
	env *tasks.TaskEnvelope,
	index int,
	phase string,
	indent int,
	playExprCtx map[string]interface{},
	jsonOut bool,
) {
	skipMarker := evaluateListWhen(env, playExprCtx)
	display := listingDisplayName(env, name)

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
		if len(env.Tags) > 0 {
			b.WriteString(fmt.Sprintf("  [tags=%s]", strings.Join(env.Tags, ",")))
		}
		ui.Output(b.String())
	}

	if env.IsGroup() {
		for i, child := range env.Block {
			childName := child.Name
			if childName == "" {
				childName = fmt.Sprintf("%s.block[%d]", name, i)
			}
			renderListEnvelope(ui, playName, childName, child, i, "block", indent+1, playExprCtx, jsonOut)
		}
		for i, child := range env.Rescue {
			childName := child.Name
			if childName == "" {
				childName = fmt.Sprintf("%s.rescue[%d]", name, i)
			}
			renderListEnvelope(ui, playName, childName, child, i, "rescue", indent+1, playExprCtx, jsonOut)
		}
		for i, child := range env.Always {
			childName := child.Name
			if childName == "" {
				childName = fmt.Sprintf("%s.always[%d]", name, i)
			}
			renderListEnvelope(ui, playName, childName, child, i, "always", indent+1, playExprCtx, jsonOut)
		}
	}
}

// evaluateListWhen returns the marker --list-tasks should print for
// env's when: predicate. Returns "" when no predicate is present or it
// evaluates true; "skipped" when it evaluates false; "unknown" when the
// predicate references `.registered.<name>` (we can't decide without
// running prior tasks); "when_error" when the predicate compiles but
// errors at evaluation time against the inputs context.
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
