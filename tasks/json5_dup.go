package tasks

import "strconv"

// jsonDupKey describes a duplicate object key found in a JSON5 document.
type jsonDupKey struct {
	Key    string
	Line   int
	Column int
}

// detectJSON5DuplicateKeys scans JSON5 bytes for an object key that appears
// more than once within the same object and returns the first such
// duplicate (nil when there is none).
//
// titanous/json5 decodes duplicate keys last-wins with no error, unlike
// yaml.v3 which rejects them outright. Without this scan a copy-pasted
// task-type key in a JSON5 recipe would be silently deduped on both the
// validate and apply paths. The input is assumed syntactically valid
// (normalizeRecipeBytes runs the real json5 parse first); on any
// unexpected byte the scan stops and returns nil, deferring to the real
// parser's error.
func detectJSON5DuplicateKeys(data []byte) *jsonDupKey {
	s := &json5Scanner{data: data, line: 1, col: 1}
	s.parseValue()
	return s.dup
}

// json5Scanner is a minimal JSON5 walker whose only job is to find a
// duplicated object key. It tracks nothing about values beyond skipping
// past them so nested braces / colons inside strings and comments do not
// confuse the object-key detection.
type json5Scanner struct {
	data []byte
	pos  int
	line int
	col  int
	dup  *jsonDupKey
}

func (s *json5Scanner) atEnd() bool { return s.pos >= len(s.data) }

func (s *json5Scanner) peek() byte {
	if s.atEnd() {
		return 0
	}
	return s.data[s.pos]
}

func (s *json5Scanner) peekAt(offset int) byte {
	if s.pos+offset >= len(s.data) {
		return 0
	}
	return s.data[s.pos+offset]
}

func (s *json5Scanner) advance() byte {
	c := s.data[s.pos]
	s.pos++
	if c == '\n' {
		s.line++
		s.col = 1
	} else {
		s.col++
	}
	return c
}

// skipWsComments consumes whitespace and JSON5 line / block comments.
func (s *json5Scanner) skipWsComments() {
	for !s.atEnd() && s.dup == nil {
		switch c := s.peek(); {
		case c == ' ' || c == '\t' || c == '\r' || c == '\n' || c == '\f' || c == '\v':
			s.advance()
		case c == '/' && s.peekAt(1) == '/':
			for !s.atEnd() && s.peek() != '\n' {
				s.advance()
			}
		case c == '/' && s.peekAt(1) == '*':
			s.advance()
			s.advance()
			for !s.atEnd() {
				if s.peek() == '*' && s.peekAt(1) == '/' {
					s.advance()
					s.advance()
					break
				}
				s.advance()
			}
		default:
			return
		}
	}
}

// parseValue dispatches on the next non-trivia byte.
func (s *json5Scanner) parseValue() {
	if s.dup != nil {
		return
	}
	s.skipWsComments()
	if s.atEnd() {
		return
	}
	switch s.peek() {
	case '{':
		s.parseObject()
	case '[':
		s.parseArray()
	case '"', '\'':
		s.readString()
	default:
		s.skipPrimitive()
	}
}

// parseObject consumes an object and reports the first key that repeats
// within it. Nested objects recurse and get their own key set.
func (s *json5Scanner) parseObject() {
	s.advance() // consume '{'
	seen := map[string]bool{}
	for s.dup == nil {
		s.skipWsComments()
		if s.atEnd() {
			return
		}
		if s.peek() == '}' {
			s.advance()
			return
		}
		keyLine, keyCol := s.line, s.col
		key, ok := s.readKey()
		if !ok {
			return
		}
		if seen[key] {
			s.dup = &jsonDupKey{Key: key, Line: keyLine, Column: keyCol}
			return
		}
		seen[key] = true

		s.skipWsComments()
		if s.peek() != ':' {
			return
		}
		s.advance() // consume ':'
		s.parseValue()

		s.skipWsComments()
		switch s.peek() {
		case ',':
			s.advance()
		case '}':
			s.advance()
			return
		default:
			return
		}
	}
}

// parseArray consumes an array, recursing into each element.
func (s *json5Scanner) parseArray() {
	s.advance() // consume '['
	for s.dup == nil {
		s.skipWsComments()
		if s.atEnd() {
			return
		}
		if s.peek() == ']' {
			s.advance()
			return
		}
		s.parseValue()

		s.skipWsComments()
		switch s.peek() {
		case ',':
			s.advance()
		case ']':
			s.advance()
			return
		default:
			return
		}
	}
}

// readKey reads an object key: a quoted string or an unquoted identifier.
// The returned key is the canonical (unquoted, unescaped) form so a
// quoted and an unquoted spelling of the same key compare equal. ok is
// false on malformed input so the caller can defer to the real parser.
func (s *json5Scanner) readKey() (string, bool) {
	switch s.peek() {
	case '"', '\'':
		return s.readString(), true
	}
	start := s.pos
	for !s.atEnd() {
		switch c := s.peek(); c {
		case ':', ' ', '\t', '\r', '\n', '\f', '\v', '/':
			goto done
		default:
			s.advance()
		}
	}
done:
	if s.pos == start {
		return "", false
	}
	return string(s.data[start:s.pos]), true
}

// readString consumes a quoted string (single or double quote) and returns
// its decoded content. Only escape handling that matters for correctly
// finding the closing quote is guaranteed; other escapes decode
// best-effort, which is sufficient for key comparison.
func (s *json5Scanner) readString() string {
	quote := s.advance() // consume opening quote
	var b []byte
	for !s.atEnd() {
		c := s.advance()
		if c == '\\' {
			if s.atEnd() {
				break
			}
			esc := s.advance()
			switch esc {
			case 'n':
				b = append(b, '\n')
			case 't':
				b = append(b, '\t')
			case 'r':
				b = append(b, '\r')
			case 'b':
				b = append(b, '\b')
			case 'f':
				b = append(b, '\f')
			case '\n', '\r':
				// line continuation: the newline is elided
			case 'u':
				if s.pos+4 <= len(s.data) {
					if r, err := strconv.ParseInt(string(s.data[s.pos:s.pos+4]), 16, 32); err == nil {
						for i := 0; i < 4; i++ {
							s.advance()
						}
						b = append(b, []byte(string(rune(r)))...)
						continue
					}
				}
				b = append(b, esc)
			default:
				b = append(b, esc)
			}
			continue
		}
		if c == quote {
			break
		}
		b = append(b, c)
	}
	return string(b)
}

// skipPrimitive consumes a bare scalar (number, true / false / null,
// Infinity, NaN, ...) up to the next structural delimiter.
func (s *json5Scanner) skipPrimitive() {
	for !s.atEnd() {
		switch s.peek() {
		case ',', '}', ']', ' ', '\t', '\r', '\n', '\f', '\v', '/':
			return
		default:
			s.advance()
		}
	}
}
