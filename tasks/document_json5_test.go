package tasks

import (
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// TestJSON5ScalarToYAMLNode is the JSON5 side of the scalar mapping. The
// lookalike strings are the rows that matter most: a quoted "true" or "5"
// has to arrive tagged !!str, or the YAML emitter writes it bare and the
// recipe quietly changes type.
func TestJSON5ScalarToYAMLNode(t *testing.T) {
	t.Parallel()

	cases := []struct {
		raw     string
		wantTag string
		wantVal string
		// forcedQuotes marks the rows yamlStringNode has to override the
		// style for: a value yaml.v3 would emit plain and then misread on
		// the way back, and any value holding a sigil template, whose
		// quote characters are part of what the recipe means. Every other
		// row leaves the choice to the emitter, which makes a better one
		// than this code could.
		forcedQuotes bool
	}{
		{`"web"`, "!!str", "web", false},
		{`'web'`, "!!str", "web", false},
		{`""`, "!!str", "", false},
		{`"true"`, "!!str", "true", false},
		{`"5"`, "!!str", "5", false},
		{`"null"`, "!!str", "null", false},
		{`"~"`, "!!str", "~", false},
		{`"<<"`, "!!str", "<<", true},
		{`"0x1F"`, "!!str", "0x1F", false},
		{`"2015-01-01"`, "!!str", "2015-01-01", false},
		{`"{{ .app }}"`, "!!str", "{{ .app }}", true},
		{`"first\nsecond"`, "!!str", "first\nsecond", false},
		{`"tab\there"`, "!!str", "tab\there", false},
		{`"café"`, "!!str", "café", false},
		{"true", "!!bool", "true", false},
		{"false", "!!bool", "false", false},
		{"null", "!!null", "null", false},
		{"0", "!!int", "0", false},
		{"42", "!!int", "42", false},
		{"-42", "!!int", "-42", false},
		{"+7", "!!int", "7", false},
		{"0x1F", "!!int", "31", false},
		{"-0x10", "!!int", "-16", false},
		{"0X0A", "!!int", "10", false},
		{"1.5", "!!float", "1.5", false},
		{"-0.25", "!!float", "-0.25", false},
		{".5", "!!float", "0.5", false},
		{"5.", "!!float", "5.0", false},
		{"1e3", "!!float", "1000.0", false},
		{"1.5e-3", "!!float", "0.0015", false},
		{"Infinity", "!!float", ".inf", false},
		{"+Infinity", "!!float", ".inf", false},
		{"-Infinity", "!!float", "-.inf", false},
		{"NaN", "!!float", ".nan", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.raw, func(t *testing.T) {
			t.Parallel()
			node, err := json5ScalarToYAMLNode(tc.raw)
			if err != nil {
				t.Fatalf("json5ScalarToYAMLNode(%s): %v", tc.raw, err)
			}
			if node.Tag != tc.wantTag {
				t.Errorf("tag = %q, want %q", node.Tag, tc.wantTag)
			}
			if node.Value != tc.wantVal {
				t.Errorf("value = %q, want %q", node.Value, tc.wantVal)
			}
			wantStyle := yaml.Style(0)
			if tc.forcedQuotes {
				wantStyle = yaml.DoubleQuotedStyle
			}
			if node.Style != wantStyle {
				t.Errorf("style = %d, want %d", node.Style, wantStyle)
			}
		})
	}
}

// TestJSON5ScalarToYAMLNodeRejects covers the tokens the lenient JSON5
// parser accepts but no JSON5 reader does.
func TestJSON5ScalarToYAMLNodeRejects(t *testing.T) {
	t.Parallel()
	for _, raw := range []string{"web", "undefined", "0x", "-", ""} {
		if node, err := json5ScalarToYAMLNode(raw); err == nil {
			t.Errorf("json5ScalarToYAMLNode(%q) = %v, want an error", raw, node)
		}
	}
}

// TestJSON5LookalikeStringsStayQuoted is the end-to-end version of the
// rows above: the emitted YAML has to quote them, and reading it back has
// to give strings.
func TestJSON5LookalikeStringsStayQuoted(t *testing.T) {
	t.Parallel()

	in := []byte(`{a: "true", b: "5", c: "null", d: "~", e: "", f: "2015-01-01", g: "<<"}`)
	out, err := Convert(in, CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}

	var decoded map[string]interface{}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	want := map[string]string{"a": "true", "b": "5", "c": "null", "d": "~", "e": "", "f": "2015-01-01", "g": "<<"}
	for key, wantValue := range want {
		got, ok := decoded[key].(string)
		if !ok {
			t.Errorf("%s = %#v (%T), want the string %q\noutput:\n%s", key, decoded[key], decoded[key], wantValue, out)
			continue
		}
		if got != wantValue {
			t.Errorf("%s = %q, want %q", key, got, wantValue)
		}
	}
}

// TestYAMLScalarToJSON5Raw is the YAML side. The integer rows are the ones
// that earn their keep: YAML accepts four spellings JSON5 has never heard
// of, and every one has to come out as a plain decimal.
func TestYAMLScalarToJSON5Raw(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain string", "k: web", `"web"`},
		{"quoted number", `k: "5"`, `"5"`},
		{"quoted bool", `k: "true"`, `"true"`},
		{"empty string", `k: ""`, `""`},
		{"sigil template", `k: '{{ .app }}'`, `"{{ .app }}"`},
		{"escaped sigil", `k: "{{ .app | dq }}"`, `"{{ .app | dq }}"`},
		{"string with quote", `k: 'say "hi"'`, `"say \"hi\""`},
		{"block scalar", "k: |\n  first\n  second\n", `"first\nsecond\n"`},
		{"folded scalar", "k: >\n  first\n  second\n", `"first second\n"`},
		{"bool true", "k: true", "true"},
		{"bool yes uppercase", "k: TRUE", "true"},
		{"bool false", "k: false", "false"},
		{"null tilde", "k: ~", "null"},
		{"null word", "k: null", "null"},
		{"empty value", "k:", "null"},
		{"int", "k: 42", "42"},
		{"negative int", "k: -42", "-42"},
		{"hex int", "k: 0x1F", "31"},
		{"octal int", "k: 0o17", "15"},
		{"underscored int", "k: 1_000", "1000"},
		{"signed int", "k: +5", "5"},
		{"float", "k: 1.5", "1.5"},
		{"float trailing zero", "k: 1.50", "1.5"},
		{"exponent", "k: 1e3", "1000"},
		{"infinity", "k: .inf", "Infinity"},
		{"negative infinity", "k: -.inf", "-Infinity"},
		{"nan", "k: .nan", "NaN"},
		{"timestamp widens", "k: 2015-01-01", `"2015-01-01"`},
		{"emoji", `k: "hi 🎉"`, `"hi 🎉"`},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var doc yaml.Node
			if err := yaml.Unmarshal([]byte(tc.in), &doc); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			value := documentBody(&doc).Content[1]
			got, err := yamlScalarToJSON5Raw(value)
			if err != nil {
				t.Fatalf("yamlScalarToJSON5Raw: %v", err)
			}
			if got != tc.want {
				t.Errorf("yamlScalarToJSON5Raw(%s) = %s, want %s (tag %s)", tc.in, got, tc.want, value.Tag)
			}
		})
	}
}

// TestYAMLScalarToJSON5RawRejectsCustomTag keeps an unrecognised tag from
// being silently stringified.
func TestYAMLScalarToJSON5RawRejectsCustomTag(t *testing.T) {
	t.Parallel()
	var doc yaml.Node
	if err := yaml.Unmarshal([]byte("k: !Foo bar"), &doc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if raw, err := yamlScalarToJSON5Raw(documentBody(&doc).Content[1]); err == nil {
		t.Errorf("yamlScalarToJSON5Raw of a !Foo scalar = %s, want an error", raw)
	}
}

// TestCommentTranslation covers the delimiter mapping in both directions.
func TestCommentTranslation(t *testing.T) {
	t.Parallel()

	t.Run("json5 to yaml", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			in   []string
			want string
		}{
			{"line comment", []string{"// hello"}, "# hello"},
			{"no space", []string{"//hello"}, "# hello"},
			{"two lines", []string{"// a", "// b"}, "# a\n# b"},
			{"block comment", []string{"/* a\n   b */"}, "# a\n#   b"},
			{"blank interior line", []string{"/* a\n\nb */"}, "# a\n#\n# b"},
			{"empty token", []string{""}, ""},
		}
		for _, tc := range cases {
			if got := json5CommentsToYAML(tc.in); got != tc.want {
				t.Errorf("%s: json5CommentsToYAML(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
			}
		}
	})

	t.Run("yaml to json5", func(t *testing.T) {
		t.Parallel()
		cases := []struct {
			name string
			in   string
			want []string
		}{
			{"one line", "# hello", []string{"// hello"}},
			{"two lines", "# a\n# b", []string{"// a", "// b"}},
			{"no hash", "bare", []string{"// bare"}},
			{"empty", "", nil},
		}
		for _, tc := range cases {
			got := yamlCommentToJSON5(tc.in)
			if strings.Join(got, "|") != strings.Join(tc.want, "|") {
				t.Errorf("%s: yamlCommentToJSON5(%q) = %q, want %q", tc.name, tc.in, got, tc.want)
			}
		}
	})
}

// TestBlankCommentLineStaysAHash guards the one comment rule that is not
// cosmetic: an empty line inside a converted comment has to come back as a
// bare "#". Emitted as "", the YAML writer produces a real blank line,
// which ends the comment on re-parse and splits one head comment in two -
// so the converted recipe would stop being idempotent under `docket fmt`.
func TestBlankCommentLineStaysAHash(t *testing.T) {
	t.Parallel()

	in := []byte("/* first\n\nsecond */\n[{ name: \"web\", tasks: [] }]\n")
	out, err := Convert(in, CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if strings.Contains(string(out), "\n\n#") {
		t.Errorf("a blank line split the head comment:\n%s", out)
	}
	again, err := CodecFor(FormatYAML).Format(out)
	if err != nil {
		t.Fatalf("Format: %v", err)
	}
	if string(again) != string(out) {
		t.Errorf("converted output is not fmt-stable:\nfirst:\n%s\nsecond:\n%s", out, again)
	}
}

// TestKeyLineCommentLandsAfterColon pins the choice to hang a member's
// trailing comment on the key node. yaml.v3 replays a key's line comment
// beside a scalar value and directly after the colon for a block one; on
// the value node instead, a container's comment would be pushed down past
// the whole block it was introducing.
func TestKeyLineCommentLandsAfterColon(t *testing.T) {
	t.Parallel()

	in := []byte("[{ name: \"web\", tasks: [{ name: \"a\" }], // about the tasks\n}]\n")
	out, err := Convert(in, CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(string(out), "tasks: # about the tasks") {
		t.Errorf("comment did not land beside the tasks key:\n%s", out)
	}
}

// TestMergeTokenStringSurvives pins the one value the !!str tag does not
// protect on its own. yaml.v3 writes a plain `g: <<`, which reads back as
// !!merge, so yamlStringNode has to force quotes around it.
//
// Before that fix the recipe was not corrupted - the YAML formatter's own
// round-trip guard caught it - but the conversion was refused outright,
// which is a poor answer for a recipe that merely mentions "<<".
func TestMergeTokenStringSurvives(t *testing.T) {
	t.Parallel()

	out, err := Convert([]byte(`{ g: "<<" }`), CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	var decoded map[string]interface{}
	if err := yaml.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if got, ok := decoded["g"].(string); !ok || got != "<<" {
		t.Errorf("g = %#v, want the string \"<<\" (output: %q)", decoded["g"], out)
	}
}

// TestOrdinaryStringsAreNotOverQuoted guards against over-correcting the
// forced-quote rules: a value the emitter already handles keeps the style
// it picked, so converted YAML does not come out wrapped in quotes it
// never needed.
func TestOrdinaryStringsAreNotOverQuoted(t *testing.T) {
	t.Parallel()

	out, err := Convert([]byte(`{ app: "web", state: "present" }`), CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if want := "app: web\nstate: present\n"; string(out) != want {
		t.Errorf("ordinary strings were quoted:\nwant:\n%s\ngot:\n%s", want, out)
	}
}

// TestConvertKeepsDqInterpolationsDoubleQuoted is a semantic regression
// test, not a formatting one. A recipe is rendered as text and only then
// parsed, so the quotes around an interpolation decide how the value is
// escaped: `dq` emits JSON-style escaping, which a double-quoted YAML
// scalar undoes and a single-quoted one does not.
//
// Left to its own judgement the YAML emitter single-quotes a value opening
// with `{`, which turned `"{{ .app | dq }}"` into `'{{ .app | dq }}'` and
// silently rendered `we"b` as the literal `we\"b`. `docket init`'s own
// scaffold uses `dq`, so a round trip through JSON5 corrupted it.
func TestConvertKeepsDqInterpolationsDoubleQuoted(t *testing.T) {
	t.Parallel()

	in := []byte(`[{ name: "web", tasks: [{ dokku_app: { app: "{{ .app | dq }}" } }] }]`)
	out, err := Convert(in, CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if !strings.Contains(string(out), `"{{ .app | dq }}"`) {
		t.Fatalf("interpolation lost its double quotes:\n%s", out)
	}

	// The assertion that matters is what the value becomes after the render
	// the quotes exist to serve.
	rendered, err := RenderTemplate(out, map[string]interface{}{"app": `we"b`}, "recipe")
	if err != nil {
		t.Fatalf("RenderTemplate: %v", err)
	}
	plays, err := UnmarshalRecipe(rendered.Bytes(), FormatYAML)
	if err != nil {
		t.Fatalf("UnmarshalRecipe: %v\n%s", err, rendered.String())
	}
	if len(plays) != 1 || len(plays[0].Tasks) != 1 {
		t.Fatalf("recipe shape changed: %d plays", len(plays))
	}
	var app string
	if spec, ok := plays[0].Tasks[0]["dokku_app"].(map[string]interface{}); ok {
		app, _ = spec["app"].(string)
	}
	if app != `we"b` {
		t.Errorf("app = %q, want %q; the quote style around the interpolation was not preserved:\n%s", app, `we"b`, rendered.String())
	}
}
