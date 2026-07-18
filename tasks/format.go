package tasks

import (
	"bytes"
	"fmt"
	"io"
	"strings"

	yaml "gopkg.in/yaml.v3"
)

// canonicalPlayKeys is the canonical key order inside a play mapping.
// Keys not in this list keep their relative order, appended after the
// canonical keys.
var canonicalPlayKeys = []string{"name", "tags", "when", "inputs", "tasks"}

// canonicalEnvelopeKeys is the canonical key order inside a task entry's
// envelope. The task-type key (e.g. dokku_app) sorts after every envelope
// key. Keys not in this list keep their relative order, appended after
// the canonical envelope keys but before the task-type key.
//
// Most envelope keys are reserved for activation in #205, #210, #211,
// #212; encoding the order today means recipes formatted now will still
// be canonical once those issues land.
var canonicalEnvelopeKeys = []string{
	"name",
	"tags",
	"when",
	"loop",
	"register",
	"changed_when",
	"failed_when",
	"ignore_errors",
}

// envelopeKeySet is canonicalEnvelopeKeys as a lookup set.
var envelopeKeySet = func() map[string]bool {
	m := make(map[string]bool, len(canonicalEnvelopeKeys))
	for _, k := range canonicalEnvelopeKeys {
		m[k] = true
	}
	return m
}()

// Format returns the canonical form of data. err is non-nil only on a
// yaml.v3 parse error or when the round-trip equivalence guard fails.
//
// The formatter:
//
//   - Uses yaml.v3's Node API so head/line/foot comments survive.
//   - Indents with 2 spaces.
//   - Reorders task envelope keys per canonicalEnvelopeKeys (task-type
//     key last) and play keys per canonicalPlayKeys.
//   - Inserts a blank line between top-level plays and between top-level
//     task entries inside a play's tasks: list.
//   - Strips trailing whitespace and forces LF line endings.
//   - Re-parses the canonical output and aborts unless the original and
//     canonical AST trees are structurally equivalent (defends against
//     yaml.v3 emitter edge cases with anchors / complex flow scalars).
func Format(data []byte) ([]byte, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	var root yaml.Node
	err := dec.Decode(&root)
	if err != nil && err != io.EOF {
		return nil, fmt.Errorf("yaml parse error: %w", err)
	}

	doc := documentBody(&root)
	if err == io.EOF || isEmptyDocument(doc) {
		// Empty or comment-only file: there is nothing to canonicalize.
		// Return the input untouched so `docket fmt` is a no-op and
		// `fmt --check` passes rather than failing the round-trip guard.
		return data, nil
	}

	// docket recipes are single-document. Reject a file with more than one
	// document rather than silently formatting the first and truncating the
	// rest. A trailing empty document (a bare `---`) is ignored.
	var extra yaml.Node
	if extraErr := dec.Decode(&extra); extraErr == nil {
		if !isEmptyDocument(documentBody(&extra)) {
			return nil, fmt.Errorf("fmt supports single-document recipes only; the file contains multiple YAML documents separated by ---")
		}
	} else if extraErr != io.EOF {
		return nil, fmt.Errorf("yaml parse error: %w", extraErr)
	}

	if doc.Kind == yaml.SequenceNode {
		canonicalizeRecipe(doc)
	}

	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&root); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("yaml encode error: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("yaml encode close error: %w", err)
	}

	out := postProcess(buf.Bytes())

	if hasDocumentMarker(data) && !hasDocumentMarker(out) {
		out = append([]byte("---\n"), out...)
	}

	// Round-trip equivalence guard: re-parse the canonical output and
	// require structural equivalence to the input. Catches yaml.v3
	// emitter edge cases before the caller writes anything to disk.
	var roundTrip yaml.Node
	if err := yaml.Unmarshal(out, &roundTrip); err != nil {
		return nil, fmt.Errorf("round-trip parse error: %w", err)
	}
	if !equivalentNodes(documentBody(&root), documentBody(&roundTrip)) {
		return nil, fmt.Errorf("round-trip equivalence check failed; refusing to write")
	}

	return out, nil
}

// canonicalizeRecipe reorders the keys inside each play mapping and each
// task entry mapping under the play's tasks: sequence.
func canonicalizeRecipe(recipe *yaml.Node) {
	for _, play := range recipe.Content {
		if play.Kind != yaml.MappingNode {
			continue
		}
		reorderMapping(play, canonicalPlayKeys)

		tasksNode := mappingValue(play, "tasks")
		if tasksNode == nil || tasksNode.Kind != yaml.SequenceNode {
			continue
		}
		for _, task := range tasksNode.Content {
			if task.Kind != yaml.MappingNode {
				continue
			}
			reorderTaskEnvelope(task)
		}
	}
}

// reorderMapping rebuilds node.Content so keys appear in the order given
// by priority, with any unrecognised keys appended in their original
// relative order.
func reorderMapping(node *yaml.Node, priority []string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	pairs := mappingPairs(node)
	out := make([]*yaml.Node, 0, safeCap(len(pairs), len(pairs)))
	used := make(map[int]bool, len(pairs))

	for _, key := range priority {
		for i, pair := range pairs {
			if used[i] {
				continue
			}
			if pair.key.Value == key {
				out = append(out, pair.key, pair.value)
				used[i] = true
				break
			}
		}
	}
	for i, pair := range pairs {
		if used[i] {
			continue
		}
		out = append(out, pair.key, pair.value)
	}
	node.Content = out
}

// reorderTaskEnvelope reorders a task entry mapping so the task-type key
// (the single non-envelope key) comes last, with envelope keys ahead in
// the order canonicalEnvelopeKeys defines.
func reorderTaskEnvelope(node *yaml.Node) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}
	pairs := mappingPairs(node)
	out := make([]*yaml.Node, 0, safeCap(len(pairs), len(pairs)))
	used := make(map[int]bool, len(pairs))

	for _, key := range canonicalEnvelopeKeys {
		for i, pair := range pairs {
			if used[i] {
				continue
			}
			if pair.key.Value == key {
				out = append(out, pair.key, pair.value)
				used[i] = true
				break
			}
		}
	}
	// Any remaining envelope-recognised keys (none today, but
	// future-proof) keep relative order before the task-type key.
	for i, pair := range pairs {
		if used[i] {
			continue
		}
		if envelopeKeySet[pair.key.Value] {
			out = append(out, pair.key, pair.value)
			used[i] = true
		}
	}
	// Everything else (the task-type key plus any unrecognised keys)
	// goes last, in original relative order.
	for i, pair := range pairs {
		if used[i] {
			continue
		}
		out = append(out, pair.key, pair.value)
	}
	node.Content = out
}

// mappingKV is a single (key, value) pair from a mapping node.
type mappingKV struct {
	key   *yaml.Node
	value *yaml.Node
}

// mappingPairs lifts a mapping's flat content slice into (key, value)
// pairs for easier reordering. yaml.v3 stores mappings as
// [k1, v1, k2, v2, ...].
func mappingPairs(node *yaml.Node) []mappingKV {
	pairs := make([]mappingKV, 0, len(node.Content)/2)
	for i := 0; i+1 < len(node.Content); i += 2 {
		pairs = append(pairs, mappingKV{key: node.Content[i], value: node.Content[i+1]})
	}
	return pairs
}

// postProcess applies the byte-level canonicalization rules yaml.v3's
// emitter does not control: trailing-whitespace strip, LF endings,
// blank lines between top-level plays, and blank lines between
// top-level task entries in each play.
func postProcess(data []byte) []byte {
	lines := strings.Split(string(data), "\n")
	for i, line := range lines {
		lines[i] = strings.TrimRight(line, " \t\r")
	}

	// Drop the trailing empty entry produced by Split when the input
	// ends with a newline; it is re-added below.
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	lines = insertBlankLinesBetweenSequenceEntries(lines)

	out := strings.Join(lines, "\n")
	out = strings.TrimRight(out, "\n") + "\n"
	return []byte(out)
}

// insertBlankLinesBetweenSequenceEntries walks the canonicalized lines
// and inserts a single blank line between top-level plays (sibling
// sequence entries at column 0) and between top-level task entries
// (sibling sequence entries at column 4) inside a play's tasks: block.
// yaml.v3's emitter does not preserve inter-entry blank lines, so we
// add them back here.
//
// yaml.v3 with SetIndent(2) emits the recipe sequence at column 0
// ("- "), the per-play tasks: key at column 2 ("  tasks:"), and each
// task entry at column 4 ("    - "). Anything deeper is a list inside
// a task body and is not blank-line separated.
func insertBlankLinesBetweenSequenceEntries(lines []string) []string {
	out := make([]string, 0, len(lines))
	seenTopDash := false
	seenTaskDash := false
	inTasksBlock := false

	for _, line := range lines {
		switch {
		case isCommentLine(line):
			// A comment line is emitted verbatim without touching the
			// trackers: it is the head comment of the entry that
			// follows, so the inter-entry blank line must land above
			// it (handled by spliceSeparatorBlank), and a top-level
			// comment must not suppress the next play's separator.
			out = append(out, line)
			continue
		case strings.HasPrefix(line, "- "):
			if seenTopDash {
				out = spliceSeparatorBlank(out, 0)
			}
			seenTopDash = true
			seenTaskDash = false
			// yaml.v3 inlines the first key of a sequence-element
			// mapping onto the dash line, so a play whose only key
			// is "tasks" emits as "- tasks:". Detect this so the
			// subsequent task entries get blank-line separation.
			inTasksBlock = line == "- tasks:"
			out = append(out, line)
			continue
		case line == "  tasks:":
			inTasksBlock = true
			seenTaskDash = false
			out = append(out, line)
			continue
		case strings.HasPrefix(line, "    - ") && inTasksBlock:
			if seenTaskDash {
				out = spliceSeparatorBlank(out, 4)
			}
			seenTaskDash = true
			out = append(out, line)
			continue
		case strings.HasPrefix(line, "  ") && !strings.HasPrefix(line, "   "):
			// A line at exactly 2-space indent that is not part of
			// the tasks: sequence (handled above) is a sibling key
			// of tasks within the play - it closes the tasks block.
			inTasksBlock = false
		case len(line) > 0 && !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "-"):
			// A top-level non-list line (like "---") resets the
			// "seen" trackers so a recipe with a leading document
			// marker still gets its blank-line behaviour.
			seenTopDash = false
			seenTaskDash = false
			inTasksBlock = false
		}
		out = append(out, line)
	}
	return out
}

// spliceSeparatorBlank inserts a single blank line into out to separate the
// previous sequence entry from the next one. The blank is placed above the
// contiguous run of the next entry's head-comment lines already appended to
// out, so a comment stays attached to the entry it documents.
//
// dashIndent is the column of the upcoming entry's "- " dash (0 for a
// top-level play, 4 for a task). Only comment lines whose "#" sits at
// exactly that column are treated as head comments; a more deeply indented
// "#" line is block-scalar content (e.g. a "# ..." line inside script: |),
// not a YAML comment, and must not be crossed - doing so would splice a
// blank line into the middle of the block scalar and corrupt it.
func spliceSeparatorBlank(out []string, dashIndent int) []string {
	at := len(out)
	for at > 0 && isCommentAtColumn(out[at-1], dashIndent) {
		at--
	}
	res := make([]string, 0, safeCap(len(out), 1))
	res = append(res, out[:at]...)
	res = append(res, "")
	res = append(res, out[at:]...)
	return res
}

// isCommentLine reports whether line, ignoring leading whitespace, is a YAML
// comment (or block-scalar content that happens to start with "#").
func isCommentLine(line string) bool {
	return strings.HasPrefix(strings.TrimLeft(line, " \t"), "#")
}

// isCommentAtColumn reports whether line is a comment whose "#" sits at
// exactly column col (col leading spaces followed immediately by "#").
func isCommentAtColumn(line string, col int) bool {
	if len(line) <= col {
		return false
	}
	for i := 0; i < col; i++ {
		if line[i] != ' ' {
			return false
		}
	}
	return line[col] == '#'
}

// equivalentNodes compares two YAML AST trees structurally. Mapping
// keys are compared as a set (canonicalisation reorders them by
// design); sequences are compared in order; scalars compare on Value
// (and Tag when set explicitly).
//
// Comments do not contribute to equivalence: they are preserved by the
// emitter but a comment-only edit to a node should not flag the round-
// trip guard.
func equivalentNodes(a, b *yaml.Node) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil {
		return false
	}
	if a.Kind != b.Kind {
		return false
	}
	if !tagsEquivalent(a, b) {
		return false
	}

	switch a.Kind {
	case yaml.ScalarNode:
		return a.Value == b.Value
	case yaml.SequenceNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !equivalentNodes(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	case yaml.MappingNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		bIdx := make(map[string]*yaml.Node, len(b.Content)/2)
		for i := 0; i+1 < len(b.Content); i += 2 {
			bIdx[b.Content[i].Value] = b.Content[i+1]
		}
		for i := 0; i+1 < len(a.Content); i += 2 {
			key := a.Content[i].Value
			bVal, ok := bIdx[key]
			if !ok {
				return false
			}
			if !equivalentNodes(a.Content[i+1], bVal) {
				return false
			}
		}
		return true
	case yaml.DocumentNode, yaml.AliasNode:
		if len(a.Content) != len(b.Content) {
			return false
		}
		for i := range a.Content {
			if !equivalentNodes(a.Content[i], b.Content[i]) {
				return false
			}
		}
		return true
	}
	return true
}

// isEmptyDocument reports whether a decoded document body carries nothing to
// format: either no body node at all (a truly empty stream) or a null scalar,
// which is what a comment-only file with an explicit `---` marker decodes to.
func isEmptyDocument(body *yaml.Node) bool {
	if body == nil {
		return true
	}
	return body.Kind == yaml.ScalarNode && body.Tag == "!!null"
}

// hasDocumentMarker reports whether the byte slice begins with a YAML
// document-start marker, optionally preceded by whitespace or a BOM.
// yaml.v3's encoder does not emit the marker by default, so a recipe
// that opened with "---" needs it re-prepended for round-trip parity.
func hasDocumentMarker(data []byte) bool {
	s := string(data)
	s = strings.TrimLeft(s, " \t\r\n\ufeff")
	return strings.HasPrefix(s, "---\n") || s == "---" || strings.HasPrefix(s, "---\r")
}

// tagsEquivalent returns true when two nodes' explicit tags are
// compatible. Empty / default tags compare equal to each other.
func tagsEquivalent(a, b *yaml.Node) bool {
	at, bt := a.Tag, b.Tag
	if at == "" || at == "!" {
		at = ""
	}
	if bt == "" || bt == "!" {
		bt = ""
	}
	return at == bt
}
