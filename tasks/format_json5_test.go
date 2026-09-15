package tasks

import (
	"strings"
	"testing"
)

func TestFormatJSON5IdempotentOnCanonicalInput(t *testing.T) {
	in := `[
  {
    name: "api",
    tasks: [
      {
        name: "create",
        dokku_app: {
          app: "api",
        },
      },
    ],
  },
]
`
	out, err := FormatJSON5([]byte(in))
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if string(out) != in {
		t.Errorf("non-idempotent format:\nwant:\n%s\ngot:\n%s", in, string(out))
	}
}

func TestFormatJSON5ReordersPlayKeys(t *testing.T) {
	in := []byte(`[{ tasks: [], inputs: [], name: "api" }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	// name should come first, inputs second, tasks last among canonical keys.
	idxName := strings.Index(string(out), "name:")
	idxInputs := strings.Index(string(out), "inputs:")
	idxTasks := strings.Index(string(out), "tasks:")
	if !(idxName < idxInputs && idxInputs < idxTasks) {
		t.Errorf("canonical key order not enforced:\n%s", out)
	}
}

func TestFormatJSON5ReordersTaskEnvelopeKeys(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "api" }, name: "create" }] }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	idxName := strings.Index(string(out), "name:")
	idxDokku := strings.Index(string(out), "dokku_app:")
	if idxName < 0 || idxDokku < 0 || idxName >= idxDokku {
		t.Errorf("envelope key order not enforced (name should come before task-type):\n%s", out)
	}
}

func TestFormatJSON5PreservesLineComments(t *testing.T) {
	in := []byte(`[
  // top of recipe
  {
    tasks: [
      {
        name: "create", // inline comment
        dokku_app: { app: "api" },
      },
    ],
  },
]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), "// top of recipe") {
		t.Errorf("head comment lost:\n%s", out)
	}
	if !strings.Contains(string(out), "// inline comment") {
		t.Errorf("trailing line comment lost:\n%s", out)
	}
}

func TestFormatJSON5PreservesBlockComments(t *testing.T) {
	in := []byte(`[
  /* preface */
  { tasks: [] },
]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), "/* preface */") {
		t.Errorf("block comment lost:\n%s", out)
	}
}

func TestFormatJSON5BlankLinesBetweenPlays(t *testing.T) {
	in := []byte(`[
  { name: "a", tasks: [] },
  { name: "b", tasks: [] },
]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	// Expect exactly one blank line between the two plays.
	if !strings.Contains(string(out), "},\n\n  {") {
		t.Errorf("blank line between plays missing:\n%s", out)
	}
}

func TestFormatJSON5RejectsInvalidInput(t *testing.T) {
	in := []byte(`[{ tasks: [`)
	_, err := FormatJSON5(in)
	if err == nil {
		t.Fatal("expected error on truncated input")
	}
	if !strings.Contains(err.Error(), "json5 parse error") {
		t.Errorf("error = %q, want json5 parse error", err.Error())
	}
}

func TestFormatJSON5HandlesEmptyArray(t *testing.T) {
	in := []byte(`[]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	got := strings.TrimSpace(string(out))
	if got != "[]" {
		t.Errorf("empty array not preserved: %q", got)
	}
}

func TestFormatJSON5InlinesScalarArrays(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_domains: { app: "api", domains: ["a.example.com", "b.example.com"] } }] }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), `["a.example.com", "b.example.com"]`) {
		t.Errorf("scalar array should be inlined:\n%s", out)
	}
}

func TestFormatJSON5RoundTripsTrailingCommas(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "api" }, }, ], }, ]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	// Re-parse to confirm valid JSON5.
	if _, err := parseJSON5(out); err != nil {
		t.Fatalf("formatted output does not re-parse: %v\n%s", err, out)
	}
}

func TestFormatJSON5SigilTemplatesSurvive(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "{{ .app }}" } }] }]`)
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), `"{{ .app }}"`) {
		t.Errorf("sigil template lost:\n%s", out)
	}
}

// bs is a single backslash. Test inputs that must contain a \uXXXX escape
// are built by concatenating bs with the following characters so the source
// of this file never contains a valid \uXXXX sequence (which a text pipeline
// could fold into the decoded character, silently defeating the test). The
// expected values use the actual decoded characters (e.g. é, 😀).
const bs = "\\"

func TestDecodeJSON5StringUnicodeAndControlEscapes(t *testing.T) {
	cases := []struct {
		name   string
		raw    string
		want   string
		wantOK bool
	}{
		{"unicode bmp single quoted", "'caf" + bs + "u00e9'", "café", true},
		{"unicode bmp double quoted", "\"caf" + bs + "u00e9\"", "café", true},
		{"hex escape", `"\x41"`, "A", true},
		{"backspace formfeed vtab", `'\b\f\v'`, "\b\f\v", true},
		{"nul escape", `'\0'`, "\x00", true},
		{"surrogate pair", "'" + bs + "ud83d" + bs + "ude00'", "😀", true},
		{"simple escapes", `"\t\n\r"`, "\t\n\r", true},
		{"line continuation lf", "'a\\\nb'", "ab", true},
		{"unknown escape is literal", `'\q'`, "q", true},
		{"bad hex", `'\xzz'`, "", false},
		{"lone high surrogate", "'" + bs + "ud83d'", "", false},
		{"truncated unicode", "'" + bs + "u00'", "", false},
		{"nul followed by digit", `'\05'`, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := decodeJSON5String(tc.raw)
			if ok != tc.wantOK {
				t.Fatalf("decodeJSON5String(%q) ok=%v, want %v", tc.raw, ok, tc.wantOK)
			}
			if ok && got != tc.want {
				t.Errorf("decodeJSON5String(%q) = %q, want %q", tc.raw, got, tc.want)
			}
		})
	}
}

func TestQuoteJSON5StringReEncodesControls(t *testing.T) {
	// A decoded string with control characters must round-trip: quoting it
	// and decoding again yields the original content.
	in := "café\t\n\b\f\v\x00\x1f\"back\\slash"
	quoted := quoteJSON5String(in)
	got, ok := decodeJSON5String(quoted)
	if !ok {
		t.Fatalf("decodeJSON5String(%q) failed", quoted)
	}
	if got != in {
		t.Errorf("round-trip mismatch: quoted=%q decoded=%q want=%q", quoted, got, in)
	}
	// A printable non-ASCII rune stays verbatim, not escaped.
	if !strings.Contains(quoted, "café") {
		t.Errorf("expected verbatim UTF-8 in %q", quoted)
	}
}

func TestFormatJSON5ReQuotesUnicodeValueWithoutCorruption(t *testing.T) {
	// Input carries a literal é escape inside a single-quoted string.
	in := []byte("[{ tasks: [{ dokku_app: { app: 'caf" + bs + "u00e9' } }] }]")
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), `"café"`) {
		t.Errorf("expected decoded unicode value in output:\n%s", out)
	}
	if strings.Contains(string(out), bs+"u00e9") {
		t.Errorf("output still carries the raw unicode escape (corruption):\n%s", out)
	}
	// The formatted output must still be valid JSON5.
	if _, err := parseJSON5(out); err != nil {
		t.Fatalf("formatted output does not re-parse: %v\n%s", err, out)
	}
	// Idempotent on the formatted output.
	again, err := FormatJSON5(out)
	if err != nil {
		t.Fatalf("FormatJSON5 second pass: %v", err)
	}
	if string(again) != string(out) {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
}

func TestFormatJSON5DecodesUnicodeKey(t *testing.T) {
	in := []byte("[{ tasks: [{ dokku_config: { 'caf" + bs + "u00e9': \"x\" } }] }]")
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	if !strings.Contains(string(out), "café:") {
		t.Errorf("expected decoded unicode key in output:\n%s", out)
	}
	if strings.Contains(string(out), bs+"u00e9") {
		t.Errorf("output still carries the raw unicode escape in the key:\n%s", out)
	}
}

func TestFormatJSON5RootAfterCommentNotDoubled(t *testing.T) {
	in := []byte("[\n  { name: \"a\" },\n]\n// trailing note\n")
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	body := string(out)
	if got := strings.Count(body, "// trailing note"); got != 1 {
		t.Errorf("after-root comment appears %d times, want 1:\n%s", got, body)
	}
	// It must sit after the closing bracket, not inside the array.
	if strings.Index(body, "// trailing note") < strings.LastIndex(body, "]") {
		t.Errorf("after-root comment placed inside the array:\n%s", body)
	}
	// Idempotent across passes (the doubling bug quadrupled it each run).
	again, err := FormatJSON5(out)
	if err != nil {
		t.Fatalf("FormatJSON5 second pass: %v", err)
	}
	if string(again) != body {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", body, again)
	}
}

func TestFormatJSON5RootInsideAndAfterComments(t *testing.T) {
	in := []byte("[\n  { name: \"a\" },\n  // inside foot\n]\n// after root\n")
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	body := string(out)
	if c := strings.Count(body, "// inside foot"); c != 1 {
		t.Errorf("inside foot comment appears %d times, want 1:\n%s", c, body)
	}
	if c := strings.Count(body, "// after root"); c != 1 {
		t.Errorf("after-root comment appears %d times, want 1:\n%s", c, body)
	}
	// inside foot before the closing bracket, after-root after it.
	idxInside := strings.Index(body, "// inside foot")
	idxBracket := strings.LastIndex(body, "]")
	idxAfter := strings.Index(body, "// after root")
	if !(idxInside < idxBracket && idxBracket < idxAfter) {
		t.Errorf("comment positions wrong (inside=%d bracket=%d after=%d):\n%s", idxInside, idxBracket, idxAfter, body)
	}
	again, err := FormatJSON5(out)
	if err != nil {
		t.Fatalf("FormatJSON5 second pass: %v", err)
	}
	if string(again) != body {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", body, again)
	}
}

// TestLexJSON5SignedNonFiniteNumbers pins the fix for a lexer bug that
// only surfaced once cross-format conversion started emitting JSON5:
// readNumber consumed the sign and then stopped on the "I", handing back
// a lone "-" token with "Infinity" trailing it as a separate identifier.
// parseArray did not require a comma between elements back then, so
// nothing rejected the pair and `[-Infinity]` silently parsed as two
// elements. The separator is required now (#537), which would catch the
// pair a second time; the lexer still has to read it as one token.
func TestLexJSON5SignedNonFiniteNumbers(t *testing.T) {
	toks, err := lexJSON5([]byte("[-Infinity, +Infinity, Infinity, NaN]"))
	if err != nil {
		t.Fatalf("lexJSON5: %v", err)
	}
	var values []string
	for _, tok := range toks {
		if tok.Kind == tokNumber || tok.Kind == tokIdent {
			values = append(values, tok.Raw)
		}
	}
	want := []string{"-Infinity", "+Infinity", "Infinity", "NaN"}
	if len(values) != len(want) {
		t.Fatalf("lexed %d value tokens %q, want %d %q", len(values), values, len(want), want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, values[i], want[i])
		}
	}

	node, err := parseJSON5([]byte("[-Infinity, +Infinity, Infinity, NaN]"))
	if err != nil {
		t.Fatalf("parseJSON5: %v", err)
	}
	if len(node.Elements) != len(want) {
		t.Errorf("parsed %d elements, want %d", len(node.Elements), len(want))
	}

	// A signed NaN is the one spelling that does not come along. JSON5
	// allows it and titanous/json5 does not, and the loader has the last
	// word on what a recipe may say.
	if _, err := parseJSON5([]byte("[-NaN]")); err == nil {
		t.Error("parseJSON5(\"[-NaN]\") = nil error, want the loader's rejection")
	}
}

// TestLexJSON5SignedWordPrefixIsNotSwallowed guards the boundary check in
// hasWordAt: an identifier that merely starts with Infinity or NaN is not
// one of them, so the sign is not glued onto it.
//
// A sign followed by a longer word is no longer a number at all: the sign
// is consumed, the boundary check refuses the word, and the digit scan
// finds nothing, so the whole thing is an invalid number rather than two
// tokens the parser would have to reject separately (#537).
func TestLexJSON5SignedWordPrefixIsNotSwallowed(t *testing.T) {
	if _, err := lexJSON5([]byte("-Infinities")); err == nil {
		t.Error("lexJSON5(\"-Infinities\") = nil error, want an invalid number")
	}

	// The other side of the boundary: the exact word still reads as one
	// signed number token.
	toks, err := lexJSON5([]byte("-Infinity"))
	if err != nil {
		t.Fatalf("lexJSON5: %v", err)
	}
	if len(toks) != 2 || toks[0].Kind != tokNumber || toks[0].Raw != "-Infinity" {
		t.Errorf("lexed %+v, want a single -Infinity number token", toks)
	}
}

// TestFormatJSON5RoundTripsNonFiniteNumbers is the formatter-level half:
// the canonical form keeps the signed spellings and is idempotent.
func TestFormatJSON5RoundTripsNonFiniteNumbers(t *testing.T) {
	in := []byte("[-Infinity, Infinity, NaN]\n")
	out, err := FormatJSON5(in)
	if err != nil {
		t.Fatalf("FormatJSON5: %v", err)
	}
	for _, want := range []string{"-Infinity", "Infinity", "NaN"} {
		if !strings.Contains(string(out), want) {
			t.Errorf("output missing %q:\n%s", want, out)
		}
	}
	again, err := FormatJSON5(out)
	if err != nil {
		t.Fatalf("FormatJSON5 second pass: %v", err)
	}
	if string(again) != string(out) {
		t.Errorf("not idempotent:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
}

// TestParseJSON5RequiresSeparatorBetweenEntries pins #537: the comma
// between two entries is not optional. parseArray and parseObject used to
// consume one only if it happened to be there, so `[1 2]` read as a
// two-element array and `{ a: 1 b: 2 }` as a two-member object - documents
// titanous/json5 rejects, which is what apply / plan / validate read a
// recipe with. A file could format cleanly and then fail to load.
func TestParseJSON5RequiresSeparatorBetweenEntries(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"array scalars", "[1 2]", "expected , or ]"},
		{"object members", "{ a: 1 b: 2 }", "expected , or }"},
		{"array objects", "[{a: 1} {b: 2}]", "expected , or ]"},
		{"after one good comma", "[1, 2 3]", "expected , or ]"},
		{"nested array", "[[1 2]]", "expected , or ]"},
		{"object value then member", "{a: {b: 1} c: 2}", "expected , or }"},
		{"comment between entries", "[\n1\n// note\n2\n]", "expected , or ]"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := parseJSON5([]byte(tc.in))
			if err == nil {
				t.Fatalf("parseJSON5(%q) = nil error, want a missing-separator error", tc.in)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to contain %q", err.Error(), tc.want)
			}
			if !strings.Contains(err.Error(), "at offset ") {
				t.Errorf("error = %q, want it to name an offset", err.Error())
			}
		})
	}

	// The offset is the one the reader needs: where the entry that should
	// have been preceded by a comma starts.
	_, err := parseJSON5([]byte("[1 2]"))
	if err == nil {
		t.Fatal("parseJSON5 = nil error")
	}
	if !strings.Contains(err.Error(), `got "2" at offset 3`) {
		t.Errorf("error = %q, want it to name the second element at offset 3", err.Error())
	}
}

// TestParseJSON5AcceptsValidSeparatorForms is the other half: requiring the
// separator must not cost the trailing comma JSON5 does make optional, nor
// any of the places a comment is allowed to sit around one.
func TestParseJSON5AcceptsValidSeparatorForms(t *testing.T) {
	cases := []struct {
		name         string
		in           string
		wantElements int
		wantMembers  int
	}{
		{"plain", "[1, 2]", 2, 0},
		{"trailing comma", "[1, 2,]", 2, 0},
		{"object trailing comma", "{a: 1, b: 2,}", 0, 2},
		{"empty array", "[]", 0, 0},
		{"empty object", "{}", 0, 0},
		{"trailing comma then comment", "[1, /* c */]", 1, 0},
		{"comment after last element", "[\n 1\n // note\n]", 1, 0},
		{"comment before comma same line", "[1 /* c */, 2]", 2, 0},
		{"comment before comma own line", "[1\n/* c */\n, 2]", 2, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			node, err := parseJSON5([]byte(tc.in))
			if err != nil {
				t.Fatalf("parseJSON5(%q): %v", tc.in, err)
			}
			if len(node.Elements) != tc.wantElements {
				t.Errorf("parsed %d elements, want %d", len(node.Elements), tc.wantElements)
			}
			if len(node.Members) != tc.wantMembers {
				t.Errorf("parsed %d members, want %d", len(node.Members), tc.wantMembers)
			}
		})
	}

	// A comment held over while the parser looked for the separator still
	// reaches the output, wherever it sat relative to the comma.
	for _, in := range []string{"[1, /* c */]", "[\n 1\n // note\n]", "[1\n/* c */\n, 2]"} {
		out, err := FormatJSON5([]byte(in))
		if err != nil {
			t.Fatalf("FormatJSON5(%q): %v", in, err)
		}
		if !strings.Contains(string(out), "c */") && !strings.Contains(string(out), "// note") {
			t.Errorf("comment lost formatting %q:\n%s", in, out)
		}
	}
}

// TestFormatJSON5RejectsMissingComma is the formatter-level half of #537:
// the missing separator surfaces as the parse error `docket fmt` prints,
// rather than as a silently rewritten file.
func TestFormatJSON5RejectsMissingComma(t *testing.T) {
	in := []byte(`[{ tasks: [{ dokku_app: { app: "a" } } { dokku_app: { app: "b" } }] }]`)
	_, err := FormatJSON5(in)
	if err == nil {
		t.Fatal("expected an error on a missing comma between task entries")
	}
	if !strings.Contains(err.Error(), "json5 parse error") {
		t.Errorf("error = %q, want json5 parse error", err.Error())
	}
}

// TestParseJSON5RejectsMalformedNumbers covers the other half of #537's
// leniency: readNumber used to hand back a token whenever it had consumed
// any byte at all, so a lone sign, a digitless 0x, a second decimal point
// and a bare exponent all lexed as numbers that `docket fmt` would rewrite
// verbatim and titanous/json5 would then refuse.
//
// Some rows fail in the lexer and some in the parser - 1.2.3 and 01 now lex
// as two adjacent number tokens, which the separator rule rejects - so
// every row asserts on parseJSON5, which is where a caller meets either.
func TestParseJSON5RejectsMalformedNumbers(t *testing.T) {
	for _, in := range []string{
		"[-]", "[+]", "[.]", "[0x]", "[0X]", "[1e]", "[1e+]", "[--5]",
		"[-NaN]", "[+NaN]", "[1.2.3]", "[01]", "[1-2]",
	} {
		t.Run(in, func(t *testing.T) {
			_, err := parseJSON5([]byte(in))
			if err == nil {
				t.Fatalf("parseJSON5(%s) = nil error, want a rejection", in)
			}
			if !strings.Contains(err.Error(), "at offset ") {
				t.Errorf("error = %q, want it to name an offset", err.Error())
			}
		})
	}
}

// TestLexJSON5AcceptsJSON5NumberForms is the guard on the other side of the
// tightening: every spelling document_json5.go already types has to keep
// lexing as exactly one value token. Infinity and NaN arrive unsigned as
// identifiers, which is how the lexer has always handed them over.
func TestLexJSON5AcceptsJSON5NumberForms(t *testing.T) {
	for _, in := range []string{
		"0", "-0", "42", "-42", "+7", "0x1F", "-0x10", "0X0A",
		"1.5", "-0.25", ".5", "5.", "1e3", "1.5e-3", "1E+3", "0.0",
		"Infinity", "+Infinity", "-Infinity", "NaN",
	} {
		t.Run(in, func(t *testing.T) {
			toks, err := lexJSON5([]byte(in))
			if err != nil {
				t.Fatalf("lexJSON5(%s): %v", in, err)
			}
			if len(toks) != 2 {
				t.Fatalf("lexed %d tokens %+v, want one value token and EOF", len(toks), toks)
			}
			if toks[0].Kind != tokNumber && toks[0].Kind != tokIdent {
				t.Errorf("token kind = %d, want a number or identifier", toks[0].Kind)
			}
			if toks[0].Raw != in {
				t.Errorf("token = %q, want %q", toks[0].Raw, in)
			}
		})
	}
}

// TestParseJSON5RejectsUnquotedValues is the third shape of #537's
// leniency: parseValue took any identifier as a scalar, so `{app: web}`
// parsed and `docket fmt` rewrote it, while titanous/json5 stops at the "w"
// and answers with a message about the literal false. Only the bare words
// JSON5 actually defines are values.
func TestParseJSON5RejectsUnquotedValues(t *testing.T) {
	for _, in := range []string{`{app: web}`, `[undefined]`, `[web, "x"]`, `{a: 1, b: two}`} {
		t.Run(in, func(t *testing.T) {
			_, err := parseJSON5([]byte(in))
			if err == nil {
				t.Fatalf("parseJSON5(%s) = nil error, want a rejection", in)
			}
			if !strings.Contains(err.Error(), "is not valid json5") {
				t.Errorf("error = %q, want it to name the unquoted value", err.Error())
			}
			if !strings.Contains(err.Error(), "at offset ") {
				t.Errorf("error = %q, want it to name an offset", err.Error())
			}
		})
	}

	// The five words that are values keep parsing, and an unquoted key is
	// still an unquoted key - parseKey reads those, not parseValue.
	node, err := parseJSON5([]byte(`[true, false, null, Infinity, NaN]`))
	if err != nil {
		t.Fatalf("parseJSON5 on the JSON5 keywords: %v", err)
	}
	if len(node.Elements) != 5 {
		t.Errorf("parsed %d elements, want 5", len(node.Elements))
	}
	if _, err := parseJSON5([]byte(`{web: "x"}`)); err != nil {
		t.Errorf("unquoted key should still parse: %v", err)
	}
}
