package tasks

import (
	"bytes"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

// FormatJSON5 returns the canonical JSON5 form of data. err is non-nil
// only on a parse error or when the round-trip equivalence guard fails.
//
// The formatter mirrors the YAML Format() contract, adapted to JSON5:
//
//   - 2-space indentation.
//   - Reorders play and task envelope keys per canonicalPlayKeys /
//     canonicalEnvelopeKeys (task-type key last). Members not in the
//     canonical list keep their relative order, appended afterwards.
//   - Inserts a blank line between top-level plays and between top-
//     level task entries inside a play's tasks list, matching the YAML
//     formatter so a JSON5 recipe and its YAML twin look the same.
//   - Always emits trailing commas on multiline objects and arrays to
//     match JSON5 community convention.
//   - Quotes string values with double quotes; quotes object keys only
//     when they are not valid JSON5 identifiers.
//   - Preserves line and block comments in their original anchor
//     position (before a member, beside it on the same line, or after
//     the last member of a container).
//   - Re-parses the canonical output and aborts unless the original
//     and canonical AST trees are structurally equivalent. Catches
//     emitter edge cases before the caller writes anything to disk.
func FormatJSON5(data []byte) ([]byte, error) {
	root, err := parseJSON5(data)
	if err != nil {
		return nil, fmt.Errorf("json5 parse error: %w", err)
	}

	canonicaliseJSON5Recipe(root)

	var buf bytes.Buffer
	emitJSON5(&buf, root, 0)
	out := buf.Bytes()

	roundTrip, err := parseJSON5(out)
	if err != nil {
		return nil, fmt.Errorf("round-trip parse error: %w", err)
	}
	if !equivalentJSON5Nodes(root, roundTrip) {
		return nil, fmt.Errorf("round-trip equivalence check failed; refusing to write")
	}

	return out, nil
}

// json5Node is the AST shape produced by parseJSON5. Kind discriminates
// the variant; fields are populated only for the relevant kind.
type json5Node struct {
	Kind json5Kind

	// Object members; nil when Kind != json5Object.
	Members []*json5Member

	// Array elements; nil when Kind != json5Array.
	Elements []*json5Element

	// Raw scalar source (the original token, e.g. `"hello"`, `42`,
	// `true`, `null`, or an unquoted identifier where JSON5 allows it).
	// Kept as-is so number formats and string escapes round-trip.
	Raw string

	// HeadComments are comments that appeared on lines above this
	// node, preserved in source order. Used for top-level documents
	// and for object/array comments that are not associated with any
	// member.
	HeadComments []string

	// FootComments are trailing comments inside a container after the
	// last member / element but before the closing brace / bracket.
	FootComments []string
}

// json5Kind enumerates AST node variants.
type json5Kind int

const (
	json5Object json5Kind = iota
	json5Array
	json5Scalar
)

// json5Member is a single (key, value) pair inside an object node.
type json5Member struct {
	Key          string
	Value        *json5Node
	HeadComments []string
	LineComment  string // trailing // ... or /* ... */ on the same line as the value
}

// json5Element is a single value inside an array node.
type json5Element struct {
	Value        *json5Node
	HeadComments []string
	LineComment  string
}

// canonicaliseJSON5Recipe applies the same canonical-key ordering the
// YAML formatter applies. The top-level node is expected to be an array
// (the recipe shape); each element is a play whose keys are reordered,
// and each play's tasks: array entry has its envelope keys reordered.
func canonicaliseJSON5Recipe(root *json5Node) {
	if root == nil || root.Kind != json5Array {
		return
	}
	for _, play := range root.Elements {
		if play.Value == nil || play.Value.Kind != json5Object {
			continue
		}
		reorderJSON5Object(play.Value, canonicalPlayKeys)
		tasksMember := findJSON5Member(play.Value, "tasks")
		if tasksMember == nil || tasksMember.Value == nil || tasksMember.Value.Kind != json5Array {
			continue
		}
		for _, task := range tasksMember.Value.Elements {
			if task.Value == nil || task.Value.Kind != json5Object {
				continue
			}
			reorderJSON5TaskEnvelope(task.Value)
		}
	}
}

// reorderJSON5Object rebuilds Members so canonical keys appear in the
// given order, with any unrecognised keys appended in their original
// relative order.
func reorderJSON5Object(node *json5Node, priority []string) {
	out := make([]*json5Member, 0, len(node.Members))
	used := make(map[int]bool, len(node.Members))
	for _, key := range priority {
		for i, m := range node.Members {
			if used[i] {
				continue
			}
			if m.Key == key {
				out = append(out, m)
				used[i] = true
				break
			}
		}
	}
	for i, m := range node.Members {
		if used[i] {
			continue
		}
		out = append(out, m)
	}
	node.Members = out
}

// reorderJSON5TaskEnvelope reorders a task entry object so the task-type
// key (the single non-envelope key) comes last, with envelope keys
// ahead in canonicalEnvelopeKeys order.
func reorderJSON5TaskEnvelope(node *json5Node) {
	out := make([]*json5Member, 0, len(node.Members))
	used := make(map[int]bool, len(node.Members))
	for _, key := range canonicalEnvelopeKeys {
		for i, m := range node.Members {
			if used[i] {
				continue
			}
			if m.Key == key {
				out = append(out, m)
				used[i] = true
				break
			}
		}
	}
	for i, m := range node.Members {
		if used[i] {
			continue
		}
		if envelopeKeySet[m.Key] {
			out = append(out, m)
			used[i] = true
		}
	}
	for i, m := range node.Members {
		if used[i] {
			continue
		}
		out = append(out, m)
	}
	node.Members = out
}

func findJSON5Member(node *json5Node, key string) *json5Member {
	for _, m := range node.Members {
		if m.Key == key {
			return m
		}
	}
	return nil
}

// equivalentJSON5Nodes compares two AST trees structurally, ignoring
// comments. Object key order is not considered (canonicalisation
// reorders by design); arrays compare element-wise.
func equivalentJSON5Nodes(a, b *json5Node) bool {
	if a == nil && b == nil {
		return true
	}
	if a == nil || b == nil || a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case json5Scalar:
		return normaliseJSON5Scalar(a.Raw) == normaliseJSON5Scalar(b.Raw)
	case json5Array:
		if len(a.Elements) != len(b.Elements) {
			return false
		}
		for i := range a.Elements {
			if !equivalentJSON5Nodes(a.Elements[i].Value, b.Elements[i].Value) {
				return false
			}
		}
		return true
	case json5Object:
		if len(a.Members) != len(b.Members) {
			return false
		}
		bIdx := make(map[string]*json5Node, len(b.Members))
		for _, m := range b.Members {
			bIdx[m.Key] = m.Value
		}
		for _, m := range a.Members {
			bv, ok := bIdx[m.Key]
			if !ok {
				return false
			}
			if !equivalentJSON5Nodes(m.Value, bv) {
				return false
			}
		}
		return true
	}
	return false
}

// normaliseJSON5Scalar collapses representational differences that do
// not affect semantics: a single-quoted string and a double-quoted
// string with the same decoded body are equal; +5 and 5 are equal as
// numbers. Implemented as a best-effort string normalisation; this
// function is only used by the round-trip guard so false negatives
// surface as a refusal to write rather than corruption.
func normaliseJSON5Scalar(raw string) string {
	if len(raw) == 0 {
		return raw
	}
	switch raw[0] {
	case '"', '\'':
		decoded, ok := decodeJSON5String(raw)
		if ok {
			return "S:" + decoded
		}
	}
	return strings.TrimPrefix(raw, "+")
}

func decodeJSON5String(raw string) (string, bool) {
	if len(raw) < 2 {
		return "", false
	}
	quote := raw[0]
	if raw[len(raw)-1] != quote {
		return "", false
	}
	body := raw[1 : len(raw)-1]
	var b strings.Builder
	for i := 0; i < len(body); i++ {
		c := body[i]
		if c != '\\' {
			b.WriteByte(c)
			continue
		}
		i++
		if i >= len(body) {
			return "", false
		}
		switch body[i] {
		case 'n':
			b.WriteByte('\n')
		case 't':
			b.WriteByte('\t')
		case 'r':
			b.WriteByte('\r')
		case '"', '\'', '\\', '/':
			b.WriteByte(body[i])
		case '\n':
			// JSON5 line continuation; keep nothing.
		default:
			b.WriteByte('\\')
			b.WriteByte(body[i])
		}
	}
	return b.String(), true
}

// ---------------------------------------------------------------------
// Lexer
// ---------------------------------------------------------------------

type json5TokKind int

const (
	tokEOF json5TokKind = iota
	tokLBrace
	tokRBrace
	tokLBracket
	tokRBracket
	tokColon
	tokComma
	tokString
	tokNumber
	tokIdent // unquoted identifier (also true / false / null)
	tokLineComment
	tokBlockComment
)

type json5Tok struct {
	Kind     json5TokKind
	Raw      string // verbatim source slice
	NewlineBefore bool // true if any newline preceded this token
}

type json5Lexer struct {
	src    []byte
	pos    int
	tokens []json5Tok
}

func lexJSON5(src []byte) ([]json5Tok, error) {
	l := &json5Lexer{src: src}
	pendingNewline := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == ' ' || c == '\t' || c == '\r' {
			l.pos++
			continue
		}
		if c == '\n' {
			l.pos++
			pendingNewline = true
			continue
		}
		if c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/' {
			start := l.pos
			l.pos += 2
			for l.pos < len(l.src) && l.src[l.pos] != '\n' {
				l.pos++
			}
			l.tokens = append(l.tokens, json5Tok{Kind: tokLineComment, Raw: string(l.src[start:l.pos]), NewlineBefore: pendingNewline})
			pendingNewline = false
			continue
		}
		if c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*' {
			start := l.pos
			l.pos += 2
			for l.pos+1 < len(l.src) && !(l.src[l.pos] == '*' && l.src[l.pos+1] == '/') {
				l.pos++
			}
			if l.pos+1 >= len(l.src) {
				return nil, fmt.Errorf("unterminated block comment at offset %d", start)
			}
			l.pos += 2
			l.tokens = append(l.tokens, json5Tok{Kind: tokBlockComment, Raw: string(l.src[start:l.pos]), NewlineBefore: pendingNewline})
			pendingNewline = false
			continue
		}

		switch c {
		case '{':
			l.tokens = append(l.tokens, json5Tok{Kind: tokLBrace, Raw: "{", NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case '}':
			l.tokens = append(l.tokens, json5Tok{Kind: tokRBrace, Raw: "}", NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case '[':
			l.tokens = append(l.tokens, json5Tok{Kind: tokLBracket, Raw: "[", NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case ']':
			l.tokens = append(l.tokens, json5Tok{Kind: tokRBracket, Raw: "]", NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case ':':
			l.tokens = append(l.tokens, json5Tok{Kind: tokColon, Raw: ":", NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case ',':
			l.tokens = append(l.tokens, json5Tok{Kind: tokComma, Raw: ",", NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case '"', '\'':
			tok, err := l.readString(c)
			if err != nil {
				return nil, err
			}
			tok.NewlineBefore = pendingNewline
			pendingNewline = false
			l.tokens = append(l.tokens, tok)
			continue
		}

		if c == '-' || c == '+' || c == '.' || (c >= '0' && c <= '9') {
			tok, err := l.readNumber()
			if err != nil {
				return nil, err
			}
			tok.NewlineBefore = pendingNewline
			pendingNewline = false
			l.tokens = append(l.tokens, tok)
			continue
		}

		if isIdentStart(rune(c)) {
			tok := l.readIdent()
			tok.NewlineBefore = pendingNewline
			pendingNewline = false
			l.tokens = append(l.tokens, tok)
			continue
		}

		return nil, fmt.Errorf("unexpected character %q at offset %d", c, l.pos)
	}
	l.tokens = append(l.tokens, json5Tok{Kind: tokEOF, NewlineBefore: pendingNewline})
	return l.tokens, nil
}

func (l *json5Lexer) readString(quote byte) (json5Tok, error) {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\\' {
			l.pos += 2
			continue
		}
		if c == quote {
			l.pos++
			return json5Tok{Kind: tokString, Raw: string(l.src[start:l.pos])}, nil
		}
		l.pos++
	}
	return json5Tok{}, fmt.Errorf("unterminated string starting at offset %d", start)
}

func (l *json5Lexer) readNumber() (json5Tok, error) {
	start := l.pos
	if l.src[l.pos] == '+' || l.src[l.pos] == '-' {
		l.pos++
	}
	if l.pos+1 < len(l.src) && l.src[l.pos] == '0' && (l.src[l.pos+1] == 'x' || l.src[l.pos+1] == 'X') {
		l.pos += 2
		for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
			l.pos++
		}
		return json5Tok{Kind: tokNumber, Raw: string(l.src[start:l.pos])}, nil
	}
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if (c >= '0' && c <= '9') || c == '.' || c == 'e' || c == 'E' || c == '+' || c == '-' {
			l.pos++
			continue
		}
		break
	}
	if l.pos == start {
		return json5Tok{}, fmt.Errorf("invalid number at offset %d", start)
	}
	return json5Tok{Kind: tokNumber, Raw: string(l.src[start:l.pos])}, nil
}

func (l *json5Lexer) readIdent() json5Tok {
	start := l.pos
	for l.pos < len(l.src) {
		r, sz := utf8.DecodeRune(l.src[l.pos:])
		if !isIdentPart(r) {
			break
		}
		l.pos += sz
	}
	return json5Tok{Kind: tokIdent, Raw: string(l.src[start:l.pos])}
}

func isIdentStart(r rune) bool {
	return r == '_' || r == '$' || unicode.IsLetter(r)
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || unicode.IsDigit(r)
}

func isHexDigit(c byte) bool {
	return (c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isJSON5IdentKey reports whether s is safe to emit unquoted as an
// object key. Keeps in step with the JSON5 spec's IdentifierName rule
// (subset chosen here for ASCII identifiers).
func isJSON5IdentKey(s string) bool {
	if s == "" {
		return false
	}
	for i, r := range s {
		if i == 0 {
			if !isIdentStart(r) {
				return false
			}
			continue
		}
		if !isIdentPart(r) {
			return false
		}
	}
	return true
}

// ---------------------------------------------------------------------
// Parser
// ---------------------------------------------------------------------

type json5Parser struct {
	tokens []json5Tok
	pos    int
}

func parseJSON5(src []byte) (*json5Node, error) {
	tokens, err := lexJSON5(src)
	if err != nil {
		return nil, err
	}
	p := &json5Parser{tokens: tokens}
	headComments := p.consumeComments()
	root, err := p.parseValue()
	if err != nil {
		return nil, err
	}
	if root != nil {
		root.HeadComments = append(headComments, root.HeadComments...)
	}
	footComments := p.consumeComments()
	if root != nil {
		root.FootComments = append(root.FootComments, footComments...)
	}
	if p.peek().Kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q after root value", p.peek().Raw)
	}
	return root, nil
}

func (p *json5Parser) peek() json5Tok {
	if p.pos < len(p.tokens) {
		return p.tokens[p.pos]
	}
	return json5Tok{Kind: tokEOF}
}

func (p *json5Parser) advance() json5Tok {
	t := p.peek()
	p.pos++
	return t
}

// consumeComments consumes a run of comment tokens and returns them
// as raw strings.
func (p *json5Parser) consumeComments() []string {
	var out []string
	for {
		t := p.peek()
		if t.Kind != tokLineComment && t.Kind != tokBlockComment {
			return out
		}
		out = append(out, t.Raw)
		p.pos++
	}
}

// consumeTrailingLineComment consumes one line/block comment that sits
// on the same line as the just-emitted value (no preceding newline).
// Returns "" if the next non-trivia token is not a same-line comment.
func (p *json5Parser) consumeTrailingLineComment() string {
	t := p.peek()
	if (t.Kind == tokLineComment || t.Kind == tokBlockComment) && !t.NewlineBefore {
		p.pos++
		return t.Raw
	}
	return ""
}

func (p *json5Parser) parseValue() (*json5Node, error) {
	t := p.peek()
	switch t.Kind {
	case tokLBrace:
		return p.parseObject()
	case tokLBracket:
		return p.parseArray()
	case tokString, tokNumber, tokIdent:
		p.advance()
		return &json5Node{Kind: json5Scalar, Raw: t.Raw}, nil
	}
	return nil, fmt.Errorf("unexpected token %q while parsing value", t.Raw)
}

func (p *json5Parser) parseObject() (*json5Node, error) {
	if p.peek().Kind != tokLBrace {
		return nil, fmt.Errorf("expected { at object start, got %q", p.peek().Raw)
	}
	p.advance()
	node := &json5Node{Kind: json5Object}
	for {
		head := p.consumeComments()
		if p.peek().Kind == tokRBrace {
			p.advance()
			node.FootComments = head
			return node, nil
		}
		key, err := p.parseKey()
		if err != nil {
			return nil, err
		}
		if p.peek().Kind != tokColon {
			return nil, fmt.Errorf("expected : after key %q, got %q", key, p.peek().Raw)
		}
		p.advance()
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		member := &json5Member{Key: key, Value: val, HeadComments: head}
		// Trailing comment on the same line as the value.
		member.LineComment = p.consumeTrailingLineComment()
		// Optional comma; trailing comma allowed.
		if p.peek().Kind == tokComma {
			p.advance()
			if member.LineComment == "" {
				member.LineComment = p.consumeTrailingLineComment()
			}
		}
		node.Members = append(node.Members, member)
	}
}

func (p *json5Parser) parseArray() (*json5Node, error) {
	if p.peek().Kind != tokLBracket {
		return nil, fmt.Errorf("expected [ at array start, got %q", p.peek().Raw)
	}
	p.advance()
	node := &json5Node{Kind: json5Array}
	for {
		head := p.consumeComments()
		if p.peek().Kind == tokRBracket {
			p.advance()
			node.FootComments = head
			return node, nil
		}
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		elem := &json5Element{Value: val, HeadComments: head}
		elem.LineComment = p.consumeTrailingLineComment()
		if p.peek().Kind == tokComma {
			p.advance()
			if elem.LineComment == "" {
				elem.LineComment = p.consumeTrailingLineComment()
			}
		}
		node.Elements = append(node.Elements, elem)
	}
}

func (p *json5Parser) parseKey() (string, error) {
	t := p.advance()
	switch t.Kind {
	case tokString:
		decoded, ok := decodeJSON5String(t.Raw)
		if !ok {
			return "", fmt.Errorf("invalid string key %q", t.Raw)
		}
		return decoded, nil
	case tokIdent, tokNumber:
		return t.Raw, nil
	}
	return "", fmt.Errorf("expected key, got %q", t.Raw)
}

// ---------------------------------------------------------------------
// Emitter
// ---------------------------------------------------------------------

const json5Indent = "  "

func emitJSON5(buf *bytes.Buffer, node *json5Node, depth int) {
	emitJSON5HeadComments(buf, node.HeadComments, depth)
	emitJSON5Value(buf, node, depth)
	emitJSON5FootComments(buf, node.FootComments, depth)
	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		buf.WriteByte('\n')
	}
}

func emitJSON5Value(buf *bytes.Buffer, node *json5Node, depth int) {
	emitJSON5ValueCtx(buf, node, depth, false)
}

// emitJSON5ValueCtx is the value emitter aware of one extra context
// bit: tasksMember signals that the value being emitted is the value
// of a member named "tasks" / "block" / "rescue" / "always", so an
// array value gets blank-line separation between its top-level
// elements (matching the YAML formatter's tasks-list rule).
func emitJSON5ValueCtx(buf *bytes.Buffer, node *json5Node, depth int, tasksMember bool) {
	if node == nil {
		buf.WriteString("null")
		return
	}
	switch node.Kind {
	case json5Scalar:
		buf.WriteString(canonicaliseScalarRaw(node.Raw))
	case json5Array:
		emitJSON5Array(buf, node, depth, tasksMember)
	case json5Object:
		emitJSON5Object(buf, node, depth)
	}
}

func emitJSON5Array(buf *bytes.Buffer, node *json5Node, depth int, tasksMember bool) {
	if len(node.Elements) == 0 && len(node.FootComments) == 0 {
		buf.WriteString("[]")
		return
	}
	if isInlineArray(node) {
		buf.WriteByte('[')
		for i, elem := range node.Elements {
			if i > 0 {
				buf.WriteString(", ")
			}
			emitJSON5Value(buf, elem.Value, depth)
		}
		buf.WriteByte(']')
		return
	}
	buf.WriteByte('[')
	buf.WriteByte('\n')
	innerIndent := strings.Repeat(json5Indent, depth+1)
	closingIndent := strings.Repeat(json5Indent, depth)
	insertPlayBlankLine := depth == 0
	insertTaskBlankLine := tasksMember
	for i, elem := range node.Elements {
		if (insertTaskBlankLine || insertPlayBlankLine) && i > 0 {
			buf.WriteByte('\n')
		}
		emitJSON5HeadComments(buf, elem.HeadComments, depth+1)
		buf.WriteString(innerIndent)
		emitJSON5Value(buf, elem.Value, depth+1)
		buf.WriteByte(',')
		if elem.LineComment != "" {
			buf.WriteByte(' ')
			buf.WriteString(elem.LineComment)
		}
		buf.WriteByte('\n')
	}
	emitJSON5FootComments(buf, node.FootComments, depth+1)
	buf.WriteString(closingIndent)
	buf.WriteByte(']')
}

func emitJSON5Object(buf *bytes.Buffer, node *json5Node, depth int) {
	if len(node.Members) == 0 && len(node.FootComments) == 0 {
		buf.WriteString("{}")
		return
	}
	buf.WriteByte('{')
	buf.WriteByte('\n')
	innerIndent := strings.Repeat(json5Indent, depth+1)
	closingIndent := strings.Repeat(json5Indent, depth)
	for _, m := range node.Members {
		emitJSON5HeadComments(buf, m.HeadComments, depth+1)
		buf.WriteString(innerIndent)
		buf.WriteString(formatJSON5Key(m.Key))
		buf.WriteString(": ")
		emitJSON5ValueCtx(buf, m.Value, depth+1, isTaskListMemberKey(m.Key))
		buf.WriteByte(',')
		if m.LineComment != "" {
			buf.WriteByte(' ')
			buf.WriteString(m.LineComment)
		}
		buf.WriteByte('\n')
	}
	emitJSON5FootComments(buf, node.FootComments, depth+1)
	buf.WriteString(closingIndent)
	buf.WriteByte('}')
}

// isTaskListMemberKey returns true for the canonical play / group
// keys that carry a list of task entries. The emitter inserts blank
// lines between elements of these arrays so the shape matches the
// YAML formatter's tasks-block rule.
func isTaskListMemberKey(key string) bool {
	switch key {
	case "tasks", "block", "rescue", "always":
		return true
	}
	return false
}

func emitJSON5HeadComments(buf *bytes.Buffer, comments []string, depth int) {
	indent := strings.Repeat(json5Indent, depth)
	for _, c := range comments {
		buf.WriteString(indent)
		buf.WriteString(c)
		buf.WriteByte('\n')
	}
}

func emitJSON5FootComments(buf *bytes.Buffer, comments []string, depth int) {
	emitJSON5HeadComments(buf, comments, depth)
}

// isInlineArray returns true for arrays of scalars that should be
// rendered on a single line. Empty arrays handled separately. Keep
// scalar-only arrays one-line so `domains: ["a.example.com"]` renders
// the same way it does in the YAML formatter.
func isInlineArray(node *json5Node) bool {
	if len(node.FootComments) > 0 {
		return false
	}
	for _, e := range node.Elements {
		if e == nil || e.Value == nil || e.Value.Kind != json5Scalar {
			return false
		}
		if len(e.HeadComments) > 0 || e.LineComment != "" {
			return false
		}
	}
	return len(node.Elements) > 0
}

// formatJSON5Key chooses between an unquoted identifier (when valid)
// and a double-quoted string. Always quoting works too but the
// identifier form is the JSON5 community default for plain keys.
func formatJSON5Key(key string) string {
	if isJSON5IdentKey(key) {
		return key
	}
	return quoteJSON5String(key)
}

// quoteJSON5String wraps s in double quotes, escaping the canonical
// JSON5 escape characters. Used when emitting object keys; string
// values keep their original quote form via canonicaliseScalarRaw.
func quoteJSON5String(s string) string {
	var b strings.Builder
	b.WriteByte('"')
	for _, r := range s {
		switch r {
		case '\\':
			b.WriteString(`\\`)
		case '"':
			b.WriteString(`\"`)
		case '\n':
			b.WriteString(`\n`)
		case '\t':
			b.WriteString(`\t`)
		case '\r':
			b.WriteString(`\r`)
		default:
			b.WriteRune(r)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// canonicaliseScalarRaw normalises a scalar's source form: single-
// quoted strings convert to double-quoted (idiomatic JSON5 output),
// other scalars (numbers, true, false, null, identifier-form values)
// pass through verbatim. Sigil templates inside strings are preserved
// because they live entirely inside the quoted body.
func canonicaliseScalarRaw(raw string) string {
	if len(raw) == 0 {
		return raw
	}
	if raw[0] == '\'' && raw[len(raw)-1] == '\'' {
		decoded, ok := decodeJSON5String(raw)
		if ok {
			return quoteJSON5String(decoded)
		}
	}
	return raw
}
