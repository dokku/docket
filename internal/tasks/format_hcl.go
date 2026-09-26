package tasks

import (
	"bytes"
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/hashicorp/hcl/v2/hclwrite"
	yaml "gopkg.in/yaml.v3"
)

// format_hcl.go writes the interchange tree back out as canonically formatted
// HCL, and is the other half of document_hcl.go, which reads it.
//
// The emitter writes plainly indented HCL and hands the bytes to
// hclwrite.Format for the last pass, which is what aligns a run of `=` signs.
// Borrowing that rather than reimplementing it means docket's HCL and
// `terraform fmt`'s agree about whitespace, which is the whole reason an HCL
// user would expect a formatter to exist.
//
// Everything above whitespace is docket's own: the canonical key order
// (canonicalPlayKeys / canonicalEnvelopeKeys, shared with the other two
// formats), the blank line before each nested block, and the choice between a
// heredoc and a quoted string.

// hclIndent is one level of indentation before hclwrite.Format normalises it.
const hclIndent = "  "

// hclSourceName is the filename HCL diagnostics are reported against. The
// caller re-words them with the real path, so this only has to be stable.
const hclSourceName = "recipe.hcl"

// hclHeredocDelimiter is the marker canonical output opens a heredoc with.
const hclHeredocDelimiter = "EOT"

// FormatHCL returns the canonical form of an HCL recipe.
//
// It is Format's shape for the third syntax: decode to the interchange tree,
// write it back out, then re-read the result and refuse to return it unless
// the two trees agree - so an emitter bug can never corrupt a recipe. An empty
// or comment-only file comes back untouched, as it does in YAML.
func FormatHCL(data []byte) ([]byte, error) {
	if err := refuseHCLUnportableQuoting(data); err != nil {
		return nil, err
	}
	doc, err := hclDocumentToYAML(data, hclSourceName, true)
	if err != nil {
		return nil, err
	}
	if doc == nil {
		return data, nil
	}

	out, err := yamlDocumentToHCL(doc)
	if err != nil {
		return nil, err
	}

	back, err := hclDocumentToYAML(out, hclSourceName, true)
	if err != nil {
		return nil, fmt.Errorf("formatted hcl failed to parse back: %w", err)
	}
	if !equivalentNodes(documentBody(doc), documentBody(back)) {
		return nil, fmt.Errorf("hcl formatting changed the recipe; refusing to write")
	}
	return out, nil
}

// yamlDocumentToHCL renders the interchange tree as canonical HCL.
func yamlDocumentToHCL(doc *yaml.Node) ([]byte, error) {
	body := documentBody(doc)
	if body == nil {
		return nil, fmt.Errorf("no document to write as hcl")
	}
	if body.Kind != yaml.SequenceNode {
		return nil, fmt.Errorf("a recipe must be a list of plays to be written as hcl")
	}
	if len(body.Content) == 0 {
		// An empty file is how HCL spells a body with nothing in it, and
		// FormatHCL reads one back as "no document" rather than as an empty
		// recipe - so writing one would not survive the round-trip guard.
		return nil, fmt.Errorf("an empty recipe has no hcl spelling; hcl cannot tell one from an empty file")
	}

	// Canonicalise a copy: Convert compares the tree it handed over against
	// the one it reads back, and reordering the caller's nodes underneath it
	// would be a surprising thing for an encoder to do.
	root := deepCopyNode(body)
	canonicalizeRecipe(root)

	e := &hclEmitter{}
	e.comment(0, root.HeadComment)
	for i, play := range root.Content {
		if i > 0 {
			e.blank()
		}
		if err := e.writePlay(play, 0); err != nil {
			return nil, err
		}
	}
	// A comment after the last play sits on the document node when yaml.v3
	// parsed it and on the recipe when another codec built the tree, so both
	// are read.
	e.comment(0, joinComments(root.FootComment, doc.FootComment))

	return hclwrite.Format(e.buf.Bytes()), nil
}

// hclEmitter accumulates the recipe text.
type hclEmitter struct {
	buf bytes.Buffer
}

// line writes one indented line.
func (e *hclEmitter) line(indent int, text string) {
	e.buf.WriteString(strings.Repeat(hclIndent, indent))
	e.buf.WriteString(text)
	e.buf.WriteByte('\n')
}

func (e *hclEmitter) blank() { e.buf.WriteByte('\n') }

// comment writes a yaml.v3 comment string as HCL comment lines.
func (e *hclEmitter) comment(indent int, comment string) {
	for _, line := range yamlCommentToHCL(comment) {
		e.line(indent, line)
	}
}

// writePlay writes one `play` block.
func (e *hclEmitter) writePlay(play *yaml.Node, indent int) error {
	if play.Kind != yaml.MappingNode {
		return fmt.Errorf("a play must be a mapping to be written as hcl; found %s at line %d",
			yamlKindName(play.Kind), play.Line)
	}
	folded := foldHCLName(mappingPairs(play))
	e.comment(indent, joinComments(play.HeadComment, folded.head))
	if err := e.writeBlockHeader(indent, hclPlayBlockType, folded.label, firstNonEmpty(play.LineComment, folded.line)); err != nil {
		return err
	}

	written := 0
	for _, pair := range folded.rest {
		switch {
		case pair.key.Value == hclInputsKey && isHCLBlockSequence(pair.value):
			if err := e.writeEntrySequence(pair, indent+1, &written, func(entry *yaml.Node, at int) error {
				return e.writeInput(entry, at)
			}); err != nil {
				return err
			}
		case pair.key.Value == hclTasksKey && isHCLBlockSequence(pair.value):
			if err := e.writeEntrySequence(pair, indent+1, &written, func(entry *yaml.Node, at int) error {
				return e.writeTaskEntry(entry, at)
			}); err != nil {
				return err
			}
		default:
			if err := e.writeAttribute(pair, indent+1); err != nil {
				return err
			}
			written++
		}
	}

	e.comment(indent+1, play.FootComment)
	e.line(indent, "}")
	return nil
}

// writeEntrySequence writes each element of an `inputs:` or `tasks:` sequence
// as its own block, with the key's own comment above the first of them and a
// blank line before every block that is not the first thing in the body.
func (e *hclEmitter) writeEntrySequence(pair mappingKV, indent int, written *int, write func(*yaml.Node, int) error) error {
	head := joinComments(pair.key.HeadComment, pair.key.LineComment)
	for _, entry := range pair.value.Content {
		if *written > 0 {
			e.blank()
		}
		e.comment(indent, head)
		head = ""
		if err := write(entry, indent); err != nil {
			return err
		}
		*written++
	}
	return nil
}

// writeInput writes one `input` block.
func (e *hclEmitter) writeInput(input *yaml.Node, indent int) error {
	folded := foldHCLName(mappingPairs(input))
	e.comment(indent, joinComments(input.HeadComment, folded.head))
	if err := e.writeBlockHeader(indent, hclInputBlockType, folded.label, firstNonEmpty(input.LineComment, folded.line)); err != nil {
		return err
	}
	for _, pair := range folded.rest {
		if err := e.writeAttribute(pair, indent+1); err != nil {
			return err
		}
	}
	e.comment(indent+1, input.FootComment)
	e.line(indent, "}")
	return nil
}

// writeTaskEntry writes one task entry, in whichever of the two spellings can
// carry it.
func (e *hclEmitter) writeTaskEntry(entry *yaml.Node, indent int) error {
	if entry.Kind != yaml.MappingNode {
		return fmt.Errorf("a task entry must be a mapping to be written as hcl; found %s at line %d",
			yamlKindName(entry.Kind), entry.Line)
	}
	folded := foldHCLName(mappingPairs(entry))
	e.comment(indent, joinComments(entry.HeadComment, folded.head))
	lineComment := firstNonEmpty(entry.LineComment, folded.line)

	if shorthand, ok := hclShorthandTask(folded); ok {
		if err := e.writeBlockHeader(indent, shorthand.typeKey.Value, folded.label, lineComment); err != nil {
			return err
		}
		for _, pair := range shorthand.envelope {
			if err := e.writeAttribute(pair, indent+1); err != nil {
				return err
			}
		}
		// A comment written about the task-type key goes immediately above
		// the first of the task's own fields, which is where decodeTaskBlock
		// looks for it. Emitting it right after the header instead would put
		// it above an envelope attribute, where the next read would take it
		// for that attribute's and the round trip would not converge.
		e.comment(indent+1, joinComments(shorthand.typeKey.HeadComment, shorthand.typeKey.LineComment))
		for _, pair := range mappingPairs(shorthand.body) {
			if err := e.writeAttribute(pair, indent+1); err != nil {
				return err
			}
		}
		e.comment(indent+1, joinComments(shorthand.body.FootComment, entry.FootComment))
		e.line(indent, "}")
		return nil
	}

	if err := e.writeBlockHeader(indent, hclTaskBlockType, folded.label, lineComment); err != nil {
		return err
	}
	written := 0
	for _, pair := range folded.rest {
		if hclGroupClauseSet[pair.key.Value] && isHCLBlockSequence(pair.value) {
			if written > 0 {
				e.blank()
			}
			if err := e.writeGroupClause(pair, indent+1); err != nil {
				return err
			}
			written++
			continue
		}
		if err := e.writeAttribute(pair, indent+1); err != nil {
			return err
		}
		written++
	}
	e.comment(indent+1, entry.FootComment)
	e.line(indent, "}")
	return nil
}

// writeGroupClause writes a `block` / `rescue` / `always` block, whose body is
// the clause's task entries rather than a value.
func (e *hclEmitter) writeGroupClause(pair mappingKV, indent int) error {
	e.comment(indent, joinComments(pair.key.HeadComment, pair.key.LineComment))
	e.line(indent, pair.key.Value+" {")
	for i, entry := range pair.value.Content {
		if i > 0 {
			e.blank()
		}
		if err := e.writeTaskEntry(entry, indent+1); err != nil {
			return err
		}
	}
	e.comment(indent+1, pair.value.FootComment)
	e.line(indent, "}")
	return nil
}

// writeBlockHeader writes `type "label" {`, with a blank line before it when
// the caller asked for one.
func (e *hclEmitter) writeBlockHeader(indent int, blockType string, label *string, lineComment string) error {
	if !hclsyntax.ValidIdentifier(blockType) {
		return fmt.Errorf("%q is not a valid hcl block type; an hcl recipe cannot spell it", blockType)
	}
	header := blockType
	if label != nil {
		header += " " + quoteHCLString(*label)
	}
	header += " {"
	if line := flattenComment(lineComment); line != "" {
		header += " " + hclCommentLine(line)
	}
	e.line(indent, header)
	return nil
}

// writeAttribute writes `key = value`.
func (e *hclEmitter) writeAttribute(pair mappingKV, indent int) error {
	if pair.key.Kind != yaml.ScalarNode {
		return fmt.Errorf("mapping key at line %d is not a scalar; hcl attribute names are identifiers", pair.key.Line)
	}
	name := pair.key.Value
	if !hclsyntax.ValidIdentifier(name) {
		return fmt.Errorf("mapping key %q at line %d is not a valid hcl identifier, so it cannot be written as an attribute name",
			name, pair.key.Line)
	}
	e.comment(indent, pair.key.HeadComment)

	value, err := e.expression(pair.value, indent)
	if err != nil {
		return err
	}
	text := name + " = " + value
	if line := flattenComment(joinComments(pair.key.LineComment, pair.value.LineComment)); line != "" {
		text += " " + hclCommentLine(line)
	}
	e.line(indent, text)
	// A body's trailing comment hangs off the last key of the mapping it
	// closes, which is where yamlFootAnchor put it and where yaml.v3 puts one
	// it parsed itself. Writing it after the attribute is the mirror.
	e.comment(indent, pair.key.FootComment)
	return nil
}

// expression renders one value.
//
// A heredoc is the single case whose text escapes the indentation the rest of
// the emitter keeps, because an unindented `<<EOT` body is the only spelling
// that carries a multi-line value back byte for byte - HCL's indented `<<-`
// form strips the smallest indent it finds, which a value whose every line is
// already indented would not survive.
func (e *hclEmitter) expression(node *yaml.Node, indent int) (string, error) {
	switch node.Kind {
	case yaml.ScalarNode:
		return yamlScalarToHCLRaw(node)
	case yaml.SequenceNode:
		return e.tuple(node, indent)
	case yaml.MappingNode:
		return e.object(node, indent)
	case yaml.AliasNode:
		return "", fmt.Errorf("unresolved alias at line %d; hcl has no anchors", node.Line)
	}
	return "", fmt.Errorf("value at line %d has no hcl representation", node.Line)
}

// tuple renders a sequence, inline when every element is a plain scalar with
// nothing to say, and one element per line otherwise.
func (e *hclEmitter) tuple(node *yaml.Node, indent int) (string, error) {
	if len(node.Content) == 0 && node.FootComment == "" {
		return "[]", nil
	}
	if hclInlineSequence(node) {
		parts := make([]string, 0, len(node.Content))
		for _, item := range node.Content {
			text, err := yamlScalarToHCLRaw(item)
			if err != nil {
				return "", err
			}
			parts = append(parts, text)
		}
		return "[" + strings.Join(parts, ", ") + "]", nil
	}

	var buf bytes.Buffer
	buf.WriteString("[\n")
	inner := strings.Repeat(hclIndent, indent+1)
	for _, item := range node.Content {
		for _, line := range yamlCommentToHCL(item.HeadComment) {
			buf.WriteString(inner + line + "\n")
		}
		text, err := e.expression(item, indent+1)
		if err != nil {
			return "", err
		}
		buf.WriteString(inner + text + ",")
		if line := flattenComment(item.LineComment); line != "" {
			buf.WriteString(" " + hclCommentLine(line))
		}
		buf.WriteString("\n")
		// The collection's trailing comment hangs off its last element, the
		// same way a body's hangs off its last key; see yamlFootAnchor.
		for _, line := range yamlCommentToHCL(item.FootComment) {
			buf.WriteString(inner + line + "\n")
		}
	}
	for _, line := range yamlCommentToHCL(node.FootComment) {
		buf.WriteString(inner + line + "\n")
	}
	buf.WriteString(strings.Repeat(hclIndent, indent) + "]")
	return buf.String(), nil
}

// object renders a mapping as an HCL object expression, one key per line.
//
// A nested mapping is always an object rather than a nested block: a block
// inside a task body would be indistinguishable from the task-type block of
// the general form, and this way every depth below the recipe's own structure
// has exactly one spelling.
func (e *hclEmitter) object(node *yaml.Node, indent int) (string, error) {
	if len(node.Content) == 0 && node.FootComment == "" {
		return "{}", nil
	}
	var buf bytes.Buffer
	buf.WriteString("{\n")
	inner := strings.Repeat(hclIndent, indent+1)
	for _, pair := range mappingPairs(node) {
		key, err := hclObjectKey(pair.key)
		if err != nil {
			return "", err
		}
		for _, line := range yamlCommentToHCL(pair.key.HeadComment) {
			buf.WriteString(inner + line + "\n")
		}
		value, err := e.expression(pair.value, indent+1)
		if err != nil {
			return "", err
		}
		buf.WriteString(inner + key + " = " + value)
		if line := flattenComment(joinComments(pair.key.LineComment, pair.value.LineComment)); line != "" {
			buf.WriteString(" " + hclCommentLine(line))
		}
		buf.WriteString("\n")
		for _, line := range yamlCommentToHCL(pair.key.FootComment) {
			buf.WriteString(inner + line + "\n")
		}
	}
	for _, line := range yamlCommentToHCL(node.FootComment) {
		buf.WriteString(inner + line + "\n")
	}
	buf.WriteString(strings.Repeat(hclIndent, indent) + "}")
	return buf.String(), nil
}

// hclInlineSequence reports whether a sequence fits on one line: every element
// a scalar that is not a heredoc, and no comments anywhere to lose.
func hclInlineSequence(node *yaml.Node) bool {
	if node.FootComment != "" {
		return false
	}
	for _, item := range node.Content {
		if item.Kind != yaml.ScalarNode {
			return false
		}
		if item.HeadComment != "" || item.LineComment != "" || item.FootComment != "" {
			return false
		}
		if hclWantsHeredoc(item) {
			return false
		}
	}
	return true
}

// hclFoldedName is a mapping split into the label its `name` key becomes and
// everything else.
type hclFoldedName struct {
	label *string
	head  string
	line  string
	rest  []mappingKV
}

// foldHCLName lifts a plain-string `name` out of a mapping so it can be
// written as the block's label.
//
// The key's own comments come with it, since the key itself is about to stop
// existing; they are merged into the block's head and trailing comments by the
// caller. A `name` that is not a plain string stays where it is, and the
// caller falls back to a spelling that can hold it.
func foldHCLName(pairs []mappingKV) hclFoldedName {
	out := hclFoldedName{rest: make([]mappingKV, 0, len(pairs))}
	for _, pair := range pairs {
		if out.label == nil && pair.key.Value == hclNameKey && isHCLLabelScalar(pair.value) {
			label := pair.value.Value
			out.label = &label
			out.head = joinComments(pair.key.HeadComment, pair.value.HeadComment)
			out.line = joinComments(pair.key.LineComment, pair.value.LineComment)
			continue
		}
		out.rest = append(out.rest, pair)
	}
	return out
}

// isHCLLabelScalar reports whether a value can be written as a block label.
func isHCLLabelScalar(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.ScalarNode {
		return false
	}
	switch node.Tag {
	case "", "!!str", "!!timestamp", "!!binary":
		return !strings.ContainsAny(node.Value, "\n\r")
	}
	return false
}

// hclShorthand is the shorthand spelling of a task entry, once it has been
// established that one exists.
type hclShorthand struct {
	typeKey  *yaml.Node
	body     *yaml.Node
	envelope []mappingKV
}

// hclShorthandTask decides whether a task entry can be written as
// `<task_type> "label" { … }`, and splits it up when it can.
//
// Every condition is about a spelling the shorthand would make ambiguous
// rather than about whether the recipe is valid - `fmt` has to round-trip
// recipes it would never have written:
//
//   - exactly one key that is not part of the envelope, since the block type
//     names it and there is only one block type;
//   - a mapping for its value, because the body of a block is a mapping and
//     `dokku_app:` with a null body would silently widen to an empty one;
//   - a block type HCL can spell, and one a play body does not already read as
//     something else;
//   - an envelope `name` that became the label, because a `name` left in the
//     body is read back as the task's own field;
//   - and a task body that shares no key with the envelope, because the two
//     are merged into one body and would be indistinguishable.
func hclShorthandTask(folded hclFoldedName) (hclShorthand, bool) {
	var out hclShorthand
	found := 0
	for _, pair := range folded.rest {
		if pair.key.Value == hclNameKey || hclEnvelopeAttrSet[pair.key.Value] {
			out.envelope = append(out.envelope, pair)
			continue
		}
		found++
		out.typeKey, out.body = pair.key, pair.value
	}
	if found != 1 || out.body == nil || out.body.Kind != yaml.MappingNode {
		return hclShorthand{}, false
	}
	if out.typeKey.Kind != yaml.ScalarNode || !hclsyntax.ValidIdentifier(out.typeKey.Value) {
		return hclShorthand{}, false
	}
	if hclReservedPlayBlocks[out.typeKey.Value] || out.typeKey.Value == hclPlayBlockType {
		return hclShorthand{}, false
	}
	for _, pair := range out.envelope {
		if pair.key.Value == hclNameKey {
			return hclShorthand{}, false
		}
	}
	for _, pair := range mappingPairs(out.body) {
		if pair.key.Kind != yaml.ScalarNode || hclEnvelopeAttrSet[pair.key.Value] {
			return hclShorthand{}, false
		}
		if !hclsyntax.ValidIdentifier(pair.key.Value) {
			return hclShorthand{}, false
		}
	}
	return out, true
}

// isHCLBlockSequence reports whether a value is a sequence of mappings, which
// is the shape HCL writes as repeated blocks.
func isHCLBlockSequence(node *yaml.Node) bool {
	if node == nil || node.Kind != yaml.SequenceNode || len(node.Content) == 0 {
		return false
	}
	for _, item := range node.Content {
		if item.Kind != yaml.MappingNode {
			return false
		}
	}
	return true
}

// yamlScalarToHCLRaw renders a YAML scalar as an HCL literal.
//
// Dispatch is on the tag and never on the style, for the reason
// yamlScalarToJSON5Raw gives: yaml.v3 resolves any quoted or block scalar to
// !!str, so the tag already answers what a style inspection would be asking.
func yamlScalarToHCLRaw(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("value at line %d is not a scalar", n.Line)
	}
	switch n.Tag {
	case "", "!!str", "!!timestamp", "!!binary":
		if hclWantsHeredoc(n) {
			return hclHeredoc(n.Value), nil
		}
		return quoteHCLString(n.Value), nil
	case "!!null":
		return "null", nil
	case "!!bool":
		var b bool
		if err := n.Decode(&b); err != nil {
			return "", fmt.Errorf("invalid boolean %q: %w", n.Value, err)
		}
		return strconv.FormatBool(b), nil
	case "!!int":
		// Decoded through yaml.v3 rather than re-lexed here, so `fmt` and the
		// loader cannot disagree about which integer 0o17 or 1_000 is - HCL
		// accepts neither spelling and the resolver is the only authority.
		var i int64
		if err := n.Decode(&i); err == nil {
			return strconv.FormatInt(i, 10), nil
		}
		var u uint64
		if err := n.Decode(&u); err == nil {
			return strconv.FormatUint(u, 10), nil
		}
		return "", fmt.Errorf("integer %q is out of range for hcl", n.Value)
	case "!!float":
		var f float64
		if err := n.Decode(&f); err != nil {
			return "", fmt.Errorf("invalid float %q: %w", n.Value, err)
		}
		if math.IsInf(f, 0) || math.IsNaN(f) {
			// JSON5 spells these Infinity and NaN; HCL has no literal for
			// either, and inventing a string would change the value's type.
			return "", fmt.Errorf("%q has no hcl representation", n.Value)
		}
		return strconv.FormatFloat(f, 'g', -1, 64), nil
	}
	return "", fmt.Errorf("YAML tag %s has no hcl representation", n.Tag)
}

// hclObjectKey renders a mapping key as an HCL object key, quoted when it is
// not an identifier. Object keys may be quoted, unlike attribute names, so
// this never has to refuse a key an attribute name would.
func hclObjectKey(n *yaml.Node) (string, error) {
	if n.Kind != yaml.ScalarNode {
		return "", fmt.Errorf("mapping key at line %d is not a scalar; hcl object keys must be strings", n.Line)
	}
	switch n.Tag {
	case "", "!!str", "!!timestamp", "!!binary":
		if hclsyntax.ValidIdentifier(n.Value) {
			return n.Value, nil
		}
		return quoteHCLString(n.Value), nil
	}
	raw, err := yamlScalarToHCLRaw(n)
	if err != nil {
		return "", err
	}
	return quoteHCLString(raw), nil
}

// quoteHCLString renders s as a complete HCL quoted string.
func quoteHCLString(s string) string {
	quoted, err := HCLScalar(s)
	if err != nil {
		// HCLScalar only fails when the JSON encoder does, which it does not
		// for a string; falling back keeps this total rather than adding an
		// error return every caller would have to thread.
		return `"` + escapeHCLTemplateSequences(strings.ReplaceAll(s, `"`, `\"`)) + `"`
	}
	return quoted
}

// hclWantsHeredoc reports whether a string is written as a heredoc rather than
// a quoted scalar.
//
// The rule is read off the value, not off how the source spelled it, which is
// what makes the choice idempotent: a heredoc's value always ends in a
// newline, so a heredoc decodes to a value that is written back as a heredoc.
//
// Any substitution at all disqualifies one, because a heredoc processes no
// backslash escapes: `| dq` inside one would land as literal backslashes, so
// neither the escaped nor the unescaped spelling belongs there.
// refuseHCLUnportableQuoting stops an UNESCAPED one reaching here from a
// source that had written it out; an escaped one is quietly moved into a
// quoted scalar, where `dq` means what it says.
func hclWantsHeredoc(n *yaml.Node) bool {
	switch n.Tag {
	case "", "!!str", "!!timestamp", "!!binary":
	default:
		return false
	}
	s := n.Value
	if !strings.Contains(s, "\n") || !strings.HasSuffix(s, "\n") {
		return false
	}
	if strings.ContainsAny(s, "\r") || substitutesAnyValue(s) {
		return false
	}
	for _, line := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if strings.TrimSpace(line) == hclHeredocDelimiter {
			return false
		}
		if line != strings.TrimRight(line, " \t") {
			// hclwrite.Format leaves a heredoc alone, but a line whose
			// trailing spaces are invisible is exactly the kind of value a
			// formatter should not be trusted to preserve.
			return false
		}
		for _, r := range line {
			if r < 0x20 && r != '\t' {
				return false
			}
		}
	}
	return true
}

// hclHeredoc renders a multi-line string as a heredoc. The body sits at column
// zero because `<<EOT` takes its content literally.
func hclHeredoc(s string) string {
	var buf strings.Builder
	buf.WriteString("<<" + hclHeredocDelimiter + "\n")
	buf.WriteString(escapeHCLTemplateSequences(s))
	buf.WriteString(hclHeredocDelimiter)
	return buf.String()
}

// refuseHCLUnportableQuoting rejects an HCL recipe whose canonical form would
// change how an interpolation is escaped.
//
// A heredoc is the case: canonical HCL writes a string holding an
// interpolation as a quoted scalar, and a heredoc tolerates a double quote in
// the substituted value where a quoted scalar needs `| dq`. It is the same
// refusal FormatJSON5 makes over a single-quoted string, for the same reason -
// see interpolation.go.
func refuseHCLUnportableQuoting(data []byte) error {
	sites, err := hclQuotingSites(data)
	if err != nil || len(sites) == 0 {
		// A parse failure is not this check's to report; the caller parses
		// next and says so properly.
		return nil
	}
	return unportableQuotingError(sites)
}

// yamlKindName names a node kind for an error message.
func yamlKindName(kind yaml.Kind) string {
	switch kind {
	case yaml.ScalarNode:
		return "a scalar"
	case yaml.SequenceNode:
		return "a list"
	case yaml.MappingNode:
		return "a mapping"
	case yaml.AliasNode:
		return "an alias"
	case yaml.DocumentNode:
		return "a document"
	}
	return "a value"
}

// firstNonEmpty returns the first non-blank string.
func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// hclSniff reports whether data opens the way an HCL body does: an identifier
// followed by `=`, by `{`, or by the quoted labels of a block header.
//
// Comments are skipped first, so a recipe `docket fmt` wrote - which comments
// with `#` precisely so this works - is recognised from stdin. It is
// deliberately narrow: YAML opens with `-`, `---` or `key:`, and JSON5 with
// `[`, `{` or a comment, none of which can reach the second token here.
func hclSniff(data []byte) bool {
	tokens, _ := hclsyntax.LexConfig(data, hclSourceName, hcl.InitialPos)
	seenIdent := false
	for _, token := range tokens {
		switch token.Type {
		case hclsyntax.TokenComment, hclsyntax.TokenNewline:
			continue
		case hclsyntax.TokenIdent:
			if seenIdent {
				return false
			}
			seenIdent = true
		case hclsyntax.TokenEqual, hclsyntax.TokenOBrace, hclsyntax.TokenOQuote:
			return seenIdent
		default:
			return false
		}
	}
	return false
}
