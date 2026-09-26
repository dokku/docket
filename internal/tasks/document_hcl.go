package tasks

import (
	"fmt"
	"math/big"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"
	yaml "gopkg.in/yaml.v3"
)

// document_hcl.go is the HCL half of the interchange: HCL source in one
// direction, the comment-carrying yaml.Node tree every cross-format
// conversion passes through in the other. It is the analogue of
// document_json5.go, and it is where the shape of an HCL recipe is decided.
//
// HCL is a language of blocks and attributes rather than of nested
// collections, so unlike YAML and JSON5 there is no mechanical mapping from
// its syntax to a tree. The one this file implements reads:
//
//	play "api" {
//	  tags = ["web"]
//
//	  input "app" {
//	    default = "api"
//	  }
//
//	  dokku_app "create the app" {
//	    when = "env != \"preview\""
//	    app  = "{{ .app | dq }}"
//	  }
//	}
//
// A block's label is the `name` key of the mapping it produces, which is what
// makes the task spelling work: ten registered task types already declare a
// `name` FIELD, so a task block's body could not hold both that and the
// envelope's `name`. Moving the envelope name out to the label leaves the body
// unambiguous - a `name` attribute in a task block is always the task's own.
//
// Two spellings exist for a task entry. The shorthand above names the task
// type as the block type; the general form
//
//	task "deploy" {
//	  block {
//	    dokku_app { app = "a" }
//	  }
//	}
//
// carries everything the shorthand cannot: a group entry, an entry with no
// single task-type key, a task-type key whose value is not a mapping, a
// non-string name, and any entry whose task body would collide with an
// envelope key. The general form is what makes the encoder total, which `fmt`
// requires - it is not a validator, and has to round-trip any recipe that
// parses.

// The block types this mapping reserves, and the keys they stand in for.
const (
	hclPlayBlockType  = "play"
	hclInputBlockType = "input"
	hclTaskBlockType  = "task"

	hclNameKey   = "name"
	hclInputsKey = "inputs"
	hclTasksKey  = "tasks"
)

// hclGroupClauseSet is the try/catch/finally clauses, each of which holds a
// list of task entries rather than a value (#211).
var hclGroupClauseSet = func() map[string]bool {
	m := make(map[string]bool, len(groupClauseKeys))
	for _, k := range groupClauseKeys {
		m[k] = true
	}
	return m
}()

// hclEnvelopeAttrSet is every key that reads as part of the envelope when it
// appears as an attribute in a task block's body - so, every envelope key
// except `name`, which the label carries.
//
// A task field sharing one of these names has no unambiguous shorthand
// spelling, which is why the encoder checks the body against this set before
// choosing one. TestCodecConformance asserts no registered task declares such
// a field today.
var hclEnvelopeAttrSet = func() map[string]bool {
	m := make(map[string]bool, len(canonicalEnvelopeKeys)+len(hclGroupClauseSet))
	for _, k := range canonicalEnvelopeKeys {
		if k == hclNameKey {
			continue
		}
		m[k] = true
	}
	for k := range hclGroupClauseSet {
		m[k] = true
	}
	return m
}()

// hclReservedPlayBlocks are the block types inside a play body that mean
// something other than "a task of this type".
var hclReservedPlayBlocks = map[string]bool{
	hclInputBlockType: true,
	hclTaskBlockType:  true,
}

// hclDocumentToYAML parses src and walks it into the interchange tree.
//
// comments selects whether the lexer's comment tokens are carried over.
// DecodeDocument wants them; ToYAML, which only feeds the loader, does not,
// and both go through this one walk so the two can never come to disagree
// about what a block means.
//
// A nil node with a nil error means src holds no document at all - an empty or
// comment-only file, which FormatHCL returns untouched, following YAML rather
// than JSON5's refusal.
func hclDocumentToYAML(src []byte, filename string, comments bool) (*yaml.Node, error) {
	file, diags := hclsyntax.ParseConfig(src, filename, hcl.InitialPos)
	if diags.HasErrors() {
		return nil, hclDiagnosticError(diags)
	}
	body, ok := file.Body.(*hclsyntax.Body)
	if !ok {
		return nil, fmt.Errorf("hcl parse error: unexpected body type %T", file.Body)
	}
	if len(body.Attributes) == 0 && len(body.Blocks) == 0 {
		return nil, nil
	}

	dec := &hclDecoder{src: src}
	if comments {
		dec.comments = newHCLCommentCursor(src, filename)
	}

	recipe, foot, err := dec.decodeRecipe(body)
	if err != nil {
		return nil, err
	}
	// A comment after the last play is a comment on the file, which yaml.v3
	// keeps on the document node rather than on the recipe.
	return &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{recipe}, FootComment: foot}, nil
}

// hclDecoder walks one parsed file. It holds the source, so a string's
// spelling can be read back off it, and the comment cursor, which is nil when
// comments are being discarded.
type hclDecoder struct {
	src      []byte
	comments *hclCommentCursor
}

// decodeRecipe turns the root body's play blocks into the recipe sequence.
//
// The root is the one body with no attribute form: a recipe is a list of
// plays, and HCL spells a repeated element as a repeated block.
func (d *hclDecoder) decodeRecipe(body *hclsyntax.Body) (*yaml.Node, string, error) {
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	foot, err := d.walkBody(body, len(d.src), func(item hclBodyItem) error {
		if item.attr != nil {
			return hclErrorAt(item.attr.SrcRange,
				"%q is an attribute at the top level; an hcl recipe is a list of %q blocks",
				item.attr.Name, hclPlayBlockType)
		}
		if item.block.Type != hclPlayBlockType {
			return hclErrorAt(item.block.TypeRange,
				"unexpected %q block at the top level; an hcl recipe is a list of %q blocks",
				item.block.Type, hclPlayBlockType)
		}
		play, err := d.decodePlay(item.block, item.line)
		if err != nil {
			return err
		}
		d.attachHead(play, item.head)
		seq.Content = append(seq.Content, play)
		return nil
	})
	if err != nil {
		return nil, "", err
	}
	return seq, foot, nil
}

// decodePlay walks one play block into its mapping.
func (d *hclDecoder) decodePlay(block *hclsyntax.Block, lineComment string) (*yaml.Node, error) {
	if len(block.Labels) > 1 {
		return nil, hclErrorAt(block.TypeRange, "a %q block takes at most one label", block.Type)
	}
	play := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	d.appendLabel(play, block, lineComment)

	var inputs, tasks *yaml.Node
	foot, err := d.walkBody(block.Body, hclBodyClose(block.Body), func(item hclBodyItem) error {
		switch {
		case item.block != nil && item.block.Type == hclInputBlockType:
			input, err := d.decodeMappingBlock(item.block, item.line)
			if err != nil {
				return err
			}
			d.attachHead(input, item.head)
			if inputs == nil {
				inputs = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
				appendMapping(play, hclInputsKey, inputs)
			}
			inputs.Content = append(inputs.Content, input)
		case item.block != nil:
			entry, err := d.decodeTaskBlock(item.block, item.line)
			if err != nil {
				return err
			}
			d.attachHead(entry, item.head)
			if tasks == nil {
				tasks = &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
				appendMapping(play, hclTasksKey, tasks)
			}
			tasks.Content = append(tasks.Content, entry)
		default:
			return d.appendAttribute(play, item)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	d.attachFoot(play, foot)
	return play, nil
}

// decodeMappingBlock walks a block whose body is a plain mapping - an `input`,
// or the task-type block inside a general-form task entry.
func (d *hclDecoder) decodeMappingBlock(block *hclsyntax.Block, lineComment string) (*yaml.Node, error) {
	if len(block.Labels) > 1 {
		return nil, hclErrorAt(block.TypeRange, "a %q block takes at most one label", block.Type)
	}
	node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	d.appendLabel(node, block, lineComment)
	foot, err := d.walkBody(block.Body, hclBodyClose(block.Body), func(item hclBodyItem) error {
		if item.block != nil {
			nested, err := d.decodeMappingBlock(item.block, item.line)
			if err != nil {
				return err
			}
			appendMappingWithComments(node, item.block.Type, nested, item.head, "")
			return nil
		}
		return d.appendAttribute(node, item)
	})
	if err != nil {
		return nil, err
	}
	d.attachFoot(node, foot)
	return node, nil
}

// decodeTaskBlock walks a task entry in either spelling.
//
// The block type decides which: `task` is the general form, where every
// attribute is a key of the entry itself, and anything else is the shorthand,
// where the block type is the task-type key and the body holds the task's
// fields alongside the envelope keys other than `name`.
func (d *hclDecoder) decodeTaskBlock(block *hclsyntax.Block, lineComment string) (*yaml.Node, error) {
	if len(block.Labels) > 1 {
		return nil, hclErrorAt(block.TypeRange, "a %q block takes at most one label", block.Type)
	}
	entry := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	d.appendLabel(entry, block, lineComment)

	general := block.Type == hclTaskBlockType
	taskBody := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
	var typeHead string

	foot, err := d.walkBody(block.Body, hclBodyClose(block.Body), func(item hclBodyItem) error {
		if item.block != nil {
			switch {
			case hclGroupClauseSet[item.block.Type]:
				clause, err := d.decodeGroupClause(item.block)
				if err != nil {
					return err
				}
				appendMappingWithComments(entry, item.block.Type, clause, item.head, item.line)
				return nil
			case general:
				nested, err := d.decodeMappingBlock(item.block, item.line)
				if err != nil {
					return err
				}
				appendMappingWithComments(entry, item.block.Type, nested, item.head, "")
				return nil
			}
			return hclErrorAt(item.block.TypeRange, "a %q block cannot hold a nested %q block",
				block.Type, item.block.Type)
		}
		if general || hclEnvelopeAttrSet[item.attr.Name] {
			return d.appendAttribute(entry, item)
		}
		if len(taskBody.Content) == 0 {
			// The first field's head comment sat directly under the block
			// header, where the shorthand puts a comment written about the
			// task-type key itself. Lifting it back onto that key is what
			// makes the round trip converge.
			typeHead, item.head = item.head, ""
		}
		return d.appendAttribute(taskBody, item)
	})
	if err != nil {
		return nil, err
	}
	if !general {
		appendMappingWithComments(entry, block.Type, taskBody, typeHead, "")
		d.attachFoot(taskBody, foot)
	} else {
		d.attachFoot(entry, foot)
	}
	return entry, nil
}

// decodeGroupClause walks a block/rescue/always body, whose contents are task
// entries rather than a value.
func (d *hclDecoder) decodeGroupClause(block *hclsyntax.Block) (*yaml.Node, error) {
	if len(block.Labels) > 0 {
		return nil, hclErrorAt(block.TypeRange, "a %q block takes no label", block.Type)
	}
	seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
	foot, err := d.walkBody(block.Body, hclBodyClose(block.Body), func(item hclBodyItem) error {
		if item.block == nil {
			return hclErrorAt(item.attr.SrcRange, "%q is an attribute inside a %q block, which holds task blocks",
				item.attr.Name, block.Type)
		}
		entry, err := d.decodeTaskBlock(item.block, item.line)
		if err != nil {
			return err
		}
		d.attachHead(entry, item.head)
		seq.Content = append(seq.Content, entry)
		return nil
	})
	if err != nil {
		return nil, err
	}
	d.attachFoot(seq, foot)
	return seq, nil
}

// appendLabel adds the block's label as the mapping's `name` key, carrying the
// comment written beside the block header onto it.
//
// The `name` key is where that comment sits in every other format - `- name:
// create app # trailing note` - so putting it back there is what makes a round
// trip through YAML or JSON5 land the comment in the same place it started.
// A block with no label has no key to hang it on, so it goes on the mapping.
func (d *hclDecoder) appendLabel(node *yaml.Node, block *hclsyntax.Block, lineComment string) {
	if len(block.Labels) != 1 {
		node.LineComment = lineComment
		return
	}
	appendMappingWithComments(node, hclNameKey, yamlStringNode(block.Labels[0]), "", lineComment)
}

// appendAttribute decodes one attribute and appends it to node.
func (d *hclDecoder) appendAttribute(node *yaml.Node, item hclBodyItem) error {
	value, err := d.decodeExpression(item.attr.Expr)
	if err != nil {
		return err
	}
	appendMappingWithComments(node, item.attr.Name, value, item.head, item.line)
	return nil
}

// hclBodyItem is one attribute or block in a body, with the comments that
// belong to it.
type hclBodyItem struct {
	attr  *hclsyntax.Attribute
	block *hclsyntax.Block
	head  string
	line  string
}

// walkBody visits a body's attributes and blocks in source order, handing each
// one to visit along with its head and trailing comments, and returns whatever
// is left before closeOffset as the body's foot comment.
//
// Source order matters twice. hclsyntax keeps attributes in a map, so the
// order has to be recovered from the ranges or a recipe would be reordered at
// random; and the comment cursor consumes strictly forwards, which is what
// binds each comment to the right item.
//
// A block's trailing comment is taken on its OPENING brace, not on the span
// that ends at its closing one: `play "web" { # note` annotates the header,
// and claiming it after the body would leave it to be picked up as the head
// comment of whatever came next.
func (d *hclDecoder) walkBody(body *hclsyntax.Body, closeOffset int, visit func(hclBodyItem) error) (string, error) {
	items := make([]hclBodyItem, 0, len(body.Attributes)+len(body.Blocks))
	for _, attr := range body.Attributes {
		items = append(items, hclBodyItem{attr: attr})
	}
	for _, block := range body.Blocks {
		items = append(items, hclBodyItem{block: block})
	}
	sort.SliceStable(items, func(i, j int) bool {
		return hclItemRange(items[i]).Start.Byte < hclItemRange(items[j]).Start.Byte
	})

	for _, item := range items {
		span := hclItemRange(item)
		item.head = d.takeHead(span.Start.Byte)
		if item.block != nil {
			item.line = d.takeTrailing(item.block.OpenBraceRange.End.Line, item.block.OpenBraceRange.End.Byte)
		} else {
			item.line = d.takeTrailing(span.End.Line, span.End.Byte)
		}
		if err := visit(item); err != nil {
			return "", err
		}
	}
	return d.takeHead(closeOffset), nil
}

// hclBodyClose is the offset of a block body's closing brace, which is where
// its foot comments stop.
func hclBodyClose(body *hclsyntax.Body) int { return body.EndRange.Start.Byte }

// hclCloserOffset is where a collection expression's foot comments stop: just
// before its closing bracket, which is the last byte of its range.
func hclCloserOffset(rng hcl.Range) int {
	if rng.End.Byte > rng.Start.Byte {
		return rng.End.Byte - 1
	}
	return rng.End.Byte
}

// hclItemRange is the source span of an attribute or a block.
func hclItemRange(item hclBodyItem) hcl.Range {
	if item.attr != nil {
		return item.attr.SrcRange
	}
	return hcl.RangeBetween(item.block.TypeRange, item.block.CloseBraceRange)
}

// decodeExpression walks one attribute value.
//
// Only the literal forms are accepted. HCL's own variables, functions,
// operators and `${…}` templates have no meaning in a recipe - docket does its
// substitution with sigil, before anything is parsed - so they are named and
// refused rather than evaluated against an empty scope, where `%{if true}`
// would quietly fold away to nothing.
func (d *hclDecoder) decodeExpression(expr hclsyntax.Expression) (*yaml.Node, error) {
	switch e := expr.(type) {
	case *hclsyntax.TemplateExpr:
		if !e.IsStringLiteral() {
			return nil, d.refuseExpression(expr, "a template")
		}
		value, diags := e.Value(nil)
		if diags.HasErrors() {
			return nil, hclDiagnosticError(diags)
		}
		return yamlStringNode(value.AsString()), nil

	case *hclsyntax.LiteralValueExpr:
		return hclValueToYAML(e.Val, expr.Range())

	case *hclsyntax.UnaryOpExpr:
		// A negative number is a unary op rather than a literal, so `-5` has
		// to come through here. Nothing else does: `!x` is refused below.
		if e.Op != hclsyntax.OpNegate {
			return nil, d.refuseExpression(expr, "an operator")
		}
		value, diags := e.Value(nil)
		if diags.HasErrors() {
			return nil, hclDiagnosticError(diags)
		}
		return hclValueToYAML(value, expr.Range())

	case *hclsyntax.TupleConsExpr:
		seq := &yaml.Node{Kind: yaml.SequenceNode, Tag: "!!seq"}
		for _, item := range e.Exprs {
			head := d.takeHead(item.Range().Start.Byte)
			node, err := d.decodeExpression(item)
			if err != nil {
				return nil, err
			}
			d.attachHead(node, head)
			node.LineComment = d.takeTrailing(item.Range().End.Line, item.Range().End.Byte)
			seq.Content = append(seq.Content, node)
		}
		d.attachFoot(seq, d.takeHead(hclCloserOffset(e.SrcRange)))
		return seq, nil

	case *hclsyntax.ObjectConsExpr:
		node := &yaml.Node{Kind: yaml.MappingNode, Tag: "!!map"}
		for _, item := range e.Items {
			head := d.takeHead(item.KeyExpr.Range().Start.Byte)
			key, err := d.decodeObjectKey(item.KeyExpr)
			if err != nil {
				return nil, err
			}
			value, err := d.decodeExpression(item.ValueExpr)
			if err != nil {
				return nil, err
			}
			line := d.takeTrailing(item.ValueExpr.Range().End.Line, item.ValueExpr.Range().End.Byte)
			appendMappingWithComments(node, key, value, head, line)
		}
		d.attachFoot(node, d.takeHead(hclCloserOffset(e.SrcRange)))
		return node, nil

	case *hclsyntax.ScopeTraversalExpr:
		return nil, d.refuseExpression(expr, "a variable")

	case *hclsyntax.FunctionCallExpr:
		return nil, d.refuseExpression(expr, "a function call")

	case *hclsyntax.ParenthesesExpr:
		return d.decodeExpression(e.Expression)
	}
	return nil, d.refuseExpression(expr, "an expression")
}

// decodeObjectKey renders an object key as the string a mapping key is.
func (d *hclDecoder) decodeObjectKey(expr hclsyntax.Expression) (string, error) {
	if key, ok := expr.(*hclsyntax.ObjectConsKeyExpr); ok {
		// A bare identifier key parses as a traversal that the wrapper knows
		// to read as its own name, which is why this asks the wrapper for the
		// value rather than unwrapping and recursing.
		value, diags := key.Value(nil)
		if diags.HasErrors() {
			return "", hclDiagnosticError(diags)
		}
		if value.Type() != cty.String {
			return "", hclErrorAt(expr.Range(), "object keys must be strings")
		}
		return value.AsString(), nil
	}
	node, err := d.decodeExpression(expr)
	if err != nil {
		return "", err
	}
	if node.Kind != yaml.ScalarNode {
		return "", hclErrorAt(expr.Range(), "object keys must be scalars")
	}
	return node.Value, nil
}

// refuseExpression is the one place an unsupported expression is worded, so
// every variant says the same thing about why.
func (d *hclDecoder) refuseExpression(expr hclsyntax.Expression, what string) error {
	return hclErrorAt(expr.Range(),
		"%s is not a recipe value; docket recipes are data, and substitution is written as \"{{ .name }}\"",
		what)
}

// hclValueToYAML renders a cty literal as a YAML scalar node.
func hclValueToYAML(val cty.Value, rng hcl.Range) (*yaml.Node, error) {
	if val.IsNull() {
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!null", Value: "null"}, nil
	}
	switch val.Type() {
	case cty.Bool:
		value := "false"
		if val.True() {
			value = "true"
		}
		return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!bool", Value: value}, nil
	case cty.String:
		return yamlStringNode(val.AsString()), nil
	case cty.Number:
		return hclNumberToYAML(val.AsBigFloat()), nil
	}
	return nil, hclErrorAt(rng, "%s has no recipe representation", val.Type().FriendlyName())
}

// hclNumberToYAML renders an HCL number.
//
// HCL has one numeric type, so the int / float split is made the way every
// reader of the value would make it - on whether the value has a fractional
// part. Text('f', -1) is the shortest exact decimal, which is the spelling
// both YAML and JSON5 read back as the same number.
func hclNumberToYAML(f *big.Float) *yaml.Node {
	tag := "!!float"
	if f.IsInt() {
		tag = "!!int"
	}
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: tag, Value: f.Text('f', -1)}
}

// hclError is one refusal - HCL's own or docket's - kept structured so a codec
// can report it as a Problem without parsing its own error string back apart.
//
// Message is position-free on purpose. The validator prints a Problem's line
// and column itself, so a message repeating them reads as `line 3:12: … line
// 3, column 12: …`; Error() is what adds them back for the callers that print
// a bare error, which is `docket fmt` and Convert.
type hclError struct {
	Line    int
	Column  int
	Summary string
	Detail  string
	Message string
	prefix  string
}

func (e *hclError) Error() string {
	parts := make([]string, 0, 3)
	if e.prefix != "" {
		parts = append(parts, e.prefix)
	}
	if e.Line > 0 {
		parts = append(parts, fmt.Sprintf("line %d, column %d", e.Line, e.Column))
	}
	return strings.Join(append(parts, e.Message), ": ")
}

// hclDiagnosticError renders HCL's own diagnostics as an error naming the
// first problem and where it is, in the shape the rest of the package reports
// a parse failure.
func hclDiagnosticError(diags hcl.Diagnostics) error {
	for _, diag := range diags {
		if diag.Severity != hcl.DiagError {
			continue
		}
		detail := strings.TrimSpace(diag.Detail)
		message := diag.Summary
		if detail != "" {
			message += ": " + detail
		}
		out := &hclError{Summary: diag.Summary, Detail: detail, Message: message, prefix: "hcl parse error"}
		if diag.Subject != nil {
			out.Line, out.Column = diag.Subject.Start.Line, diag.Subject.Start.Column
		}
		return out
	}
	return fmt.Errorf("hcl parse error: %s", diags.Error())
}

// hclErrorAt builds a positioned error for a refusal docket makes itself,
// rather than one hclsyntax raised.
func hclErrorAt(rng hcl.Range, format string, args ...interface{}) error {
	return &hclError{
		Line:    rng.Start.Line,
		Column:  rng.Start.Column,
		Message: fmt.Sprintf(format, args...),
	}
}

// hclPos renders a range's start as `line N, column M`, for the errors that
// carry no Problem of their own to put it in.
func hclPos(rng hcl.Range) string {
	return fmt.Sprintf("line %d, column %d", rng.Start.Line, rng.Start.Column)
}

// appendMapping adds a key/value pair to a mapping node.
func appendMapping(node *yaml.Node, key string, value *yaml.Node) {
	node.Content = append(node.Content, yamlStringNode(key), value)
}

// appendMappingWithComments adds a pair carrying comments.
//
// Both go on the KEY node, which is where yaml.v3 puts them when it parses the
// same comment back - so a document that came in through HCL re-emits as YAML
// in the shape the YAML formatter would have produced on its own.
func appendMappingWithComments(node *yaml.Node, key string, value *yaml.Node, head, line string) {
	keyNode := yamlStringNode(key)
	keyNode.HeadComment = head
	keyNode.LineComment = line
	node.Content = append(node.Content, keyNode, value)
}

// attachHead sets a head comment, leaving an existing one alone rather than
// overwriting it.
func (d *hclDecoder) attachHead(node *yaml.Node, comment string) {
	if comment == "" {
		return
	}
	node.HeadComment = joinComments(comment, node.HeadComment)
}

// attachFoot sets a body's trailing comment - the one written before its
// closing brace - where yaml.v3 would have parsed the same comment to.
//
// That is not the container node: yaml.v3 hangs a trailing comment off the
// LAST KEY of the mapping it closes, descending through a sequence to get
// there. Putting it anywhere else means the emitter writes it somewhere else,
// which for a deeply nested body means at the end of the document.
func (d *hclDecoder) attachFoot(node *yaml.Node, comment string) {
	if comment == "" {
		return
	}
	anchor := yamlFootAnchor(node)
	anchor.FootComment = joinComments(anchor.FootComment, comment)
}

// yamlFootAnchor returns the node a trailing comment belongs on.
func yamlFootAnchor(node *yaml.Node) *yaml.Node {
	switch node.Kind {
	case yaml.MappingNode:
		if len(node.Content) >= 2 {
			return node.Content[len(node.Content)-2]
		}
	case yaml.SequenceNode:
		if len(node.Content) > 0 {
			return yamlFootAnchor(node.Content[len(node.Content)-1])
		}
	}
	return node
}

// takeHead consumes every comment before offset and renders it as one YAML
// comment string.
func (d *hclDecoder) takeHead(offset int) string {
	if d.comments == nil {
		return ""
	}
	return hclCommentsToYAML(d.comments.before(offset))
}

// takeTrailing consumes a comment that starts on line, after offset.
func (d *hclDecoder) takeTrailing(line, offset int) string {
	if d.comments == nil {
		return ""
	}
	return hclCommentsToYAML(d.comments.trailing(line, offset))
}

// hclCommentCursor hands out the lexer's comment tokens in source order.
//
// Consumption is strictly forwards, which is what binds a comment to the right
// item without a separate association pass: whatever sits before an item and
// has not already been claimed as the previous item's trailing comment is that
// item's head comment. A comment written INSIDE an expression is therefore
// claimed by the next item and re-emitted above it - its text survives, its
// placement moves, and a second `fmt` is stable.
type hclCommentCursor struct {
	items []hclCommentToken
	next  int
}

// hclCommentToken is one comment, with what the cursor needs to place it.
type hclCommentToken struct {
	text  string
	start int
	end   int
	line  int
}

// newHCLCommentCursor lexes src for its comments. A lexer error is ignored:
// the parse has already succeeded by the time this runs, so there is nothing
// left for it to report.
func newHCLCommentCursor(src []byte, filename string) *hclCommentCursor {
	tokens, _ := hclsyntax.LexConfig(src, filename, hcl.InitialPos)
	cursor := &hclCommentCursor{}
	for _, token := range tokens {
		if token.Type != hclsyntax.TokenComment {
			continue
		}
		cursor.items = append(cursor.items, hclCommentToken{
			text:  strings.TrimRight(string(token.Bytes), "\r\n"),
			start: token.Range.Start.Byte,
			end:   token.Range.End.Byte,
			line:  token.Range.Start.Line,
		})
	}
	return cursor
}

// before consumes every unclaimed comment starting before offset.
func (c *hclCommentCursor) before(offset int) []string {
	var out []string
	for c.next < len(c.items) && c.items[c.next].start < offset {
		out = append(out, c.items[c.next].text)
		c.next++
	}
	return out
}

// trailing consumes one comment if it opens on line, at or after offset - the
// `# note` written beside a value rather than above it.
func (c *hclCommentCursor) trailing(line, offset int) []string {
	if c.next >= len(c.items) {
		return nil
	}
	item := c.items[c.next]
	if item.line != line || item.start < offset {
		return nil
	}
	c.next++
	return []string{item.text}
}
