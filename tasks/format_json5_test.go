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
// parseArray does not require a comma between elements, so nothing
// rejected the pair and `[-Infinity]` silently parsed as two elements.
func TestLexJSON5SignedNonFiniteNumbers(t *testing.T) {
	toks, err := lexJSON5([]byte("[-Infinity, +Infinity, Infinity, -NaN, NaN]"))
	if err != nil {
		t.Fatalf("lexJSON5: %v", err)
	}
	var values []string
	for _, tok := range toks {
		if tok.Kind == tokNumber || tok.Kind == tokIdent {
			values = append(values, tok.Raw)
		}
	}
	want := []string{"-Infinity", "+Infinity", "Infinity", "-NaN", "NaN"}
	if len(values) != len(want) {
		t.Fatalf("lexed %d value tokens %q, want %d %q", len(values), values, len(want), want)
	}
	for i := range want {
		if values[i] != want[i] {
			t.Errorf("token %d = %q, want %q", i, values[i], want[i])
		}
	}

	node, err := parseJSON5([]byte("[-Infinity, +Infinity, Infinity, -NaN, NaN]"))
	if err != nil {
		t.Fatalf("parseJSON5: %v", err)
	}
	if len(node.Elements) != len(want) {
		t.Errorf("parsed %d elements, want %d", len(node.Elements), len(want))
	}
}

// TestLexJSON5SignedWordPrefixIsNotSwallowed guards the boundary check in
// hasWordAt: an identifier that merely starts with Infinity or NaN is not
// one of them, so the sign is not glued onto it.
//
// The assertion is on the tokens rather than on parseJSON5 returning an
// error, because it does not: the lexer yields "-" and "Infinities" as
// two tokens and parseArray accepts them as two elements, the comma
// between entries being optional. That leniency is #537; what matters here
// is that hasWordAt did not claim the longer word.
func TestLexJSON5SignedWordPrefixIsNotSwallowed(t *testing.T) {
	toks, err := lexJSON5([]byte("-Infinities"))
	if err != nil {
		t.Fatalf("lexJSON5: %v", err)
	}
	for _, tok := range toks {
		if tok.Raw == "-Infinities" {
			t.Errorf("hasWordAt swallowed a longer identifier: %q", tok.Raw)
		}
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
