package tasks

import (
	"fmt"
	"strings"
)

// The catalog publishes the prose a task's reference page prints, not only the
// raw declarations it is built from (#651). An embedder that documents tasks
// in another tool, such as a Terraform provider's registry docs, would
// otherwise re-render the same sentences from the raw pieces and drift from
// docket's wording. generate/docs.go renders docs/tasks from these same
// published notes, so the page and the catalog cannot disagree.
//
// Each kind of note is rendered in one place: a task note by one entry in
// taskNoteKinds, a field note by one entry in fieldNoteKinds. A renderer takes
// the state a consumer drives, or "" for every state. The "" rendering is what
// docs/tasks prints; a note whose text changes with the state also publishes
// one rendering per value of the task's `state` field, so a consumer that
// drives one state, and may hide the field, can pick the matching text.
// Adding a note kind is one renderer appended to the list in page order.

// NoteKind names one note on a task's reference page.
type NoteKind string

// Kinds of task note, in the order a reference page prints them.
//
//sdk:export
const (
	// NoteDeprecation is the Deprecated admonition.
	NoteDeprecation NoteKind = "deprecation"

	// NoteImperative says the task runs an operation and has nothing to undo.
	NoteImperative NoteKind = "imperative"

	// NoteRequirements lists the Dokku floor and the plugins the task needs.
	NoteRequirements NoteKind = "requirements"

	// NoteRunnerRequirements lists the files docket reads on the machine it
	// runs on.
	NoteRunnerRequirements NoteKind = "runner_requirements"

	// NoteExport says whether `docket export` can reconstruct the task.
	NoteExport NoteKind = "export"

	// NoteProbe says how much of its own state the task can read back.
	NoteProbe NoteKind = "probe"

	// NoteLifecycle names the states that apply and tear down the task.
	NoteLifecycle NoteKind = "lifecycle"

	// NoteIdentity names the fields that select the resource.
	NoteIdentity NoteKind = "identity"

	// NoteObjects names the app and service the task changes.
	NoteObjects NoteKind = "objects"

	// NoteConstraints lists the rules over several fields.
	NoteConstraints NoteKind = "constraints"

	// NoteProperties is the table of names a property task accepts.
	NoteProperties NoteKind = "properties"
)

// NoteSchema is one rendered note on a task's reference page.
type NoteSchema struct {
	// Kind is what the note covers.
	Kind NoteKind `json:"kind"`

	// Markdown is the note as the reference page prints it, heading included
	// and with no trailing newline. It covers every state. A link in it is
	// relative to docs/tasks/.
	Markdown string `json:"markdown"`

	// States renders the note for each value of the task's `state` field.
	// Present only when the text depends on the state. An empty value means
	// the note does not apply in that state.
	States map[string]string `json:"states,omitempty"`
}

// FieldNoteSchema is the text a reference page appends to a field's
// description in the Parameters table.
type FieldNoteSchema struct {
	// Suffix is appended to the field's Description as is, leading space or
	// period included, so Description + Suffix is the Parameters cell. It
	// covers every state.
	Suffix string `json:"suffix"`

	// States renders the suffix for each value of the task's `state` field.
	// Present only when the text depends on the state. An empty value means
	// nothing is appended in that state.
	States map[string]string `json:"states,omitempty"`
}

// taskNoteKind renders one kind of task note. render returns "" when the
// note does not apply; state is "" for the every-state rendering.
type taskNoteKind struct {
	kind   NoteKind
	render func(schema TaskSchema, state State) string
}

// taskNoteKinds are the task notes in the order a reference page prints them.
var taskNoteKinds = []taskNoteKind{
	{NoteDeprecation, deprecationNote},
	{NoteImperative, imperativeNote},
	{NoteRequirements, requirementsNote},
	{NoteRunnerRequirements, runnerRequirementsNote},
	{NoteExport, exportNote},
	{NoteProbe, probeNote},
	{NoteLifecycle, lifecycleNote},
	{NoteIdentity, identityNote},
	{NoteObjects, objectsNote},
	{NoteConstraints, constraintsNote},
	{NoteProperties, propertiesNote},
}

// fieldNoteKind renders one kind of field note. A sentence note reads as a
// sentence after the description, which is then ended with a period; every
// other note is a parenthetical.
type fieldNoteKind struct {
	sentence bool
	render   func(field FieldSchema, state State) string
}

// fieldNoteKinds are the field notes in the order the Parameters table
// appends them.
var fieldNoteKinds = []fieldNoteKind{
	{true, itemFieldsNote},
	{false, orderedNote},
	{false, uniqueNote},
	{false, requiredWhenNote},
	{false, forbiddenWhenNote},
	{false, runnerFileNote},
	{false, writeOnlyNote},
	{false, applyOnlyNote},
	{false, sensitiveNote},
}

// buildTaskNotes renders the notes of a task whose other catalog fields are
// already filled in.
func buildTaskNotes(schema TaskSchema) []NoteSchema {
	states := noteStates(schema.Fields)
	var notes []NoteSchema
	for _, kind := range taskNoteKinds {
		markdown := kind.render(schema, "")
		if markdown == "" {
			continue
		}
		notes = append(notes, NoteSchema{
			Kind:     kind.kind,
			Markdown: markdown,
			States: renderByState(states, markdown, func(state State) string {
				return kind.render(schema, state)
			}),
		})
	}
	return notes
}

// applyFieldNotes fills in the Note of every field, element fields included.
// states are the task's, since an element's conditional states are too.
func applyFieldNotes(fields []FieldSchema, states []State) {
	for i := range fields {
		field := &fields[i]
		if field.Item != nil {
			applyFieldNotes(field.Item.Fields, states)
		}
		suffix := fieldNoteSuffix(*field, "")
		if suffix == "" {
			field.Note = nil
			continue
		}
		snapshot := *field
		field.Note = &FieldNoteSchema{
			Suffix: suffix,
			States: renderByState(states, suffix, func(state State) string {
				return fieldNoteSuffix(snapshot, state)
			}),
		}
	}
}

// fieldNoteSuffix joins a field's notes onto its description and returns
// what was appended: a space before each note unless the description is
// empty, and a period first when a sentence note follows a description that
// lacks one.
func fieldNoteSuffix(field FieldSchema, state State) string {
	text := strings.TrimSpace(field.Description)
	var suffix strings.Builder
	for _, kind := range fieldNoteKinds {
		note := kind.render(field, state)
		if note == "" {
			continue
		}
		if kind.sentence && text != "" && !strings.HasSuffix(text, ".") {
			suffix.WriteString(".")
			text += "."
		}
		if text != "" {
			suffix.WriteString(" ")
			text += " "
		}
		suffix.WriteString(note)
		text += note
	}
	return suffix.String()
}

// renderByState renders a note once per state, and returns nil when every
// rendering equals the every-state text, so a note that does not depend on
// the state carries no per-state copy.
func renderByState(states []State, all string, render func(State) string) map[string]string {
	byState := make(map[string]string, len(states))
	differs := false
	for _, state := range states {
		text := render(state)
		byState[string(state)] = text
		if text != all {
			differs = true
		}
	}
	if !differs {
		return nil
	}
	return byState
}

// noteStates are the values of a task's `state` field, in declared order.
func noteStates(fields []FieldSchema) []State {
	for _, field := range fields {
		if field.Name != "state" {
			continue
		}
		states := make([]State, len(field.Choices))
		for i, choice := range field.Choices {
			states[i] = State(choice)
		}
		return states
	}
	return nil
}

// appliesIn reports whether something limited to states applies in state.
// An empty limit means every state, and state "" stands for every state.
func appliesIn(states []State, state State) bool {
	if state == "" || len(states) == 0 {
		return true
	}
	for _, s := range states {
		if s == state {
			return true
		}
	}
	return false
}

// containsState reports whether states, as recipe strings, include state.
func containsState(states []string, state State) bool {
	for _, s := range states {
		if s == string(state) {
			return true
		}
	}
	return false
}

// itemFieldsNote names the keys of a list-of-struct field's element, so the
// Parameters table can describe the item shape inline. Only a list qualifies:
// a dict's entries are keyed rather than shaped, and its value type is
// already carried by the type column.
func itemFieldsNote(field FieldSchema, _ State) string {
	if field.Type != TypeList || field.Item == nil || field.Item.Type != TypeObject || len(field.Item.Fields) == 0 {
		return ""
	}
	names := make([]string, 0, len(field.Item.Fields))
	for _, item := range field.Item.Fields {
		names = append(names, item.Name)
	}
	return "Each item has: " + strings.Join(names, ", ") + "."
}

func orderedNote(field FieldSchema, _ State) string {
	if field.Ordered == nil || !*field.Ordered {
		return ""
	}
	return "(order is significant)"
}

func uniqueNote(field FieldSchema, _ State) string {
	if !field.Unique {
		return ""
	}
	return "(no duplicates)"
}

func requiredWhenNote(field FieldSchema, state State) string {
	if len(field.RequiredWhen) == 0 {
		return ""
	}
	if state == "" {
		return "(required when state is " + joinWords(field.RequiredWhen, "or", "") + ")"
	}
	if containsState(field.RequiredWhen, state) {
		return "(required)"
	}
	return ""
}

func forbiddenWhenNote(field FieldSchema, state State) string {
	if len(field.ForbiddenWhen) == 0 {
		return ""
	}
	if state == "" {
		return "(must not be set when state is " + joinWords(field.ForbiddenWhen, "or", "") + ")"
	}
	if containsState(field.ForbiddenWhen, state) {
		return "(must not be set)"
	}
	return ""
}

func runnerFileNote(field FieldSchema, _ State) string {
	if !field.RunnerFile {
		return ""
	}
	return "(read from the machine running docket)"
}

func writeOnlyNote(field FieldSchema, _ State) string {
	if !field.WriteOnly {
		return ""
	}
	return "(not returned when reading state)"
}

func applyOnlyNote(field FieldSchema, _ State) string {
	if !field.ApplyOnly {
		return ""
	}
	return "(applied only, not read back)"
}

func sensitiveNote(field FieldSchema, _ State) string {
	if !field.Sensitive {
		return ""
	}
	return "(sensitive)"
}

// deprecationNote is the Deprecated admonition for a task that declares one.
// The page prints it between the Synopsis and the Requirements and
// Parameters so the reader sees it before scanning the field table.
func deprecationNote(schema TaskSchema, _ State) string {
	if schema.Deprecation == "" {
		return ""
	}
	return "> **Deprecated:** " + EscapeProse(schema.Deprecation)
}

// imperativeNote is the note for a task that runs an operation rather than
// declaring a resource, so a reader planning a teardown knows there is
// nothing to undo.
func imperativeNote(schema TaskSchema, _ State) string {
	if !schema.Imperative {
		return ""
	}
	return "> **Imperative:** this task runs an operation rather than declaring a resource. It has nothing to undo, so it offers no `absent` or `clear` state."
}

// requirementsNote is the Requirements list: the Dokku floor first, then one
// line per plugin.
func requirementsNote(schema TaskSchema, _ State) string {
	if schema.MinimumDokkuVersion == "" && len(schema.Requirements) == 0 {
		return ""
	}
	lines := []string{"## Requirements", ""}
	if schema.MinimumDokkuVersion != "" {
		lines = append(lines, "- Dokku >= "+EscapeProse(schema.MinimumDokkuVersion))
	}
	for _, requirement := range schema.Requirements {
		lines = append(lines, "- "+EscapeProse(requirement))
	}
	return strings.Join(lines, "\n")
}

// runnerRequirementsNote lists the fields naming files docket reads on the
// machine it runs on rather than on the dokku server. Element fields are
// listed under a `parent[].child` name, since the Parameters table lists
// those by name only.
func runnerRequirementsNote(schema TaskSchema, _ State) string {
	names := runnerFileNames("", schema.Fields)
	if len(names) == 0 {
		return ""
	}
	lines := []string{"## Runner requirements", ""}
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("- `%s` is read from the machine running docket, not the dokku server, so the file must exist there even when docket drives the server over `--host`.", name))
	}
	return strings.Join(lines, "\n")
}

// runnerFileNames returns the names of the runner-side file fields in
// declaration order, descending into structured list and dict elements.
func runnerFileNames(prefix string, fields []FieldSchema) []string {
	var names []string
	for _, field := range fields {
		if field.RunnerFile {
			names = append(names, prefix+field.Name)
		}
		if field.Item != nil && field.Item.Type == TypeObject {
			names = append(names, runnerFileNames(prefix+field.Name+"[].", field.Item.Fields)...)
		}
	}
	return names
}

// exportNote states whether `docket export` can reconstruct the task from a
// live server. Every task declares this; the note is omitted only for one
// that does not, which the export coverage test prevents.
func exportNote(schema TaskSchema, _ State) string {
	support := schema.Export
	if support.Status == "" {
		return ""
	}
	var label string
	switch support.Status {
	case ExportSupported:
		label = "Supported"
	case ExportPartial:
		label = "Partial"
	case ExportUnsupported:
		label = "Not supported"
	default:
		label = string(support.Status)
	}
	return supportNote("Export support", label, support.Caveat)
}

// probeNote states whether `plan` can read the task's current state before
// deciding on drift. A task rendered as "Not supported" plans as drift on
// every run, so it never lets `plan --detailed-exitcode` exit 0.
func probeNote(schema TaskSchema, _ State) string {
	support := schema.Probe
	if support.Status == "" {
		return ""
	}
	var label string
	switch support.Status {
	case ProbeSupported:
		label = "Supported"
	case ProbePartial:
		label = "Partial"
	case ProbeUnsupported:
		label = "Not supported"
	default:
		label = string(support.Status)
	}
	return supportNote("Probe support", label, support.Caveat)
}

// lifecycleNote names the state that applies the task and the state that
// tears it down, and the fields a teardown resets because its state refuses
// them, so a reader driving the task from another tool knows both without
// working them out from the `state` choices.
func lifecycleNote(schema TaskSchema, _ State) string {
	lifecycle := schema.Lifecycle
	if lifecycle.Apply == "" {
		return ""
	}
	line := fmt.Sprintf("Applied with state `%s`", lifecycle.Apply)
	if lifecycle.Teardown == "" {
		line += " and has no teardown state, so tearing it down leaves the server unchanged."
	} else {
		line += fmt.Sprintf(" and torn down with state `%s`", lifecycle.Teardown)
		if names := forbiddenFieldNames("", schema.Fields, lifecycle.Teardown); len(names) > 0 {
			pronoun := "them"
			if len(names) == 1 {
				pronoun = "it"
			}
			line += fmt.Sprintf(", which refuses %s, so a teardown resets %s", joinCodeNames(names), pronoun)
		}
		line += "."
	}
	note := "See [Applying and tearing down a task](../embedding.md#applying-and-tearing-down-a-task)."
	return "## Lifecycle\n\n" + line + " " + note
}

// forbiddenFieldNames returns the names of the fields forbidden in state, in
// declaration order, descending into structured list and dict elements.
func forbiddenFieldNames(prefix string, fields []FieldSchema, state State) []string {
	var names []string
	for _, field := range fields {
		if containsState(field.ForbiddenWhen, state) {
			names = append(names, prefix+field.Name)
			continue
		}
		if field.Item != nil && field.Item.Type == TypeObject {
			names = append(names, forbiddenFieldNames(prefix+field.Name+"[].", field.Item.Fields, state)...)
		}
	}
	return names
}

// supportNote renders an Export or Probe support section.
func supportNote(heading, label, caveat string) string {
	line := label
	if caveat != "" {
		line += " - " + EscapeProse(caveat)
	}
	return "## " + heading + "\n\n" + line + "."
}

// identityNote names the fields that select the resource the task manages
// and, where the task owns a collection, what identifies one entry in it. The
// address form is explained once in docs/task-envelope.md rather than on
// every page; what a reader needs here is which fields go into it.
func identityNote(schema TaskSchema, _ State) string {
	identity := schema.Identity
	if len(identity.Keys) == 0 {
		return ""
	}

	line := "Keyed by " + joinCodeNames(identity.Keys) + "."
	if len(identity.Keys) > 1 {
		line += " Fields left empty are omitted from the address."
	}
	for _, collection := range identity.Collections {
		line += fmt.Sprintf(" Manages the whole `%s` collection", collection.Name)
		switch collection.Item {
		case ItemIdentityMapKey:
			line += "; entries are identified by their key."
		case ItemIdentityFields:
			if len(collection.ItemKeys) > 0 {
				line += "; entries are identified by " + joinCodeNames(collection.ItemKeys) + "."
			} else {
				line += "."
			}
		default:
			line += "; entries are identified by value."
		}
	}
	return "## Identity\n\n" + line
}

// objectsNote names the app and the datastore service the task changes,
// whether it creates them, and when it restarts them. The locking it implies
// is explained once in docs/embedding.md rather than on every page.
//
// In one state, a creating task says only which of creating and destroying
// it does, and a restart limited to other states is left out.
func objectsNote(schema TaskSchema, state State) string {
	if len(schema.Objects) == 0 {
		return ""
	}
	var lines []string
	for _, object := range schema.Objects {
		var line string
		switch object.Kind {
		case ObjectService:
			line = "- The datastore service named by " + joinCodeNames(object.Keys) + "."
		default:
			line = "- The app named by `" + strings.Join(object.Keys, "`, `") + "`."
		}
		if object.Creates {
			switch state {
			case "":
				line += " Creates it, and destroys it in state `absent`."
			case StateAbsent:
				line += " Destroys it."
			default:
				line += " Creates it."
			}
		}
		if r := object.Restarts; r != nil && appliesIn(r.States, state) {
			line += " Can restart or redeploy it"
			var conditions []string
			if len(r.When) > 0 {
				conditions = append(conditions, "when "+joinWords(r.When, "or", "`")+" is true")
			}
			if len(r.Unless) > 0 {
				conditions = append(conditions, "unless "+joinWords(r.Unless, "or", "`")+" is true")
			}
			if len(r.States) > 0 && state == "" {
				conditions = append(conditions, "when `state` is "+joinWords(stateWords(r.States), "or", "`"))
			}
			if len(conditions) > 0 {
				line += " " + strings.Join(conditions, ", and ")
			}
			line += "."
		}
		lines = append(lines, line)
	}
	note := "Tasks applied at once on the same object take turns; see [Running tasks concurrently](../embedding.md#running-tasks-concurrently)."
	return "## Objects\n\n" + strings.Join(lines, "\n") + "\n\n" + note
}

// constraintsNote lists the rules over several fields that the task, and the
// elements of its structured lists, declare. Rules about one field and the
// state - required in some states, forbidden in others - are noted in that
// field's Parameters row instead.
//
// In one state, only the rules that apply in it are listed, without their
// state clause.
func constraintsNote(schema TaskSchema, state State) string {
	var lines []string
	for _, c := range schema.Constraints {
		if appliesIn(c.States, state) {
			lines = append(lines, "- "+constraintSentence(c, state))
		}
	}
	for _, field := range schema.Fields {
		if field.Item == nil {
			continue
		}
		for _, c := range field.Item.Constraints {
			if !appliesIn(c.States, state) {
				continue
			}
			sentence := constraintSentence(c, state)
			if !strings.HasPrefix(sentence, "`") {
				sentence = strings.ToLower(sentence[:1]) + sentence[1:]
			}
			lines = append(lines, fmt.Sprintf("- Each `%s` item: %s", field.Name, sentence))
		}
	}
	if len(lines) == 0 {
		return ""
	}
	return "## Constraints\n\n" + strings.Join(lines, "\n")
}

// constraintSentence renders one multi-field rule as a sentence. The state
// clause is printed only for the every-state rendering.
func constraintSentence(c Constraint, state State) string {
	var sentence string
	switch c.Kind {
	case ConstraintExactlyOneOf:
		sentence = "Exactly one of " + joinWords(c.Fields, "or", "`") + " must be set"
	case ConstraintAtLeastOneOf:
		sentence = "At least one of " + joinWords(c.Fields, "or", "`") + " must be set"
	case ConstraintAllOrNoneOf:
		sentence = joinWords(c.Fields, "and", "`") + " must be set together or not at all"
	case ConstraintConflictsWith:
		sentence = "`" + c.Field + "` must not be set together with " + joinWords(c.Fields, "or", "`")
	case ConstraintRequiredWith:
		sentence = "`" + c.Field + "` requires " + joinWords(c.Fields, "and", "`")
	default:
		sentence = string(c.Kind) + " " + joinWords(c.Fields, "and", "`")
	}
	if len(c.States) > 0 && state == "" {
		sentence += " when `state` is " + joinWords(stateWords(c.States), "or", "`")
	}
	return sentence + "."
}

// propertiesNote is the Properties table for a task that manages a dokku
// property table: every name its `property` field accepts, the scopes that
// accept it, and the report keys the drift probe reads. Without it the only
// way to discover a legal property name is to trigger a validation error.
func propertiesNote(schema TaskSchema, _ State) string {
	table := schema.PropertySchema
	if table == nil || len(table.Properties) == 0 {
		return ""
	}

	var b strings.Builder
	b.WriteString("## Properties\n\n")
	fmt.Fprintf(&b,
		"`property` accepts one of the following, applied with `dokku %s`. A property with no form in a scope is rejected there, matching dokku's own rejection.\n\n",
		table.Subcommand,
	)
	b.WriteString("| Property | Scopes | Report key (app) | Report key (global) |\n")
	b.WriteString("| --- | --- | --- | --- |\n")
	for _, property := range table.Properties {
		name := "`" + property.Name + "`"
		if property.Sensitive {
			name += " (sensitive)"
		}
		fmt.Fprintf(&b,
			"| %s | %s | %s | %s |\n",
			name,
			strings.Join(property.Scopes, ", "),
			codeOrBlank(property.AppReportKey),
			codeOrBlank(property.GlobalReportKey),
		)
	}

	for _, family := range table.Dynamic {
		fmt.Fprintf(&b, "\nNames starting with `%s` are also accepted %s. dokku validates them through `%s` rather than through its report schema, so they cannot be listed above. ",
			family.Prefix, scopePhrase(family.Scopes), table.Subcommand)
		if family.Probeable {
			b.WriteString("The plugin reports each one it has been given, so they probe for drift like any listed property.")
		} else {
			b.WriteString("The plugin does not report them, so they are applied on every run and never converge.")
		}
		if family.Sensitive {
			b.WriteString(" Their values are treated as secrets and masked.")
		}
		b.WriteString("\n")
	}

	for _, family := range table.Rejected {
		fmt.Fprintf(&b, "\nNames starting with `%s` are rejected here - they are managed by `%s`, since %s.\n",
			family.Prefix, family.Replacement, family.Reason)
	}
	return strings.TrimRight(b.String(), "\n")
}

// scopePhrase renders a dynamic family's scopes as the clause that follows
// "are also accepted": a family with one scope is spelled out as a
// restriction, since that is the part a reader has to know, and one with both
// reads as no restriction at all.
func scopePhrase(scopes []string) string {
	if len(scopes) == 1 {
		return fmt.Sprintf("in the %s scope only", scopes[0])
	}
	return "in the " + strings.Join(scopes, " and ") + " scopes"
}

// codeOrBlank renders a report key as inline code, or an empty cell when the
// property has no form in that scope.
func codeOrBlank(key string) string {
	if key == "" {
		return ""
	}
	return "`" + key + "`"
}

// stateWords spells states as recipe strings.
func stateWords(states []State) []string {
	words := make([]string, len(states))
	for i, state := range states {
		words[i] = string(state)
	}
	return words
}

// joinWords renders words as `a`, `a or b`, or `a, b or c`, each wrapped in
// quote - a backtick for code, or nothing for prose.
func joinWords(words []string, conjunction, quote string) string {
	quoted := make([]string, len(words))
	for i, word := range words {
		quoted[i] = quote + word + quote
	}
	if len(quoted) <= 1 {
		return strings.Join(quoted, "")
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + " " + conjunction + " " + quoted[len(quoted)-1]
}

// joinCodeNames renders a list of field names as backticked, comma-separated
// prose with a trailing "and".
func joinCodeNames(names []string) string {
	quoted := make([]string, len(names))
	for i, name := range names {
		quoted[i] = "`" + name + "`"
	}
	switch len(quoted) {
	case 1:
		return quoted[0]
	case 2:
		return quoted[0] + " and " + quoted[1]
	}
	return strings.Join(quoted[:len(quoted)-1], ", ") + ", and " + quoted[len(quoted)-1]
}

// EscapeProse escapes a bare `<...>` in task-supplied prose so a placeholder
// such as `<service>:set` in a description or caveat renders as text.
// Markdown treats a bare `<service>` as an inline HTML tag, which GitHub drops
// from the page and which markdown-lint rejects as MD033. Only a `<` that
// looks like a tag start (a letter, `/`, `!` or `?` after it) is rewritten to
// `&lt;`; a bare `>` and a comparison such as `>=` are left alone, since they
// are not HTML. Brackets inside a backtick code span are left alone too, since
// a code span already renders them literally and an entity there would show
// up as `&lt;`.
func EscapeProse(s string) string {
	runes := []rune(s)
	var b strings.Builder
	inCode := false
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch {
		case r == '`':
			inCode = !inCode
			b.WriteRune(r)
		case inCode:
			b.WriteRune(r)
		case r == '<' && i+1 < len(runes) && isHTMLTagStart(runes[i+1]):
			b.WriteString("&lt;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// isHTMLTagStart reports whether r can open an HTML tag after a `<`.
func isHTMLTagStart(r rune) bool {
	return (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || r == '/' || r == '!' || r == '?'
}
