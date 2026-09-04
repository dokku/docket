package tasks

import (
	"reflect"
	"sort"
	"strings"
	"testing"

	yaml "gopkg.in/yaml.v3"
)

// convertFixtureYAML and convertFixtureJSON5 are the same recipe in both
// surface syntaxes, carrying every shape the walkers have to survive:
// head, line and foot comments, a nested tasks list, a block scalar, a
// string that would resolve as a number and one that would resolve as a
// bool, an inline scalar array, and a sigil template.
const convertFixtureYAML = `# top of file
- name: web
  # about the tasks
  tasks:
    - name: create app # trailing note
      dokku_app:
        app: '{{ .app }}'
        state: present

    - name: config
      dokku_config:
        app: web
        keys:
          PORT: "5000"
          DEBUG: "true"
          NOTE: |
            first line
            second line
        restart: false
        order: [1, 2, 3]
`

const convertFixtureJSON5 = `// top of file
[
  {
    name: "web",
    // about the tasks
    tasks: [
      {
        name: "create app", // trailing note
        dokku_app: {
          app: "{{ .app }}",
          state: "present",
        },
      },

      {
        name: "config",
        dokku_config: {
          app: "web",
          keys: {
            PORT: "5000",
            DEBUG: "true",
            NOTE: "first line\nsecond line\n",
          },
          restart: false,
          order: [1, 2, 3],
        },
      },
    ],
  },
]
`

// fixtureFor returns the fixture written in the given codec's syntax.
func fixtureFor(t *testing.T, name string) string {
	t.Helper()
	switch name {
	case FormatYAML:
		return convertFixtureYAML
	case FormatNameJSON5:
		return convertFixtureJSON5
	}
	t.Fatalf("no conversion fixture for codec %q; add one when adding a format", name)
	return ""
}

// TestConvertSameFormatIsFormat pins the property every pre-existing
// `docket fmt` invocation depends on: converting to the format a recipe is
// already in is byte-for-byte what the formatter alone would have done.
func TestConvertSameFormatIsFormat(t *testing.T) {
	t.Parallel()

	cases := map[string]map[string]string{
		FormatYAML: {
			"recipe":        convertFixtureYAML,
			"empty":         "",
			"comment only":  "# just a note\n",
			"marker":        "---\n- name: web\n  tasks: []\n",
			"non-canonical": "- tasks: []\n  name: web\n",
		},
		FormatNameJSON5: {
			"recipe":        convertFixtureJSON5,
			"non-canonical": "[{tasks: [], name: \"web\"}]\n",
		},
	}

	for _, codec := range Codecs() {
		codec := codec
		for label, input := range cases[codec.Name()] {
			label, input := label, input
			t.Run(codec.Name()+"/"+label, func(t *testing.T) {
				t.Parallel()
				want, wantErr := codec.Format([]byte(input))
				got, gotErr := Convert([]byte(input), codec, codec)
				if (wantErr == nil) != (gotErr == nil) {
					t.Fatalf("Format err = %v, Convert err = %v", wantErr, gotErr)
				}
				if string(got) != string(want) {
					t.Errorf("Convert != Format:\nwant:\n%s\ngot:\n%s", want, got)
				}
			})
		}
	}
}

// TestConvertMatrix runs every ordered pair of registered codecs. A new
// format joining the registry is checked against all of these without
// anyone editing the test.
func TestConvertMatrix(t *testing.T) {
	t.Parallel()

	for _, from := range Codecs() {
		for _, to := range Codecs() {
			if from.Name() == to.Name() {
				continue
			}
			from, to := from, to
			t.Run(from.Name()+"_to_"+to.Name(), func(t *testing.T) {
				t.Parallel()
				input := []byte(fixtureFor(t, from.Name()))

				out, err := Convert(input, from, to)
				if err != nil {
					t.Fatalf("Convert: %v", err)
				}

				// The conversion writes canonical bytes, so a converted
				// file passes `docket fmt --check` without a second pass.
				formatted, err := to.Format(out)
				if err != nil {
					t.Fatalf("Format of converted output: %v", err)
				}
				if string(formatted) != string(out) {
					t.Errorf("converted output is not canonical:\nconverted:\n%s\nformatted:\n%s", out, formatted)
				}

				if problems := to.Lint(out); len(problems) > 0 {
					t.Errorf("Lint of converted output = %v, want none", problems)
				}

				// And it means the same recipe.
				before, err := UnmarshalRecipe(input, from.Name())
				if err != nil {
					t.Fatalf("UnmarshalRecipe(input): %v", err)
				}
				after, err := UnmarshalRecipe(out, to.Name())
				if err != nil {
					t.Fatalf("UnmarshalRecipe(converted): %v", err)
				}
				if !reflect.DeepEqual(before, after) {
					t.Errorf("converted recipe differs:\nbefore: %#v\nafter:  %#v", before, after)
				}
			})
		}
	}
}

// TestConvertPreservesCommentText is the property that makes conversion
// worth doing at all. It compares comment bodies as a multiset rather than
// asserting on placement, which catches a comment being dropped and a
// comment being printed twice with one assertion, and stays true for a
// format whose comment anchors do not line up exactly with YAML's.
func TestConvertPreservesCommentText(t *testing.T) {
	t.Parallel()

	for _, from := range Codecs() {
		for _, to := range Codecs() {
			if from.Name() == to.Name() {
				continue
			}
			from, to := from, to
			t.Run(from.Name()+"_to_"+to.Name(), func(t *testing.T) {
				t.Parallel()
				input := []byte(fixtureFor(t, from.Name()))

				out, err := Convert(input, from, to)
				if err != nil {
					t.Fatalf("Convert: %v", err)
				}

				want := commentBodies(t, from, input)
				got := commentBodies(t, to, out)
				if len(want) == 0 {
					t.Fatal("fixture carries no comments; the test would prove nothing")
				}
				if !reflect.DeepEqual(want, got) {
					t.Errorf("comments changed:\nwant %q\ngot  %q\noutput:\n%s", want, got, out)
				}
			})
		}
	}
}

// TestConvertRoundTripConverges checks that comment attachment settles
// after one pass: a second full round trip reproduces the first exactly.
func TestConvertRoundTripConverges(t *testing.T) {
	t.Parallel()

	for _, from := range Codecs() {
		for _, to := range Codecs() {
			if from.Name() == to.Name() {
				continue
			}
			from, to := from, to
			t.Run(from.Name()+"_to_"+to.Name(), func(t *testing.T) {
				t.Parallel()
				input := []byte(fixtureFor(t, from.Name()))

				there, err := Convert(input, from, to)
				if err != nil {
					t.Fatalf("Convert out: %v", err)
				}
				back, err := Convert(there, to, from)
				if err != nil {
					t.Fatalf("Convert back: %v", err)
				}
				thereAgain, err := Convert(back, from, to)
				if err != nil {
					t.Fatalf("Convert out again: %v", err)
				}
				if string(thereAgain) != string(there) {
					t.Errorf("round trip does not converge:\nfirst:\n%s\nsecond:\n%s", there, thereAgain)
				}

				backAgain, err := Convert(thereAgain, to, from)
				if err != nil {
					t.Fatalf("Convert back again: %v", err)
				}
				if string(backAgain) != string(back) {
					t.Errorf("reverse round trip does not converge:\nfirst:\n%s\nsecond:\n%s", back, backAgain)
				}
			})
		}
	}
}

// TestConvertEmptyAndCommentOnlyIsNoOp pins that a file with no recipe in
// it is left exactly as it was, rather than becoming an empty document in
// the other format.
func TestConvertEmptyAndCommentOnlyIsNoOp(t *testing.T) {
	t.Parallel()

	for _, input := range []string{"", "# just a note\n", "\n\n"} {
		got, err := Convert([]byte(input), CodecFor(FormatYAML), CodecFor(FormatNameJSON5))
		if err != nil {
			t.Fatalf("Convert(%q): %v", input, err)
		}
		if string(got) != input {
			t.Errorf("Convert(%q) = %q, want it untouched", input, got)
		}
	}
}

// TestConvertRejectsMultiDocument keeps Format's rule: a file holding more
// than one YAML document is refused rather than half-converted.
func TestConvertRejectsMultiDocument(t *testing.T) {
	t.Parallel()
	in := []byte("- name: a\n  tasks: []\n---\n- name: b\n  tasks: []\n")
	if out, err := Convert(in, CodecFor(FormatYAML), CodecFor(FormatNameJSON5)); err == nil {
		t.Errorf("Convert accepted a multi-document file: %s", out)
	}
}

// TestConvertNonSequenceRoot covers the shapes `fmt` accepts that are not
// recipes. It formats any document today and must keep converting any
// document too.
func TestConvertNonSequenceRoot(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want string
	}{
		{"mapping root", "a: 1\n", "{\n  a: 1,\n}\n"},
		{"scalar root", "hi\n", "\"hi\"\n"},
		{"empty sequence", "[]\n", "[]\n"},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got, err := Convert([]byte(tc.in), CodecFor(FormatYAML), CodecFor(FormatNameJSON5))
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if string(got) != tc.want {
				t.Errorf("Convert(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestConvertInlinesAliases covers the anchor policy: sharing is expanded
// into copies, which is what the loader already does when it unmarshals,
// and the anchor declarations disappear.
func TestConvertInlinesAliases(t *testing.T) {
	t.Parallel()

	in := []byte("- name: &n web\n  tasks: []\n- name: *n\n  tasks: []\n")
	out, err := Convert(in, CodecFor(FormatYAML), CodecFor(FormatNameJSON5))
	if err != nil {
		t.Fatalf("Convert: %v", err)
	}
	if strings.Contains(string(out), "&n") || strings.Contains(string(out), "*n") {
		t.Errorf("anchor or alias survived into json5:\n%s", out)
	}
	if n := strings.Count(string(out), `name: "web"`); n != 2 {
		t.Errorf("alias expanded %d times, want 2:\n%s", n, out)
	}
}

// TestConvertExpandsMergeKeys walks the YAML 1.1 merge precedence rules:
// a key the mapping states itself beats a merged one, and an earlier merge
// source beats a later one.
func TestConvertExpandsMergeKeys(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{
			name: "own keys win",
			in:   "defaults: &d\n  host: shared\n  sudo: \"yes\"\nplay:\n  <<: *d\n  host: mine\n",
			want: map[string]string{"host": "mine", "sudo": "yes"},
		},
		{
			name: "earlier source wins",
			in:   "a: &a\n  k: first\nb: &b\n  k: second\nplay:\n  <<: [*a, *b]\n",
			want: map[string]string{"k": "first"},
		},
		{
			name: "nested merge resolves first",
			in:   "base: &base\n  k: deep\nmid: &mid\n  <<: *base\nplay:\n  <<: *mid\n",
			want: map[string]string{"k": "deep"},
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := Convert([]byte(tc.in), CodecFor(FormatYAML), CodecFor(FormatNameJSON5))
			if err != nil {
				t.Fatalf("Convert: %v", err)
			}
			if strings.Contains(string(out), "<<") {
				t.Errorf("merge key survived into json5:\n%s", out)
			}
			var decoded map[string]interface{}
			yamlBytes, problem := CodecFor(FormatNameJSON5).ToYAML(out)
			if problem != nil {
				t.Fatalf("ToYAML: %s", problem.Message)
			}
			if err := yaml.Unmarshal(yamlBytes, &decoded); err != nil {
				t.Fatalf("Unmarshal: %v", err)
			}
			play, _ := decoded["play"].(map[string]interface{})
			for key, want := range tc.want {
				if got, _ := play[key].(string); got != want {
					t.Errorf("play[%q] = %q, want %q (output:\n%s)", key, got, want, out)
				}
			}
		})
	}
}

// TestConvertRejectsBadMergeValue keeps a malformed merge from being
// quietly ignored.
func TestConvertRejectsBadMergeValue(t *testing.T) {
	t.Parallel()
	in := []byte("play:\n  <<: not-a-mapping\n")
	if out, err := Convert(in, CodecFor(FormatYAML), CodecFor(FormatNameJSON5)); err == nil {
		t.Errorf("Convert accepted a scalar merge value: %s", out)
	}
}

// TestConvertRejectsUnrepresentable covers the constructs a target format
// cannot carry. Each has to be reported, never guessed at.
func TestConvertRejectsUnrepresentable(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		from string
		to   string
		in   string
		want string
	}{
		{
			name: "complex mapping key",
			from: FormatYAML,
			to:   FormatNameJSON5,
			in:   "? [a, b]\n: value\n",
			want: "not a scalar",
		},
		{
			name: "custom tag",
			from: FormatYAML,
			to:   FormatNameJSON5,
			in:   "key: !Foo bar\n",
			want: "no json5 representation",
		},
		{
			name: "bare json5 identifier",
			from: FormatNameJSON5,
			to:   FormatYAML,
			in:   "{ key: web }\n",
			want: "not valid json5",
		},
		{
			name: "duplicate json5 key",
			from: FormatNameJSON5,
			to:   FormatYAML,
			in:   "{ key: 1, key: 2 }\n",
			want: "duplicate key",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			out, err := Convert([]byte(tc.in), CodecFor(tc.from), CodecFor(tc.to))
			if err == nil {
				t.Fatalf("Convert accepted %q: %s", tc.in, out)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error = %q, want it to mention %q", err, tc.want)
			}
			if out != nil {
				t.Errorf("Convert returned bytes alongside an error: %q", out)
			}
		})
	}
}

// TestConvertNamesTheDuplicateKey pins what the Lint screen buys. The
// same file is refused either way - the JSON5 formatter's own round-trip
// guard has always rejected a duplicate-keyed object, and same-format
// formatting still gets exactly that - but a conversion says which key is
// duplicated instead of reporting that the recipe changed.
func TestConvertNamesTheDuplicateKey(t *testing.T) {
	t.Parallel()
	in := []byte("{ key: 1, key: 2 }\n")

	_, sameErr := Convert(in, CodecFor(FormatNameJSON5), CodecFor(FormatNameJSON5))
	_, formatErr := CodecFor(FormatNameJSON5).Format(in)
	if sameErr == nil || formatErr == nil || sameErr.Error() != formatErr.Error() {
		t.Errorf("same-format Convert err = %v, Format err = %v; want them identical", sameErr, formatErr)
	}

	_, convertErr := Convert(in, CodecFor(FormatNameJSON5), CodecFor(FormatYAML))
	if convertErr == nil {
		t.Fatal("cross-format Convert accepted a duplicate-keyed recipe")
	}
	if !strings.Contains(convertErr.Error(), `duplicate key "key"`) {
		t.Errorf("cross-format error = %q, want it to name the duplicated key", convertErr)
	}
}

// TestConvertRejectsDeepAliasNest keeps a small file from asking for an
// enormous tree.
func TestConvertRejectsDeepAliasNest(t *testing.T) {
	t.Parallel()

	var b strings.Builder
	b.WriteString("a0: &a0 [x]\n")
	for i := 1; i < 40; i++ {
		b.WriteString("a")
		b.WriteString(itoa(i))
		b.WriteString(": &a")
		b.WriteString(itoa(i))
		b.WriteString(" [*a")
		b.WriteString(itoa(i - 1))
		b.WriteString(", *a")
		b.WriteString(itoa(i - 1))
		b.WriteString("]\n")
	}
	if out, err := Convert([]byte(b.String()), CodecFor(FormatYAML), CodecFor(FormatNameJSON5)); err == nil {
		t.Errorf("Convert accepted an exponential alias nest, produced %d bytes", len(out))
	}
}

// TestEquivalentConvertedNodes pins the comparator the cross-format guard
// leans on: a deliberate normalisation compares equal, a change in meaning
// does not.
func TestEquivalentConvertedNodes(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		a, b string
		want bool
	}{
		{"hex and decimal", "k: 0x1F", "k: 31", true},
		{"underscored int", "k: 1_000", "k: 1000", true},
		{"octal", "k: 0o17", "k: 15", true},
		{"timestamp widened to string", "k: 2015-01-01", `k: "2015-01-01"`, true},
		{"float spellings", "k: 1.50", "k: 1.5", true},
		{"string is not int", `k: "5"`, "k: 5", false},
		{"bool is not string", "k: true", `k: "true"`, false},
		{"different value", "k: 1", "k: 2", false},
		{"missing key", "k: 1\nj: 2", "k: 1", false},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var a, b yaml.Node
			if err := yaml.Unmarshal([]byte(tc.a), &a); err != nil {
				t.Fatalf("unmarshal a: %v", err)
			}
			if err := yaml.Unmarshal([]byte(tc.b), &b); err != nil {
				t.Fatalf("unmarshal b: %v", err)
			}
			if got := equivalentConvertedNodes(documentBody(&a), documentBody(&b)); got != tc.want {
				t.Errorf("equivalentConvertedNodes(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// commentBodies returns every comment in a recipe as sorted, delimiter-
// stripped text, so two formats' comments can be compared directly.
func commentBodies(t *testing.T, codec Codec, data []byte) []string {
	t.Helper()
	doc, err := codec.DecodeDocument(data)
	if err != nil {
		t.Fatalf("DecodeDocument: %v", err)
	}
	var out []string
	collectComments(doc, &out)
	sort.Strings(out)
	return out
}

// collectComments walks a node tree gathering head, line and foot comment
// text, one entry per line.
func collectComments(n *yaml.Node, out *[]string) {
	if n == nil {
		return
	}
	for _, comment := range []string{n.HeadComment, n.LineComment, n.FootComment} {
		for _, line := range strings.Split(comment, "\n") {
			line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "#"))
			if line != "" {
				*out = append(*out, line)
			}
		}
	}
	for _, child := range n.Content {
		collectComments(child, out)
	}
}

// itoa keeps the alias-nest fixture readable without dragging strconv into
// the test's import list for one call.
func itoa(i int) string {
	if i < 10 {
		return string(rune('0' + i))
	}
	return itoa(i/10) + string(rune('0'+i%10))
}

// FuzzConvertRoundTrip is the backstop for the scalar and comment tables:
// they cover what was thought of, this covers what was not.
//
// The contract is deliberately weak, because most random input is not a
// recipe at all. Convert may refuse anything it likes; what it may not do
// is panic, or produce output that means something other than what it was
// given.
func FuzzConvertRoundTrip(f *testing.F) {
	f.Add(convertFixtureYAML)
	f.Add(convertFixtureJSON5)
	f.Add("- name: web\n  tasks: []\n")
	f.Add("[{ name: \"web\", tasks: [] }]\n")
	f.Add("# comment only\n")
	f.Add("")
	f.Add("k: <<\n")
	f.Add("- &a [1]\n- *a\n")
	f.Add("a: &a {k: v}\nb:\n  <<: *a\n")
	f.Add("{ a: 0x1F, b: Infinity, c: \"true\" }")

	f.Fuzz(func(t *testing.T, input string) {
		for _, from := range Codecs() {
			for _, to := range Codecs() {
				if from.Name() == to.Name() {
					continue
				}
				out, err := Convert([]byte(input), from, to)
				if err != nil {
					if out != nil {
						t.Errorf("%s to %s returned bytes alongside an error %v: %q", from.Name(), to.Name(), err, out)
					}
					continue
				}
				// Whatever came out has to be readable as the format it
				// claims to be, and has to mean what went in. Convert's own
				// guard checks the second; re-checking here catches a guard
				// that stopped running.
				before, err := from.DecodeDocument([]byte(input))
				if err != nil || before == nil {
					continue
				}
				// Compare against the flattened tree, not the raw one:
				// inlining an alias is a deliberate rewrite, so a document
				// that still holds the alias node is not the thing the
				// output should equal.
				if err := inlineAliases(before); err != nil {
					continue
				}
				if err := expandMergeKeys(before); err != nil {
					continue
				}
				after, err := to.DecodeDocument(out)
				if err != nil {
					t.Fatalf("%s to %s produced unreadable %s: %v\ninput:\n%s\noutput:\n%s",
						from.Name(), to.Name(), to.Name(), err, input, out)
				}
				if !equivalentConvertedNodes(documentBody(before), documentBody(after)) {
					t.Fatalf("%s to %s changed the recipe\ninput:\n%s\noutput:\n%s",
						from.Name(), to.Name(), input, out)
				}
			}
		}
	})
}
