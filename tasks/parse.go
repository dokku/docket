package tasks

import (
	"fmt"
	"reflect"
	"strings"

	defaults "github.com/mcuadros/go-defaults"
	yaml "gopkg.in/yaml.v3"
)

// parse.go is the shared, position-carrying recipe parser used by both the
// offline validator (tasks.Validate) and the runtime loader
// (GetPlaysWithFormat). One structural walk over the yaml.v3 node tree
// produces a parsedRecipe AST plus a list of Problems anchored to source
// positions. The validator consumes every problem at once; the loader
// converts the first problem into its fail-fast error via recipeParseError,
// so both commands agree on what is structurally valid by construction.

// parsedRecipe is the parsed form of one rendered recipe document.
type parsedRecipe struct {
	// Doc is the document body node (the top-level sequence of plays).
	// nil when the document failed to parse or was empty.
	Doc *yaml.Node

	// Plays holds one entry per top-level play, in source order.
	Plays []*parsedPlay

	// Problems are document-level findings (yaml parse errors, recipe
	// shape violations). Play- and entry-level findings live on the
	// parsedPlay / parsedTaskEntry they anchor to.
	Problems []Problem
}

// parsedPlay is the parsed form of one play mapping.
type parsedPlay struct {
	// Node is the play's mapping node; positions anchor here.
	Node *yaml.Node

	// Index is the play's 1-based position in the recipe.
	Index int

	// Label is the diagnostic label ("play #N"), matching the validator's
	// historical labelling.
	Label string

	// Name is the play's scalar `name:` value, "" when absent.
	Name string

	// TasksNode is the play's `tasks:` sequence node, nil when absent.
	TasksNode *yaml.Node

	// Entries holds the parsed task entries, in source order.
	Entries []*parsedTaskEntry

	// Problems are play-level findings (non-mapping play, unexpected play
	// keys, non-sequence tasks value).
	Problems []Problem
}

// parsedTaskEntry is the parsed form of one task entry mapping, including
// its typed envelope values and the partitioned task-type key. Group
// entries (block / rescue / always) carry their children recursively.
type parsedTaskEntry struct {
	// Node is the entry's mapping node; positions anchor here.
	Node *yaml.Node

	// Index is the entry's 1-based position within its task list (or
	// group clause).
	Index int

	// Label is the diagnostic label ("task #N" or `task #N "name"`).
	Label string

	// Name is the entry's `name:` value, "" when absent.
	Name string

	// NameNode is the value node of `name:`, nil when absent.
	NameNode *yaml.Node

	// Typed envelope values. Decoded during the walk; a wrong type is
	// reported as a Problem and leaves the zero value here.
	Tags         []string
	When         string
	Register     string
	ChangedWhen  string
	FailedWhen   string
	IgnoreErrors bool

	// LoopNode is the raw `loop:` value node (list literal or expr
	// string); nil when absent. The loader decodes it at envelope-build
	// time, the validator checks the scalar form compiles.
	LoopNode *yaml.Node

	// TypeKey / TypeNode / BodyNode identify the single registered
	// task-type key and its body. All zero for group entries and for
	// entries whose structure was rejected.
	TypeKey  string
	TypeNode *yaml.Node
	BodyNode *yaml.Node

	// Group clause nodes and their recursively parsed children.
	IsGroup    bool
	BlockNode  *yaml.Node
	RescueNode *yaml.Node
	AlwaysNode *yaml.Node
	Block      []*parsedTaskEntry
	Rescue     []*parsedTaskEntry
	Always     []*parsedTaskEntry

	// Problems are the structural findings anchored to this entry.
	Problems []Problem

	// Valid is false when a structural problem prevents the entry from
	// being decoded further (body validation and envelope building skip
	// invalid entries).
	Valid bool
}

// normalizeRecipeBytes converts a recipe's on-disk surface syntax to YAML
// bytes. JSON5 input is converted via json5ToYAMLBytes; YAML (and any
// unknown format value) passes through unchanged. A conversion failure is
// returned as a json5_parse problem.
func normalizeRecipeBytes(data []byte, format string) ([]byte, []Problem) {
	if !IsJSON5Format(format) {
		return data, nil
	}
	converted, err := json5ToYAMLBytes(data)
	if err != nil {
		return nil, []Problem{{
			Code:    "json5_parse",
			Message: err.Error(),
		}}
	}
	// json5.Unmarshal deduped any duplicate keys during the conversion
	// above; scan the original bytes so a copy-pasted key is rejected the
	// way yaml.v3 rejects a duplicate YAML key (#318).
	if dup := detectJSON5DuplicateKeys(data); dup != nil {
		return nil, []Problem{{
			Code:    "duplicate_key",
			Line:    dup.Line,
			Column:  dup.Column,
			Message: fmt.Sprintf("duplicate key %q", dup.Key),
		}}
	}
	return converted, nil
}

// parseRecipe parses data (YAML bytes, already sigil-rendered and
// format-normalized) into a parsedRecipe. Structural findings are
// collected as Problems rather than returned as an error so the validator
// can report all of them; the loader uses recipeParseError to fail fast on
// the first one.
func parseRecipe(data []byte) *parsedRecipe {
	out := &parsedRecipe{}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		line, col := parseYAMLErrorPosition(err.Error())
		out.Problems = append(out.Problems, Problem{
			Code:    "yaml_parse",
			Line:    line,
			Column:  col,
			Message: err.Error(),
		})
		return out
	}

	doc := documentBody(&root)
	if doc == nil || (doc.Kind == yaml.ScalarNode && doc.Tag == "!!null") {
		out.Problems = append(out.Problems, Problem{
			Code:    "recipe_shape",
			Message: "no recipe found in tasks file",
		})
		return out
	}
	if doc.Kind != yaml.SequenceNode {
		out.Problems = append(out.Problems, Problem{
			Code:    "recipe_shape",
			Line:    doc.Line,
			Column:  doc.Column,
			Message: "recipe must be a yaml list of plays",
		})
		return out
	}
	out.Doc = doc

	for i, play := range doc.Content {
		out.Plays = append(out.Plays, parsePlay(play, i+1))
	}
	return out
}

// parsePlay walks one play mapping into a parsedPlay.
func parsePlay(play *yaml.Node, index int) *parsedPlay {
	pp := &parsedPlay{
		Node:  play,
		Index: index,
		Label: fmt.Sprintf("play #%d", index),
	}

	if play.Kind != yaml.MappingNode {
		pp.Problems = append(pp.Problems, Problem{
			Code:    "recipe_shape",
			Play:    pp.Label,
			Line:    play.Line,
			Column:  play.Column,
			Message: "play must be a yaml mapping with inputs and/or tasks",
		})
		return pp
	}

	pp.Name = scalarChild(play, "name")

	for i := 0; i < len(play.Content); i += 2 {
		key := play.Content[i].Value
		if !allowedPlayKeys[key] {
			pp.Problems = append(pp.Problems, Problem{
				Code:    "recipe_shape",
				Play:    pp.Label,
				Line:    play.Content[i].Line,
				Column:  play.Content[i].Column,
				Message: fmt.Sprintf("unexpected play key %q (expected: %s)", key, allowedPlayKeysList),
			})
		}
	}

	flagReservedInputNames(pp, play)

	tasksNode := mappingValue(play, "tasks")
	if tasksNode == nil {
		return pp
	}
	if tasksNode.Kind != yaml.SequenceNode {
		pp.Problems = append(pp.Problems, Problem{
			Code:    "recipe_shape",
			Play:    pp.Label,
			Line:    tasksNode.Line,
			Column:  tasksNode.Column,
			Message: "tasks must be a yaml list",
		})
		return pp
	}
	pp.TasksNode = tasksNode

	for i, task := range tasksNode.Content {
		pp.Entries = append(pp.Entries, parseTaskEntry(task, i+1, pp.Label))
	}
	flagDuplicateTaskNames(pp)
	return pp
}

// flagDuplicateTaskNames rejects two top-level entries in a play that
// share a literal name. At runtime such entries collapse onto the same
// ordered-map key, silently dropping the earlier task (#307). loop:
// entries are skipped because their names gain a per-item suffix at
// expansion time (loop-suffix collisions are caught by the runtime guard
// in GetPlaysWithFormat instead). The problem is recorded on the
// duplicate entry so both the loader and the validator surface it.
func flagDuplicateTaskNames(pp *parsedPlay) {
	seen := map[string]*parsedTaskEntry{}
	for _, entry := range pp.Entries {
		if entry.Name == "" || entry.LoopNode != nil {
			continue
		}
		first, dup := seen[entry.Name]
		if !dup {
			seen[entry.Name] = entry
			continue
		}
		hint := fmt.Sprintf("first declared on %s", first.Label)
		if first.NameNode != nil {
			hint = fmt.Sprintf("first declared on %s (line %d)", first.Label, first.NameNode.Line)
		}
		anchor := entry.NameNode
		if anchor == nil {
			anchor = entry.Node
		}
		entry.Problems = append(entry.Problems, Problem{
			Code:    "duplicate_task_name",
			Play:    pp.Label,
			Task:    entry.Label,
			Line:    anchor.Line,
			Column:  anchor.Column,
			Message: fmt.Sprintf("duplicate task name %q", entry.Name),
			Hint:    hint,
		})
	}
}

// flagReservedInputNames rejects any input in the play whose name
// collides with a built-in CLI flag (see ReservedInputNames). Such a name
// used to make pflag panic before flag parsing began; both the loader and
// the validator now reject it offline (#302).
func flagReservedInputNames(pp *parsedPlay, play *yaml.Node) {
	inputsNode := mappingValue(play, "inputs")
	if inputsNode == nil || inputsNode.Kind != yaml.SequenceNode {
		return
	}
	for _, in := range inputsNode.Content {
		nameNode := mappingValue(in, "name")
		if nameNode == nil || nameNode.Kind != yaml.ScalarNode {
			continue
		}
		if !ReservedInputNames[nameNode.Value] {
			continue
		}
		pp.Problems = append(pp.Problems, Problem{
			Code:    "reserved_input_name",
			Play:    pp.Label,
			Line:    nameNode.Line,
			Column:  nameNode.Column,
			Message: fmt.Sprintf("input name %q is reserved for a built-in flag and cannot be used as an input name", nameNode.Value),
		})
	}
}

// parseTaskEntry walks one task entry mapping into a parsedTaskEntry,
// partitioning envelope keys from the task-type key, decoding typed
// envelope values, and recursing into group clauses.
func parseTaskEntry(task *yaml.Node, index int, playLabel string) *parsedTaskEntry {
	e := &parsedTaskEntry{
		Node:  task,
		Index: index,
		Label: fmt.Sprintf("task #%d", index),
		Valid: true,
	}

	addProblem := func(code, message, hint string, node *yaml.Node) {
		p := Problem{
			Code:    code,
			Play:    playLabel,
			Task:    e.Label,
			Message: message,
			Hint:    hint,
		}
		if node != nil {
			p.Line = node.Line
			p.Column = node.Column
		}
		e.Problems = append(e.Problems, p)
	}
	// problem records a structural finding and marks the entry invalid so
	// downstream body validation / envelope building skips it. Envelope-key
	// type errors that do not block body decoding use addProblem instead.
	problem := func(code, message, hint string, node *yaml.Node) {
		addProblem(code, message, hint, node)
		e.Valid = false
	}

	if task.Kind != yaml.MappingNode {
		problem("task_entry_shape", "task entry must be a yaml mapping", "", task)
		return e
	}

	var (
		taskTypeKeys []string
		unknownKeys  []string
		unknownNodes []*yaml.Node
	)

	for i := 0; i < len(task.Content); i += 2 {
		keyNode := task.Content[i]
		valueNode := task.Content[i+1]
		key := keyNode.Value

		switch key {
		case "name":
			e.NameNode = valueNode
			switch {
			case valueNode.Kind == yaml.ScalarNode && valueNode.Tag == "!!null":
				// A bare `name:` (null value) means "no name" - the
				// loader auto-generates one, matching an omitted key.
			case valueNode.Kind == yaml.ScalarNode && valueNode.Tag == "!!str":
				e.Name = valueNode.Value
			default:
				// A non-string name (e.g. `name: 123`) used to be silently
				// dropped and replaced with a random auto-name; reject it
				// with a typed error like when:/register: (#342).
				addProblem("envelope_key_type",
					fmt.Sprintf("name must be a string, got %s", scalarTypeName(valueNode)), "", valueNode)
			}
		case "tags":
			var raw interface{}
			if err := valueNode.Decode(&raw); err != nil {
				problem("envelope_key_type", fmt.Sprintf("tags decode error: %v", err), "", valueNode)
				continue
			}
			tags, err := decodeTags(raw)
			if err != nil {
				problem("envelope_key_type", err.Error(), "", valueNode)
				continue
			}
			e.Tags = tags
		case "when":
			s, ok, typeName := scalarString(valueNode)
			if !ok {
				problem("envelope_key_type", fmt.Sprintf("when must be a string expression, got %s", typeName), "", valueNode)
				continue
			}
			e.When = s
		case "register":
			s, ok, typeName := scalarString(valueNode)
			if !ok {
				problem("envelope_key_type", fmt.Sprintf("register must be a string, got %s", typeName), "", valueNode)
				continue
			}
			e.Register = s
		case "changed_when":
			s, ok, typeName := scalarString(valueNode)
			if !ok {
				problem("envelope_key_type", fmt.Sprintf("changed_when must be a string expression, got %s", typeName), "", valueNode)
				continue
			}
			e.ChangedWhen = s
		case "failed_when":
			s, ok, typeName := scalarString(valueNode)
			if !ok {
				problem("envelope_key_type", fmt.Sprintf("failed_when must be a string expression, got %s", typeName), "", valueNode)
				continue
			}
			e.FailedWhen = s
		case "ignore_errors":
			var b bool
			if err := valueNode.Decode(&b); err != nil {
				problem("envelope_key_type", fmt.Sprintf("ignore_errors must be a bool, got %s", scalarTypeName(valueNode)), "", valueNode)
				continue
			}
			e.IgnoreErrors = b
		case "loop":
			e.LoopNode = valueNode
		case "block":
			e.BlockNode = valueNode
			e.IsGroup = true
		case "rescue":
			e.RescueNode = valueNode
		case "always":
			e.AlwaysNode = valueNode
		default:
			if dependentIssue, reserved := reservedEnvelopeKeys[key]; reserved {
				problem("envelope_key_unsupported",
					fmt.Sprintf("envelope key %q is reserved but not yet supported", key),
					fmt.Sprintf("activates with %s", dependentIssue), keyNode)
				continue
			}
			if _, registered := RegisteredTasks[key]; registered {
				taskTypeKeys = append(taskTypeKeys, key)
				e.TypeKey = key
				e.TypeNode = keyNode
				e.BodyNode = valueNode
				continue
			}
			unknownKeys = append(unknownKeys, key)
			unknownNodes = append(unknownNodes, keyNode)
		}
	}

	if e.Name != "" {
		e.Label = fmt.Sprintf("task #%d %q", index, e.Name)
		for i := range e.Problems {
			e.Problems[i].Task = e.Label
		}
	}

	if e.RescueNode != nil && e.BlockNode == nil {
		problem("block_orphan_clause", "rescue: requires a block: in the same task entry", "", e.RescueNode)
		return e
	}
	if e.AlwaysNode != nil && e.BlockNode == nil {
		problem("block_orphan_clause", "always: requires a block: in the same task entry", "", e.AlwaysNode)
		return e
	}

	if e.IsGroup {
		if len(taskTypeKeys) > 0 {
			problem("block_with_task_type",
				fmt.Sprintf("block: group entry cannot also carry task-type key %q", taskTypeKeys[0]), "", e.TypeNode)
		}
		for i, key := range unknownKeys {
			problem("unknown_key", unknownKeyMessage(key), unknownKeyHint(key), unknownNodes[i])
		}
		if e.BlockNode.Kind != yaml.SequenceNode {
			problem("block_shape", "block: must be a yaml list of task entries", "", e.BlockNode)
			return e
		}
		if len(e.BlockNode.Content) == 0 {
			problem("block_empty", "block: must contain at least one child task", "", e.BlockNode)
		}
		e.TypeKey = ""
		e.TypeNode = nil
		e.BodyNode = nil
		e.Block = parseGroupClause(e.BlockNode, playLabel)
		e.Rescue = parseGroupClause(e.RescueNode, playLabel)
		e.Always = parseGroupClause(e.AlwaysNode, playLabel)
		return e
	}

	switch {
	case len(taskTypeKeys) == 0 && len(unknownKeys) == 1:
		problem("unknown_task_type",
			fmt.Sprintf("unknown task type %q", unknownKeys[0]),
			unknownKeyHint(unknownKeys[0]), unknownNodes[0])
		return e
	case len(unknownKeys) > 0:
		for i, key := range unknownKeys {
			problem("unknown_key", unknownKeyMessage(key), unknownKeyHint(key), unknownNodes[i])
		}
		return e
	case len(taskTypeKeys) == 0:
		problem("task_entry_shape", "task entry must contain exactly one task-type key", "", task)
		return e
	case len(taskTypeKeys) > 1:
		problem("task_entry_shape",
			fmt.Sprintf("task entry has %d task-type keys (%s); exactly one is allowed", len(taskTypeKeys), strings.Join(taskTypeKeys, ", ")), "", task)
		e.TypeKey = ""
		e.TypeNode = nil
		e.BodyNode = nil
		return e
	}

	// A task-type key with a null body (`dokku_app:` with nothing after
	// it) leaves the value node as a !!null scalar. The old loader
	// allocated a nil struct pointer here and panicked in SetDefaults;
	// the old validator decoded it to a zero struct and reported confusing
	// missing_required_field noise. Reject the empty body directly so both
	// paths agree on a single, clear diagnostic.
	if isNullNode(e.BodyNode) {
		problem("empty_task_body", fmt.Sprintf("%s body must not be empty", e.TypeKey), "", e.TypeNode)
		e.TypeKey = ""
		e.TypeNode = nil
		e.BodyNode = nil
		return e
	}

	return e
}

// isNullNode reports whether node is absent or an explicit/implicit YAML
// null scalar. An empty mapping (`{}`) is not null, so a task with an
// intentionally empty body still decodes and surfaces its missing
// required fields as usual.
func isNullNode(node *yaml.Node) bool {
	return node == nil || (node.Kind == yaml.ScalarNode && node.Tag == "!!null")
}

// parseGroupClause parses the children of a block / rescue / always
// sequence node. A nil or non-sequence clause yields no children (the
// group-level shape problems are reported by parseTaskEntry).
func parseGroupClause(clause *yaml.Node, playLabel string) []*parsedTaskEntry {
	if clause == nil || clause.Kind != yaml.SequenceNode {
		return nil
	}
	out := make([]*parsedTaskEntry, 0, len(clause.Content))
	for i, child := range clause.Content {
		out = append(out, parseTaskEntry(child, i+1, playLabel))
	}
	return out
}

// scalarString decodes node into a string, reporting whether the value
// really was a string plus a human-readable name of the actual type for
// diagnostics. Matches the loader's historical `got %T` phrasing by
// naming the Go type the value would decode to.
func scalarString(node *yaml.Node) (string, bool, string) {
	var raw interface{}
	if err := node.Decode(&raw); err != nil {
		return "", false, scalarTypeName(node)
	}
	s, ok := raw.(string)
	if !ok {
		return "", false, fmt.Sprintf("%T", raw)
	}
	return s, true, "string"
}

// scalarTypeName renders a best-effort Go-style type name for a node used
// in envelope-type diagnostics when the value cannot be decoded.
func scalarTypeName(node *yaml.Node) string {
	var raw interface{}
	if err := node.Decode(&raw); err != nil {
		return node.Tag
	}
	return fmt.Sprintf("%T", raw)
}

// unknownKeyMessage renders the diagnostic for a key that is neither an
// envelope key nor a registered task type, on an entry that already has
// (or lacks) a task-type key. The allowlist text mirrors the loader's
// historical message.
func unknownKeyMessage(key string) string {
	return fmt.Sprintf("unknown envelope key %q (allowed: %s, or any registered task type)", key, strings.Join(envelopeAllowlistKeys, ", "))
}

// unknownKeyHint returns a did-you-mean hint for an unknown key, searching
// both the envelope allowlist and the registered task names.
func unknownKeyHint(key string) string {
	if suggestion := nearestEnvelopeOrTaskKey(key); suggestion != "" {
		return fmt.Sprintf("did you mean %q?", suggestion)
	}
	return ""
}

// allProblems flattens every problem in the parse result: document-level
// first, then per play in source order (play problems, then each entry's
// problems including group children, depth-first).
func (r *parsedRecipe) allProblems() []Problem {
	out := append([]Problem(nil), r.Problems...)
	for _, play := range r.Plays {
		out = append(out, play.Problems...)
		for _, entry := range play.Entries {
			out = append(out, entryProblems(entry)...)
		}
	}
	return out
}

// entryProblems returns the entry's own problems followed by its group
// children's, depth-first.
func entryProblems(e *parsedTaskEntry) []Problem {
	out := append([]Problem(nil), e.Problems...)
	for _, group := range [][]*parsedTaskEntry{e.Block, e.Rescue, e.Always} {
		for _, child := range group {
			out = append(out, entryProblems(child)...)
		}
	}
	return out
}

// parseTaskEntrySeq parses data (rendered YAML bytes) as a bare list of
// task entries. Used by loop-group expansion, which re-renders a group
// clause's YAML per iteration and needs the children parsed through the
// same structural walk the recipe-level parse uses.
func parseTaskEntrySeq(data []byte, playLabel string) ([]*parsedTaskEntry, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("decode error: %v", err)
	}
	seq := documentBody(&root)
	if seq == nil {
		return nil, nil
	}
	if seq.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("must be a list of task entries")
	}
	out := make([]*parsedTaskEntry, 0, len(seq.Content))
	for i, child := range seq.Content {
		out = append(out, parseTaskEntry(child, i+1, playLabel))
	}
	return out, nil
}

// problemToError renders a Problem as the loader's fail-fast error. The
// historical `task parse error:` / `parse error:` prefixes are preserved
// so operator-facing errors stay recognizable, now enriched with the
// problem's source position and did-you-mean hint.
func problemToError(p Problem) error {
	var b strings.Builder
	if p.Task != "" {
		b.WriteString("task parse error: ")
		b.WriteString(p.Task)
		if p.Line > 0 {
			fmt.Fprintf(&b, " (line %d)", p.Line)
		}
		b.WriteString(": ")
	} else {
		b.WriteString("parse error: ")
		if p.Line > 0 {
			fmt.Fprintf(&b, "line %d: ", p.Line)
		}
	}
	b.WriteString(p.Message)
	if p.Hint != "" {
		fmt.Fprintf(&b, " - %s", p.Hint)
	}
	return fmt.Errorf("%s", b.String())
}

// decodeTaskBytes decodes a marshaled task body into a fresh instance of
// the registered task type and applies its struct-tag defaults. The
// concrete struct is allocated with reflect on the registered pointer's
// element type, so a null body decodes to the zero struct instead of a
// nil pointer (the historical loader allocation panicked in SetDefaults
// on `dokku_app:` with no body). Shared by the loader's direct and loop
// decode paths and by the validator's body checks.
func decodeTaskBytes(typeKey string, body []byte) (Task, error) {
	registered, ok := RegisteredTasks[typeKey]
	if !ok {
		return nil, fmt.Errorf("unknown task type %q", typeKey)
	}
	v := reflect.New(reflect.TypeOf(registered).Elem())
	if err := yaml.Unmarshal(body, v.Interface()); err != nil {
		return nil, err
	}
	defaults.SetDefaults(v.Interface())
	task, ok := v.Interface().(Task)
	if !ok {
		return nil, fmt.Errorf("registered type for %q does not implement Task", typeKey)
	}
	return task, nil
}
