package tasks

import (
	"bytes"
	"fmt"
	"strings"
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

	// canonicaliseScalarRaw folds a single-quoted string into the
	// double-quoted canonical form, which for a string holding an
	// interpolation is not a spelling change but a change to how the
	// substituted value is escaped. Refuse instead, after the parse so a
	// malformed file still reports the parse error it always did.
	if err := refuseJSON5UnportableQuoting(data); err != nil {
		return nil, err
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

	// AfterComments are comments that follow the value entirely. Only the
	// root node uses this (comments after the closing bracket / brace, or
	// after a scalar root). Kept distinct from FootComments so a container
	// root's foot comments are emitted once inside the brackets and its
	// after-value comments once afterwards, rather than both being doubled.
	AfterComments []string
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

// decodeJSON5String decodes a quoted JSON5 string token (single or double
// quoted) into its actual character content, resolving every JSON5 escape
// to the character it denotes. ok is false on a malformed escape (bad hex,
// truncated sequence); callers treat that as "leave the original quoting
// untouched" so a formatting bug can never corrupt a recipe.
//
// It knows the whole JSON5 escape set, including the \v, \0, \xHH and
// identity escapes readString now refuses on the loader's behalf (#537).
// Keeping them costs nothing and means the decoder still answers for a
// string that reached it from somewhere other than the lexer.
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
			if c < utf8.RuneSelf {
				b.WriteByte(c)
				continue
			}
			// Invalid UTF-8 decodes to the replacement character, one byte at
			// a time, which is what unquoteBytes - and so titanous/json5, and
			// so the loader - makes of it. Copying the bytes through instead
			// meant the value docket converted was not the value the loader
			// read (#537).
			r, size := utf8.DecodeRuneInString(body[i:])
			if r == utf8.RuneError && size == 1 {
				b.WriteRune(utf8.RuneError)
				continue
			}
			b.WriteString(body[i : i+size])
			i += size - 1
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
		case 'b':
			b.WriteByte('\b')
		case 'f':
			b.WriteByte('\f')
		case 'v':
			b.WriteByte('\v')
		case '0':
			// JSON5 NUL escape. A following decimal digit would form an
			// (unsupported) octal-looking sequence, so refuse it.
			if i+1 < len(body) && body[i+1] >= '0' && body[i+1] <= '9' {
				return "", false
			}
			b.WriteByte(0)
		case 'x':
			if i+2 >= len(body) {
				return "", false
			}
			hi, ok1 := hexDigitValue(body[i+1])
			lo, ok2 := hexDigitValue(body[i+2])
			if !ok1 || !ok2 {
				return "", false
			}
			b.WriteByte(hi<<4 | lo)
			i += 2
		case 'u':
			r, adv, ok := decodeJSON5Unicode(body, i)
			if !ok {
				return "", false
			}
			b.WriteRune(r)
			i += adv
		case '"', '\'', '\\', '/':
			b.WriteByte(body[i])
		case '\n':
			// Line continuation; emit nothing.
		case '\r':
			// Line continuation, possibly a CRLF pair.
			if i+1 < len(body) && body[i+1] == '\n' {
				i++
			}
		default:
			// Any other escaped character represents itself in JSON5.
			b.WriteByte(body[i])
		}
	}
	return b.String(), true
}

// decodeJSON5Unicode decodes a \uXXXX escape whose 'u' is at body[i]
// (the leading backslash already consumed). A high surrogate immediately
// followed by \uYYYY that is a valid low surrogate is combined into the
// astral code point. adv is the number of bytes consumed after the 'u'.
// ok is false only for a truncated or non-hex sequence, which the lexer
// rejects outright and the loader rejects with it.
//
// An unpaired surrogate is not an error: it decodes to the replacement
// character, consuming just the escape it read, so the escape after it is
// still read on its own terms. That is encoding/json's rule and therefore
// titanous/json5's, and refusing it here made `docket fmt` fail on a key
// the loader reads quite happily (#537).
func decodeJSON5Unicode(body string, i int) (r rune, adv int, ok bool) {
	hi, ok := readHex4(body, i+1)
	if !ok {
		return 0, 0, false
	}
	if hi >= 0xD800 && hi <= 0xDBFF {
		if i+6 < len(body) && body[i+5] == '\\' && body[i+6] == 'u' {
			lo, ok := readHex4(body, i+7)
			if ok && lo >= 0xDC00 && lo <= 0xDFFF {
				combined := 0x10000 + (rune(hi-0xD800) << 10) + rune(lo-0xDC00)
				return combined, 10, true
			}
		}
		return utf8.RuneError, 4, true
	}
	if hi >= 0xDC00 && hi <= 0xDFFF {
		return utf8.RuneError, 4, true
	}
	return rune(hi), 4, true
}

// readHex4 reads exactly four hex digits at body[start:] and returns their
// value. ok is false when fewer than four digits remain or one is invalid.
func readHex4(body string, start int) (int, bool) {
	if start+3 >= len(body) {
		return 0, false
	}
	v := 0
	for k := 0; k < 4; k++ {
		d, ok := hexDigitValue(body[start+k])
		if !ok {
			return 0, false
		}
		v = v<<4 | int(d)
	}
	return v, true
}

// hexDigitValue returns the numeric value of a single hex digit byte.
func hexDigitValue(c byte) (byte, bool) {
	switch {
	case c >= '0' && c <= '9':
		return c - '0', true
	case c >= 'a' && c <= 'f':
		return c - 'a' + 10, true
	case c >= 'A' && c <= 'F':
		return c - 'A' + 10, true
	}
	return 0, false
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
	Kind          json5TokKind
	Raw           string // verbatim source slice
	Offset        int    // byte offset of the token's first byte in the source
	NewlineBefore bool   // true if any newline preceded this token
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
			if err := json5CommentUTF8Error(l.src[start:l.pos], start); err != nil {
				return nil, err
			}
			l.tokens = append(l.tokens, json5Tok{Kind: tokLineComment, Raw: string(l.src[start:l.pos]), Offset: start, NewlineBefore: pendingNewline})
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
			if err := json5CommentUTF8Error(l.src[start:l.pos], start); err != nil {
				return nil, err
			}
			l.tokens = append(l.tokens, json5Tok{Kind: tokBlockComment, Raw: string(l.src[start:l.pos]), Offset: start, NewlineBefore: pendingNewline})
			pendingNewline = false
			continue
		}

		switch c {
		case '{':
			l.tokens = append(l.tokens, json5Tok{Kind: tokLBrace, Raw: "{", Offset: l.pos, NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case '}':
			l.tokens = append(l.tokens, json5Tok{Kind: tokRBrace, Raw: "}", Offset: l.pos, NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case '[':
			l.tokens = append(l.tokens, json5Tok{Kind: tokLBracket, Raw: "[", Offset: l.pos, NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case ']':
			l.tokens = append(l.tokens, json5Tok{Kind: tokRBracket, Raw: "]", Offset: l.pos, NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case ':':
			l.tokens = append(l.tokens, json5Tok{Kind: tokColon, Raw: ":", Offset: l.pos, NewlineBefore: pendingNewline})
			l.pos++
			pendingNewline = false
			continue
		case ',':
			l.tokens = append(l.tokens, json5Tok{Kind: tokComma, Raw: ",", Offset: l.pos, NewlineBefore: pendingNewline})
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
	l.tokens = append(l.tokens, json5Tok{Kind: tokEOF, Offset: len(l.src), NewlineBefore: pendingNewline})
	return l.tokens, nil
}

// json5CommentUTF8Error reports a comment whose bytes are not text. raw is
// the comment token including its delimiters, and start its offset in the
// source, so the error can name the offending byte rather than the token.
//
// The lexer keeps a comment verbatim, which is what lets `fmt` carry one
// across a conversion, and nothing between here and yaml.v3's emitter looks
// at those bytes again: a comment is written out raw, and the writer panics
// outright on a byte that starts no rune, so `docket fmt --format yaml` died
// rather than naming a problem with the file (#544).
//
// Only a comment needs this. decodeJSON5String already replaces an invalid
// byte in a string with U+FFFD, the way the loader does, and an unquoted key
// cannot hold one at all, isIdentPart being ASCII-only. A YAML source cannot
// carry one either: yaml.v3 refuses it while reading.
//
// It is the one place the formatter's grammar is deliberately narrower than
// titanous/json5's rather than identical to it (#537). It can afford to be:
// the loader throws every comment away before it reads anything, so this
// refuses no recipe the loader would have gone on to run. This is a fmt rule.
func json5CommentUTF8Error(raw []byte, start int) error {
	if utf8.Valid(raw) {
		return nil
	}
	for i := 0; i < len(raw); {
		r, size := utf8.DecodeRune(raw[i:])
		if r == utf8.RuneError && size == 1 {
			return fmt.Errorf("invalid UTF-8 byte %#x in comment at offset %d", raw[i], start+i)
		}
		i += size
	}
	return nil
}

// readString reads a quoted string literal, validating it as it goes.
//
// The rules are titanous/json5's, which are encoding/json's plus the single
// quote and the line continuation: no raw control character inside the
// quotes, and an escape drawn from a fixed set. JSON5 itself also spells
// \v, \0 and \xHH and lets any other character escape to itself, but the
// loader refuses all of those, and a string the formatter keeps and the
// loader refuses is a recipe that formats cleanly and then fails to run
// (#537).
func (l *json5Lexer) readString(quote byte) (json5Tok, error) {
	start := l.pos
	l.pos++
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\\':
			if err := l.readStringEscape(); err != nil {
				return json5Tok{}, err
			}
			continue
		case c == quote:
			l.pos++
			return json5Tok{Kind: tokString, Raw: string(l.src[start:l.pos]), Offset: start}, nil
		case c < 0x20:
			// A newline or a tab has to be written as an escape. Reading one
			// raw would also mean a runaway string swallowing the rest of the
			// recipe before anything complained.
			return json5Tok{}, fmt.Errorf("control character in string at offset %d", l.pos)
		}
		l.pos++
	}
	return json5Tok{}, fmt.Errorf("unterminated string starting at offset %d", start)
}

// readStringEscape consumes one backslash escape inside a string literal.
// A backslash at the very end of the source is an error rather than a step
// past it, which is what the old two-byte skip could do.
func (l *json5Lexer) readStringEscape() error {
	start := l.pos
	l.pos++
	if l.pos >= len(l.src) {
		return fmt.Errorf("unterminated string escape at offset %d", start)
	}
	c := l.src[l.pos]
	l.pos++
	switch c {
	case 'b', 'f', 'n', 'r', 't', '\\', '/', '"', '\'', '\n':
		// The bare newline is a line continuation; it contributes nothing to
		// the decoded value.
		return nil
	case '\r':
		// The same, written CRLF: the LF belongs to this escape.
		if l.pos < len(l.src) && l.src[l.pos] == '\n' {
			l.pos++
		}
		return nil
	case 'u':
		for i := 0; i < 4; i++ {
			if l.pos >= len(l.src) || !isHexDigit(l.src[l.pos]) {
				return fmt.Errorf("invalid \\u escape in string at offset %d", start)
			}
			l.pos++
		}
		return nil
	}
	return fmt.Errorf("invalid string escape %q at offset %d", l.src[start:l.pos], start)
}

// readNumber reads a JSON5 NumericLiteral: an optional sign, then Infinity,
// a hexadecimal integer, or a decimal number.
//
// What it accepts is deliberately the grammar titanous/json5 accepts - its
// scanner states, plus the isValidNumber check its decoder applies
// afterwards - because that is the parser apply / plan / validate read a
// recipe with. A spelling the formatter passes through and the loader
// refuses is a file that formats cleanly and then fails to run, which is
// what a lone "-", a digitless "0x", "01", "1.2.3" and "1e" all used to be
// (#537).
func (l *json5Lexer) readNumber() (json5Tok, error) {
	start := l.pos
	if l.src[l.pos] == '+' || l.src[l.pos] == '-' {
		l.pos++
	}
	// JSON5 spells the infinities Infinity and allows a sign in front. It
	// has to be recognised here rather than left to readIdent, because the
	// sign has already been consumed: the digit scan below stops dead on the
	// "I", which used to yield a lone "-" token with "Infinity" following it
	// as a separate identifier (#418). An unsigned Infinity never reaches
	// here at all; it starts with a letter, so the lexer sends it to
	// readIdent.
	//
	// NaN is deliberately absent. The JSON5 spec allows a sign in front of
	// it, but titanous/json5 does not - after a sign its scanner takes a
	// digit, a dot or an "I" and nothing else - so a signed NaN is a number
	// the formatter would keep and the loader would refuse.
	if l.hasWordAt(l.pos, "Infinity") {
		l.pos += len("Infinity")
		return l.numberTok(start), nil
	}

	intStart := l.pos
	if l.pos < len(l.src) && l.src[l.pos] == '0' {
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == 'x' || l.src[l.pos] == 'X') {
			l.pos++
			if !l.readHexDigits() {
				return json5Tok{}, fmt.Errorf("invalid hexadecimal number at offset %d", start)
			}
			return l.numberTok(start), nil
		}
		// A leading zero is the whole integer part. JSON5 has no octal
		// literal, so the integer part of 0123 ends at the 0 and the rest is
		// a second token the parser will refuse for the comma that is not
		// between them.
	} else {
		l.readDigits()
	}
	hasInt := l.pos > intStart

	hasFraction := false
	if l.pos < len(l.src) && l.src[l.pos] == '.' {
		l.pos++
		hasFraction = l.readDigits()
	}
	// 1. and .5 are both numbers; a bare ".", "-" or "+" is not.
	if !hasInt && !hasFraction {
		return json5Tok{}, fmt.Errorf("invalid number at offset %d", start)
	}

	if l.pos < len(l.src) && (l.src[l.pos] == 'e' || l.src[l.pos] == 'E') {
		l.pos++
		if l.pos < len(l.src) && (l.src[l.pos] == '+' || l.src[l.pos] == '-') {
			l.pos++
		}
		if !l.readDigits() {
			return json5Tok{}, fmt.Errorf("invalid exponent in number at offset %d", start)
		}
	}
	return l.numberTok(start), nil
}

// numberTok wraps the source between start and the current position as a
// number token.
func (l *json5Lexer) numberTok(start int) json5Tok {
	return json5Tok{Kind: tokNumber, Raw: string(l.src[start:l.pos]), Offset: start}
}

// readDigits consumes a run of decimal digits and reports whether there was
// at least one.
func (l *json5Lexer) readDigits() bool {
	start := l.pos
	for l.pos < len(l.src) && l.src[l.pos] >= '0' && l.src[l.pos] <= '9' {
		l.pos++
	}
	return l.pos > start
}

// readHexDigits is readDigits for a hexadecimal run.
func (l *json5Lexer) readHexDigits() bool {
	start := l.pos
	for l.pos < len(l.src) && isHexDigit(l.src[l.pos]) {
		l.pos++
	}
	return l.pos > start
}

// hasWordAt reports whether the source holds word at pos and does not
// continue into a longer identifier, so "NaN" matches but "NaNny" does
// not.
func (l *json5Lexer) hasWordAt(pos int, word string) bool {
	if pos+len(word) > len(l.src) {
		return false
	}
	if string(l.src[pos:pos+len(word)]) != word {
		return false
	}
	if pos+len(word) == len(l.src) {
		return true
	}
	r, _ := utf8.DecodeRune(l.src[pos+len(word):])
	return !isIdentPart(r)
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
	return json5Tok{Kind: tokIdent, Raw: string(l.src[start:l.pos]), Offset: start}
}

// isIdentStart and isIdentPart spell the unquoted-key alphabet, and they
// are ASCII on purpose. The JSON5 spec takes any Unicode letter, but
// titanous/json5 takes [A-Za-z0-9_$] and nothing else, and it is the reader
// apply / plan / validate use. Letting é through here meant `docket fmt`
// unquoted a `café` key the loader had been reading quite happily and wrote
// a recipe that no longer loaded (#537).
func isIdentStart(r rune) bool {
	return r == '_' || r == '$' || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}

func isIdentPart(r rune) bool {
	return isIdentStart(r) || (r >= '0' && r <= '9')
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
		// These sit after the root value entirely; keep them separate from
		// a container root's inside-the-bracket foot comments so the emitter
		// does not print both slices twice.
		root.AfterComments = footComments
	}
	if p.peek().Kind != tokEOF {
		return nil, fmt.Errorf("unexpected token %q after root value at offset %d", p.peek().Raw, p.peek().Offset)
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
	case tokString, tokNumber:
		p.advance()
		return &json5Node{Kind: json5Scalar, Raw: t.Raw}, nil
	case tokIdent:
		// JSON5 gives a meaning to exactly five bare words. Every other
		// identifier in value position is an unquoted string, which no JSON5
		// reader accepts: titanous/json5, the one apply / plan / validate
		// read a recipe with, stops at its first letter. Carrying `web`
		// where `"web"` was meant let a recipe format cleanly and then fail
		// to load (#537).
		if !isJSON5Keyword(t.Raw) {
			return nil, fmt.Errorf("unquoted value %q at offset %d is not valid json5", t.Raw, t.Offset)
		}
		p.advance()
		return &json5Node{Kind: json5Scalar, Raw: t.Raw}, nil
	}
	return nil, fmt.Errorf("unexpected token %q while parsing value at offset %d", t.Raw, t.Offset)
}

// isJSON5Keyword reports whether s is one of the bare words JSON5 gives a
// meaning to in value position. A signed Infinity is not here because the
// lexer reads it as a single number token instead.
func isJSON5Keyword(s string) bool {
	switch s {
	case "true", "false", "null", "Infinity", "NaN":
		return true
	}
	return false
}

func (p *json5Parser) parseObject() (*json5Node, error) {
	if p.peek().Kind != tokLBrace {
		return nil, fmt.Errorf("expected { at object start, got %q at offset %d", p.peek().Raw, p.peek().Offset)
	}
	p.advance()
	node := &json5Node{Kind: json5Object}
	// Comments consumed while looking for the separator after a member;
	// they belong to whatever comes next, so they are held over to the top
	// of the following iteration.
	var pending []string
	for {
		head := append(pending, p.consumeComments()...)
		pending = nil
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
			return nil, fmt.Errorf("expected : after key %q, got %q at offset %d", key, p.peek().Raw, p.peek().Offset)
		}
		p.advance()
		val, err := p.parseValue()
		if err != nil {
			return nil, err
		}
		member := &json5Member{Key: key, Value: val, HeadComments: head}
		// Trailing comment on the same line as the value.
		member.LineComment = p.consumeTrailingLineComment()
		// JSON5 makes only the trailing comma optional; a missing separator
		// is an error. titanous/json5, which apply / plan / validate read a
		// recipe with, rejects it too, so accepting it here meant a recipe
		// could pass `docket fmt` and then fail to load (#537).
		if p.peek().Kind == tokComma {
			p.advance()
			if member.LineComment == "" {
				member.LineComment = p.consumeTrailingLineComment()
			}
		} else {
			// A comment may sit between the member and its comma, or between
			// the last member and the closing brace; either way it belongs to
			// what follows, not to the member just parsed.
			pending = p.consumeComments()
			if p.peek().Kind == tokComma {
				p.advance()
			} else if p.peek().Kind != tokRBrace {
				t := p.peek()
				return nil, fmt.Errorf("expected , or } after object member, got %q at offset %d", t.Raw, t.Offset)
			}
		}
		node.Members = append(node.Members, member)
	}
}

func (p *json5Parser) parseArray() (*json5Node, error) {
	if p.peek().Kind != tokLBracket {
		return nil, fmt.Errorf("expected [ at array start, got %q at offset %d", p.peek().Raw, p.peek().Offset)
	}
	p.advance()
	node := &json5Node{Kind: json5Array}
	// See parseObject: comments found while looking for the separator are
	// held over as the next element's head comments.
	var pending []string
	for {
		head := append(pending, p.consumeComments()...)
		pending = nil
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
		} else {
			pending = p.consumeComments()
			if p.peek().Kind == tokComma {
				p.advance()
			} else if p.peek().Kind != tokRBracket {
				t := p.peek()
				return nil, fmt.Errorf("expected , or ] after array element, got %q at offset %d", t.Raw, t.Offset)
			}
		}
		node.Elements = append(node.Elements, elem)
	}
}

// parseKey reads an object key: a quoted string or an unquoted identifier,
// which is the whole of what JSON5 allows. A number is not a key - titanous
// /json5 refuses `{1: "x"}`, and the emitter already writes a numeric key
// quoted, so nothing docket produces changes shape (#537).
func (p *json5Parser) parseKey() (string, error) {
	t := p.advance()
	switch t.Kind {
	case tokString:
		decoded, ok := decodeJSON5String(t.Raw)
		if !ok {
			return "", fmt.Errorf("invalid string key %q at offset %d", t.Raw, t.Offset)
		}
		return decoded, nil
	case tokIdent:
		return t.Raw, nil
	}
	return "", fmt.Errorf("expected key, got %q at offset %d", t.Raw, t.Offset)
}

// ---------------------------------------------------------------------
// Emitter
// ---------------------------------------------------------------------

const json5Indent = "  "

func emitJSON5(buf *bytes.Buffer, node *json5Node, depth int) {
	emitJSON5HeadComments(buf, node.HeadComments, depth)
	// A container's foot comments are emitted inside its brackets by
	// emitJSON5Value; only the root's after-value comments are emitted here,
	// so they are never printed twice.
	emitJSON5Value(buf, node, depth)
	emitJSON5FootComments(buf, node.AfterComments, depth)
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

// quoteJSON5String wraps s (already-decoded character content) in double
// quotes, re-escaping only what must be escaped: the backslash and quote,
// the common control characters as their short escapes, and any other
// sub-U+0020 control as \uXXXX. Printable runes, including non-ASCII such
// as é, are written verbatim as UTF-8. Used for object keys and for
// single-quoted string values converted to double quotes by
// canonicaliseScalarRaw.
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
		case '\b':
			b.WriteString(`\b`)
		case '\f':
			b.WriteString(`\f`)
		default:
			if r < 0x20 {
				fmt.Fprintf(&b, `\u%04x`, r)
			} else {
				b.WriteRune(r)
			}
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

// refuseJSON5UnportableQuoting rejects JSON5 source holding a single-quoted
// interpolation, which every rewrite of it - formatting in place, or
// converting to YAML - would leave double-quoted.
//
// It is called from the two entry points a recipe can reach from `docket
// fmt`, and from neither of the ones the loader uses: ToYAML and Lint read
// a recipe for `validate`, `plan` and `apply`, which render before they
// parse and so do not care how the file is spelled. This is a fmt rule.
func refuseJSON5UnportableQuoting(data []byte) error {
	sites, err := json5QuotingSites(data)
	if err != nil {
		// The caller has already parsed, or is about to; a lex failure
		// here is that same problem and is better reported by the parser,
		// which says where it is.
		return nil
	}
	if len(sites) == 0 {
		return nil
	}
	return unportableQuotingError(sites)
}
